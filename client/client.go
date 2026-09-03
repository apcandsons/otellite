package client

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/metric"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
)

// Options configures Start. The zero value is a working configuration
// driven entirely by the OTEL_* environment.
type Options struct {
	Handler    slog.Handler         // the app's own handler; nil => TextHandler(os.Stderr)
	Scope      string               // log stream / meter scope; default OTEL_SERVICE_NAME, else "default"; "/" becomes "-"
	Endpoint   string               // overrides OTEL_EXPORTER_OTLP_ENDPOINT
	Interval   time.Duration        // overrides OTEL_METRIC_EXPORT_INTERVAL (default 60 s)
	Attributes []attribute.KeyValue // extra resource attributes, merged over the environment
	Redact     *regexp.Regexp       // attribute keys to redact; nil => DefaultRedact
	NoStdLog   bool                 // leave the standard log package alone
	NoProcess  bool                 // do not register the process gauges
}

// DefaultRedact matches attribute keys (and group.key paths) whose values
// must never reach a log sink.
var DefaultRedact = regexp.MustCompile(`(?i)(password|secret|token|authorization|cookie|key)`)

// state is what Start installs for the package-level accessors.
type state struct {
	scope  string
	logger *slog.Logger
}

var (
	current atomic.Pointer[state]

	// generation increments on every Start so instruments cached by the
	// middleware are rebuilt against the new meter provider.
	generation atomic.Uint64
)

// Start configures the OTel SDK from o and the OTEL_* environment, installs
// the slog fan-out as slog's default, and returns a shutdown that flushes
// and stops the exporters. Without an endpoint only the fan-out is
// installed and shutdown is a no-op; err is nil.
func Start(ctx context.Context, o Options) (shutdown func(context.Context) error, err error) {
	endpoint := o.Endpoint
	if endpoint == "" {
		endpoint = os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	}
	scope := resolveScope(o.Scope)
	redact := o.Redact
	if redact == nil {
		redact = DefaultRedact
	}
	app := o.Handler
	if app == nil {
		app = slog.NewTextHandler(os.Stderr, nil)
	}

	generation.Add(1)
	if endpoint == "" {
		install(scope, newFanout(redact, app), o.NoStdLog)
		return func(context.Context) error { return nil }, nil
	}

	res, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithAttributes(o.Attributes...),
	)
	if err != nil && !errors.Is(err, resource.ErrPartialResource) {
		return nil, fmt.Errorf("client: resource: %w", err)
	}
	metricExp, err := otlpmetrichttp.New(ctx,
		otlpmetrichttp.WithEndpointURL(signalURL(endpoint, "/v1/metrics")),
		otlpmetrichttp.WithTemporalitySelector(Temporality),
	)
	if err != nil {
		return nil, fmt.Errorf("client: metric exporter: %w", err)
	}
	logExp, err := otlploghttp.New(ctx, otlploghttp.WithEndpointURL(signalURL(endpoint, "/v1/logs")))
	if err != nil {
		return nil, fmt.Errorf("client: log exporter: %w", err)
	}

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp, sdkmetric.WithInterval(resolveInterval(o.Interval)))),
	)
	lp := sdklog.NewLoggerProvider(
		sdklog.WithResource(res),
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logExp)),
	)
	otel.SetMeterProvider(mp)
	install(scope, newFanout(redact, app, newBridge(lp.Logger(scope))), o.NoStdLog)
	if !o.NoProcess {
		registerProcess(mp.Meter(scope))
	}
	return func(ctx context.Context) error {
		return errors.Join(mp.Shutdown(ctx), lp.Shutdown(ctx))
	}, nil
}

// install makes the fan-out the process-wide logger and, unless noStdLog,
// routes the standard log package through it at INFO.
func install(scope string, h slog.Handler, noStdLog bool) {
	prevOut, prevFlags := log.Writer(), log.Flags()
	logger := slog.New(h)
	slog.SetDefault(logger)
	if noStdLog {
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
	} else {
		log.SetFlags(0)
		log.SetOutput(lineWriter{h})
	}
	current.Store(&state{scope: scope, logger: logger})
}

// signalURL appends the OTLP signal path to the base endpoint, the way
// the SDK does for OTEL_EXPORTER_OTLP_ENDPOINT.
func signalURL(endpoint, path string) string {
	return strings.TrimRight(endpoint, "/") + path
}

// Meter returns the meter for this service's scope from the global
// provider. Call Start first.
func Meter() metric.Meter {
	return otel.GetMeterProvider().Meter(scopeName())
}

// Logger returns the fan-out logger installed by Start (also slog's
// default), or slog.Default before Start.
func Logger() *slog.Logger {
	if s := current.Load(); s != nil {
		return s.logger
	}
	return slog.Default()
}

func scopeName() string {
	if s := current.Load(); s != nil {
		return s.scope
	}
	return resolveScope("")
}

// lazy builds a value once per Start generation, so instruments cached by
// the middleware follow the meter provider installed by the latest Start.
type lazy[T any] struct {
	mu sync.Mutex
	p  atomic.Pointer[lazyVal[T]]
}

type lazyVal[T any] struct {
	gen uint64
	v   T
}

func (l *lazy[T]) get(build func() T) T {
	gen := generation.Load()
	if p := l.p.Load(); p != nil && p.gen == gen {
		return p.v
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if p := l.p.Load(); p != nil && p.gen == gen {
		return p.v
	}
	v := build()
	l.p.Store(&lazyVal[T]{gen: gen, v: v})
	return v
}
