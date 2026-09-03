// Command sample-app simulates a small web service and exports metrics and
// logs to an OTel Lite system of record over OTLP/HTTP.
//
// It is a demo client only: it uses the official OpenTelemetry Go SDK the
// same way any real service would, so it doubles as an interop check for
// the sor receiver.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math"
	"math/rand/v2"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	otellog "go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/metric"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"

	"github.com/apcandsons/otellite/client"
)

// scope names the instrumentation scope. OTel Lite names log streams after
// it, and stream names are path segments, so keep it free of slashes.
const scope = "sample-app"

var routes = []string{"/login", "/users", "/users/{id}", "/health", "/search"}

func main() {
	endpoint := flag.String("endpoint", "localhost:4318", "host:port of the system of record (OTLP/HTTP)")
	namespace := flag.String("namespace", "iam", "service.namespace resource attribute (ignored with -config)")
	service := flag.String("service", "iam-api", "service.name resource attribute (ignored with -config)")
	interval := flag.Duration("interval", 5*time.Second, "how often metrics are exported")
	rps := flag.Float64("rps", 20, "simulated requests per second (default per service with -config)")
	burstFor := flag.Duration("burst", 3*time.Minute, "how long a burst (press b) lasts")
	config := flag.String("config", "", "file listing services to simulate: one 'service <namespace> <name> [rps=<n>]' per line")
	flag.Parse()

	services := []serviceConfig{{Namespace: *namespace, Service: *service, RPS: *rps}}
	if *config != "" {
		var err error
		if services, err = loadConfig(*config, *rps); err != nil {
			log.Fatalf("config: %v", err)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var sims []*simulator
	for _, sc := range services {
		sim, shutdown, err := startService(ctx, *endpoint, sc, *interval, *burstFor)
		if err != nil {
			log.Fatalf("%s/%s: %v", sc.Namespace, sc.Service, err)
		}
		defer shutdown()
		sims = append(sims, sim)
		go sim.run(ctx)
	}

	restore := rawTerminal()
	defer restore()
	keys := make(chan byte)
	go readKeys(os.Stdin, keys)

	log.Printf("sample-app: exporting to http://%s every %s", *endpoint, *interval)
	for i, sim := range sims {
		log.Printf("  %d: %s (%.0f rps)", i+1, sim.name, sim.rps)
	}
	log.Printf("sample-app: press b to burst every service for %s, 1-%d to burst one, p to pause/resume gauges, q to quit", *burstFor, len(sims))

	control(ctx, sims, keys)
	restore()
	log.Print("sample-app: shutting down")
}

// control dispatches keypresses until q, Ctrl-C, or ctx is done.
func control(ctx context.Context, sims []*simulator, keys <-chan byte) {
	for {
		select {
		case <-ctx.Done():
			return
		case k := <-keys:
			switch {
			case k == 'b' || k == 'B':
				for _, sim := range sims {
					sim.startBurst()
				}
			case k >= '1' && k <= '9' && int(k-'1') < len(sims):
				sims[k-'1'].startBurst()
			case k == 'p' || k == 'P':
				for _, sim := range sims {
					sim.togglePause()
				}
			case k == 'q' || k == 'Q' || k == 3: // 3 is Ctrl-C, which raw mode no longer turns into SIGINT
				return
			}
		}
	}
}

// startService builds the OTel SDK pipeline for one simulated service and
// returns its simulator plus a func that flushes and shuts the pipeline down.
func startService(ctx context.Context, endpoint string, sc serviceConfig, interval, burstFor time.Duration) (*simulator, func(), error) {
	res, err := resource.Merge(resource.Default(), resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceNamespace(sc.Namespace),
		semconv.ServiceName(sc.Service),
	))
	if err != nil {
		return nil, nil, fmt.Errorf("resource: %w", err)
	}
	metricExp, err := otlpmetrichttp.New(ctx,
		otlpmetrichttp.WithEndpoint(endpoint),
		otlpmetrichttp.WithInsecure(),
		otlpmetrichttp.WithTemporalitySelector(client.Temporality),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("metric exporter: %w", err)
	}
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp, sdkmetric.WithInterval(interval))),
	)
	logExp, err := otlploghttp.New(ctx, otlploghttp.WithEndpoint(endpoint), otlploghttp.WithInsecure())
	if err != nil {
		mp.Shutdown(context.Background())
		return nil, nil, fmt.Errorf("log exporter: %w", err)
	}
	lp := sdklog.NewLoggerProvider(
		sdklog.WithResource(res),
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logExp, sdklog.WithExportInterval(interval))),
	)
	shutdown := func() {
		mp.Shutdown(context.Background())
		lp.Shutdown(context.Background())
	}
	sim, err := newSimulator(mp.Meter(scope), lp.Logger(scope), newBurst(burstFor))
	if err != nil {
		shutdown()
		return nil, nil, fmt.Errorf("instruments: %w", err)
	}
	sim.name = sc.Namespace + "/" + sc.Service
	sim.rps = sc.RPS
	return sim, shutdown, nil
}

