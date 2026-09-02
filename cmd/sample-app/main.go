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
)

// scope names the instrumentation scope. OTel Lite names log streams after
// it, and stream names are path segments, so keep it free of slashes.
const scope = "sample-app"

var routes = []string{"/login", "/users", "/users/{id}", "/health", "/search"}

func main() {
	endpoint := flag.String("endpoint", "localhost:4318", "host:port of the system of record (OTLP/HTTP)")
	namespace := flag.String("namespace", "iam", "service.namespace resource attribute")
	service := flag.String("service", "iam-api", "service.name resource attribute")
	interval := flag.Duration("interval", 5*time.Second, "how often metrics are exported")
	rps := flag.Float64("rps", 20, "simulated requests per second")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	res, err := resource.Merge(resource.Default(), resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceNamespace(*namespace),
		semconv.ServiceName(*service),
	))
	if err != nil {
		log.Fatalf("resource: %v", err)
	}

	metricExp, err := otlpmetrichttp.New(ctx, otlpmetrichttp.WithEndpoint(*endpoint), otlpmetrichttp.WithInsecure())
	if err != nil {
		log.Fatalf("metric exporter: %v", err)
	}
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp, sdkmetric.WithInterval(*interval))),
	)
	defer mp.Shutdown(context.Background())

	logExp, err := otlploghttp.New(ctx, otlploghttp.WithEndpoint(*endpoint), otlploghttp.WithInsecure())
	if err != nil {
		log.Fatalf("log exporter: %v", err)
	}
	lp := sdklog.NewLoggerProvider(
		sdklog.WithResource(res),
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logExp, sdklog.WithExportInterval(*interval))),
	)
	defer lp.Shutdown(context.Background())

	sim, err := newSimulator(mp.Meter(scope), lp.Logger(scope))
	if err != nil {
		log.Fatalf("instruments: %v", err)
	}

	log.Printf("sample-app: exporting %s/%s to http://%s every %s (%.0f rps)", *namespace, *service, *endpoint, *interval, *rps)
	sim.run(ctx, *rps)
	log.Print("sample-app: shutting down")
}

// simulator fakes a web service's traffic and process state.
type simulator struct {
	logger otellog.Logger

	requests metric.Int64Counter
	errors   metric.Int64Counter
	duration metric.Float64Histogram
	inflight metric.Int64UpDownCounter

	memUsed int64
	start   time.Time
}

func newSimulator(m metric.Meter, l otellog.Logger) (*simulator, error) {
	s := &simulator{logger: l, memUsed: 40 << 20, start: time.Now()}
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
			o.Observe(s.memUsed)
			return nil
		}))
	if err != nil {
		return nil, err
	}
	_, err = m.Float64ObservableGauge("process.cpu.utilization", metric.WithUnit("1"), metric.WithDescription("Simulated CPU utilization 0..1"),
		metric.WithFloat64Callback(func(_ context.Context, o metric.Float64Observer) error {
			o.Observe(s.cpu())
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

// cpu follows a slow sine wave with jitter so graphs have some shape.
func (s *simulator) cpu() float64 {
	t := time.Since(s.start).Seconds()
	v := 0.35 + 0.25*math.Sin(t/60) + rand.Float64()*0.1
	return math.Max(0, math.Min(1, v))
}

func (s *simulator) run(ctx context.Context, rps float64) {
	tick := time.NewTicker(time.Duration(float64(time.Second) / rps))
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			s.request(ctx)
		}
	}
}

func (s *simulator) request(ctx context.Context) {
	route := routes[rand.IntN(len(routes))]
	status := 200
	switch r := rand.Float64(); {
	case r < 0.02:
		status = 500
	case r < 0.07:
		status = 404
	}
	latency := 5 + rand.ExpFloat64()*40 // ms, long-tailed
	if route == "/search" {
		latency *= 3
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

	// Memory drifts upward and occasionally "GCs" back down.
	s.memUsed += int64(rand.IntN(512 << 10))
	if rand.Float64() < 0.01 {
		s.memUsed = 40<<20 + int64(rand.IntN(8<<20))
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