// simulator fakes a web service's traffic and process state.
type simulator struct {
	name   string // namespace/service, for log lines
	rps    float64
	logger otellog.Logger

	requests metric.Int64Counter
	errors   metric.Int64Counter
	duration metric.Float64Histogram
	inflight metric.Int64UpDownCounter

	memUsed int64
	start   time.Time
	burst   *burst
	blip    *burst
	paused  atomic.Bool // gauges stop reporting, so their streams go absent
}

// During a burst: traffic multiplies, half the requests fail, latency
// balloons, memory climbs without a GC until it hits a ceiling, and CPU
// pins near 100%.
const (
	burstRPSFactor    = 10
	burstErrorRate    = 0.5
	burstLatencyScale = 8.0
	burstMemPerReq    = 96 << 10
	burstMemCap       = 512 << 20
	burstCPU          = 0.92
)

// Blips are short, random episodes of elevated errors and traffic. They
// push the per-interval counters over their alert thresholds but end
// well before a rule's "for" window, so the dashboard shows amber, not
// red. Bursts (the b key) are what actually fire alerts.
const (
	blipRPSFactor   = 5
	blipErrorRate   = 0.25
	blipMinFor      = 5 * time.Second
	blipMaxFor      = 20 * time.Second
	blipMinGap      = 45 * time.Second
	blipMaxGap      = 120 * time.Second
	normalErrorRate = 0.02
)

// pace is how the request loop should run right now.
type pace int

const (
	normal pace = iota
	blipping
	bursting
)

func (p pace) rpsFactor() float64 {
	switch p {
	case bursting:
		return burstRPSFactor
	case blipping:
		return blipRPSFactor
	}
	return 1
}

func (p pace) errorRate() float64 {
	switch p {
	case bursting:
		return burstErrorRate
	case blipping:
		return blipErrorRate
	}
	return normalErrorRate
}

func newSimulator(m metric.Meter, l otellog.Logger, b *burst) (*simulator, error) {
	s := &simulator{logger: l, memUsed: 40 << 20, start: time.Now(), burst: b, blip: newBurst(blipMaxFor)}
	var err error
	if s.requests, err = m.Int64Counter("http.server.requests", metric.WithUnit("1"), metric.WithDescription("Total HTTP requests")); err != nil {
		return nil, err
	}
	if s.errors, err = m.Int64Counter("http.server.errors", metric.WithUnit("1"), metric.WithDescription("HTTP responses with status >= 500")); err != nil {
		return nil, err
	}
	if s.duration, err = m.Float64Histogram("http.server.duration", metric.WithUnit("ms"), metric.WithDescription("HTTP request latency")); err != nil {
		return nil, err
	}
	if s.inflight, err = m.Int64UpDownCounter("http.server.active_requests", metric.WithUnit("1"), metric.WithDescription("In-flight HTTP requests")); err != nil {
		return nil, err
	}
	_, err = m.Int64ObservableGauge("go.memory.used", metric.WithUnit("By"), metric.WithDescription("Simulated heap in use"),
		metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
			if !s.paused.Load() {
				o.Observe(s.memUsed)
			}
			return nil
		}))
	if err != nil {
		return nil, err
	}
	_, err = m.Float64ObservableGauge("process.cpu.utilization", metric.WithUnit("1"), metric.WithDescription("Simulated CPU utilization 0..1"),
		metric.WithFloat64Callback(func(_ context.Context, o metric.Float64Observer) error {
			if !s.paused.Load() {
				o.Observe(s.cpu())
			}
			return nil
		}))
	if err != nil {
		return nil, err
	}
	_, err = m.Float64ObservableCounter("process.uptime", metric.WithUnit("s"),
		metric.WithFloat64Callback(func(_ context.Context, o metric.Float64Observer) error {
			o.Observe(time.Since(s.start).Seconds())
			return nil
		}))
	return s, err
}

// cpu follows a slow sine wave with jitter so graphs have some shape, and
// pins high during a burst.
func (s *simulator) cpu() float64 {
	if s.burst.active(time.Now()) {
		return math.Min(1, burstCPU+rand.Float64()*0.06)
	}
	t := time.Since(s.start).Seconds()
	v := 0.35 + 0.25*math.Sin(t/60) + rand.Float64()*0.1
	return math.Max(0, math.Min(1, v))
}

// togglePause stops or resumes the memory and CPU gauges. While paused
// their streams receive no samples, which is what an "absent" rule catches.
func (s *simulator) togglePause() {
	paused := !s.paused.Load()
	s.paused.Store(paused)
	if paused {
		log.Printf("%s: gauges paused (go.memory.used and process.cpu.utilization go absent)", s.name)
	} else {
		log.Printf("%s: gauges resumed", s.name)
	}
}

// startBurst begins a burst unless one is already running.
func (s *simulator) startBurst() {
	if s.burst.start(time.Now()) {
		log.Printf("%s: Bursting...", s.name)
	}
}

// pace reports whether the service is bursting, blipping, or idle.
func (s *simulator) pace(now time.Time) pace {
	switch {
	case s.burst.active(now):
		return bursting
	case s.blip.active(now):
		return blipping
	}
	return normal
}

func randDuration(lo, hi time.Duration) time.Duration {
	return lo + time.Duration(rand.Int64N(int64(hi-lo)))
}

// run drives simulated traffic until ctx is cancelled, speeding up while
// a burst or blip is active and scheduling random blips in between.
func (s *simulator) run(ctx context.Context) {
	interval := time.Duration(float64(time.Second) / s.rps)
	tick := time.NewTicker(interval)
	defer tick.Stop()
	current := normal
	nextBlip := time.Now().Add(randDuration(blipMinGap, blipMaxGap))
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			now := time.Now()
			if now.After(nextBlip) && !s.burst.active(now) {
				d := randDuration(blipMinFor, blipMaxFor)
				s.blip.startFor(now, d)
				nextBlip = now.Add(d + randDuration(blipMinGap, blipMaxGap))
				log.Printf("%s: blip for %s (%.0f%% errors, %dx traffic)", s.name, d.Round(time.Second), blipErrorRate*100, blipRPSFactor)
			}
			if p := s.pace(now); p != current {
				current = p
				tick.Reset(time.Duration(float64(interval) / p.rpsFactor()))
			}
			s.request(ctx)
			if s.burst.tick(now) {
				log.Printf("%s: Ended burst", s.name)
			}
			s.blip.tick(now)
		}
	}
}

func (s *simulator) request(ctx context.Context) {
	p := s.pace(time.Now())
	bursting := p == bursting
	route := routes[rand.IntN(len(routes))]
	status := 200
	errorRate := p.errorRate()
	switch r := rand.Float64(); {
	case r < errorRate:
		status = 500
	case r < errorRate+0.05:
		status = 404
	}
	latency := 5 + rand.ExpFloat64()*40 // ms, long-tailed
	if route == "/search" {
		latency *= 3
	}
	if bursting {
		latency *= burstLatencyScale
	}

	// OTel Lite keys streams by metric name only, so attributes are left
	// off the metrics and carried on the log records instead.
	s.inflight.Add(ctx, 1)
	s.requests.Add(ctx, 1)
	s.duration.Record(ctx, latency)
	if status >= 500 {
		s.errors.Add(ctx, 1)
	}
	s.inflight.Add(ctx, -1)

	// Memory drifts upward and occasionally "GCs" back down. A burst
	// allocates faster and suppresses GC, so the heap keeps climbing until
	// the burst ends and the next GC drops it back.
	if bursting {
		s.memUsed = min(s.memUsed+burstMemPerReq, burstMemCap)
	} else {
		s.memUsed += int64(rand.IntN(512 << 10))
		if rand.Float64() < 0.01 {
			s.memUsed = 40<<20 + int64(rand.IntN(8<<20))
		}
	}

	s.emitLog(ctx, route, status, latency)
}

func (s *simulator) emitLog(ctx context.Context, route string, status int, latency float64) {
	var rec otellog.Record
	rec.SetTimestamp(time.Now())
	rec.AddAttributes(
		attribute.String("http.route", route),
		attribute.Int("http.response.status_code", status),
		attribute.Float64("duration_ms", latency),
	)
	switch {
	case status >= 500:
		rec.SetSeverity(otellog.SeverityError)
		rec.SetSeverityText("ERROR")
		rec.SetBody(attribute.StringValue(fmt.Sprintf("%s failed: upstream timeout (%d)", route, status)))
	case status >= 400:
		rec.SetSeverity(otellog.SeverityWarn)
		rec.SetSeverityText("WARN")
		rec.SetBody(attribute.StringValue(fmt.Sprintf("%s -> %d", route, status)))
	case latency > 100:
		rec.SetSeverity(otellog.SeverityWarn)
		rec.SetSeverityText("WARN")
		rec.SetBody(attribute.StringValue(fmt.Sprintf("%s slow: %.0fms", route, latency)))
	default:
		rec.SetSeverity(otellog.SeverityInfo)
		rec.SetSeverityText("INFO")
		rec.SetBody(attribute.StringValue(fmt.Sprintf("%s -> %d in %.1fms", route, status, latency)))
	}
	s.logger.Emit(ctx, rec)
}
