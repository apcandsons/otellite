// Package otlpexport ships the bridge's gauges to the SoR over OTLP/HTTP.
// The SoR keys streams by resource identity (service.namespace /
// service.name), one resource per export, so the exporter keeps one meter
// provider per distinct <ns>/<svc> among the targets and flushes them all
// on Flush. The bridge's own telemetry (uptime, memory) is not routed here;
// that goes through the ordinary client under the bridge's identity.
package otlpexport

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"

	"github.com/apcandsons/otellite/internal/domain"
)

const (
	meterName = "awsbridge"
	// The periodic reader never fires on its own: Flush drives every export,
	// so a tick's samples leave together and nothing goes out between ticks.
	neverInterval = 24 * time.Hour
)

// Exporter implements usecase.GaugeExporter.
type Exporter struct {
	ctx      context.Context
	endpoint string

	mu        sync.Mutex
	providers map[string]*provider // keyed by <ns>/<svc>
}

type provider struct {
	mp     *sdkmetric.MeterProvider
	meter  metric.Meter
	gauges map[string]metric.Float64Gauge // by full metric name
}

// New builds an exporter that posts to endpoint's /v1/metrics. Resource
// attributes from OTEL_RESOURCE_ATTRIBUTES (deployment.environment, …) are
// carried onto every target's resource; service.namespace and
// service.name are set per target.
func New(ctx context.Context, endpoint string) (*Exporter, error) {
	if endpoint == "" {
		return nil, errors.New("otlpexport: no endpoint (OTEL_EXPORTER_OTLP_ENDPOINT)")
	}
	return &Exporter{ctx: ctx, endpoint: strings.TrimRight(endpoint, "/"), providers: map[string]*provider{}}, nil
}

// Gauge records the latest value of the target's metric.
func (e *Exporter) Gauge(t domain.Target, name string, value float64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	p, err := e.provider(t)
	if err != nil {
		return // reported by Flush, which cannot create the exporter either
	}
	full := t.MetricName(name)
	g, ok := p.gauges[full]
	if !ok {
		g, err = p.meter.Float64Gauge(full)
		if err != nil {
			return
		}
		p.gauges[full] = g
	}
	g.Record(e.ctx, value)
}

// Flush exports every provider's current gauges.
func (e *Exporter) Flush(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	var errs []error
	for _, p := range e.providers {
		errs = append(errs, p.mp.ForceFlush(ctx))
	}
	return errors.Join(errs...)
}

// Shutdown stops every provider.
func (e *Exporter) Shutdown(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	var errs []error
	for k, p := range e.providers {
		errs = append(errs, p.mp.Shutdown(ctx))
		delete(e.providers, k)
	}
	return errors.Join(errs...)
}

// provider returns (creating on first use) the provider for the target's
// <ns>/<svc>. Caller holds e.mu.
func (e *Exporter) provider(t domain.Target) (*provider, error) {
	key := t.Namespace + "/" + t.Service
	if p, ok := e.providers[key]; ok {
		return p, nil
	}
	res, err := resource.New(e.ctx,
		resource.WithFromEnv(),
		resource.WithAttributes(
			attribute.String(string(semconv.ServiceNamespaceKey), t.Namespace),
			attribute.String(string(semconv.ServiceNameKey), t.Service),
		),
	)
	if err != nil && !errors.Is(err, resource.ErrPartialResource) {
		return nil, err
	}
	exp, err := otlpmetrichttp.New(e.ctx, otlpmetrichttp.WithEndpointURL(e.endpoint+"/v1/metrics"))
	if err != nil {
		return nil, err
	}
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exp, sdkmetric.WithInterval(neverInterval))),
	)
	p := &provider{mp: mp, meter: mp.Meter(meterName), gauges: map[string]metric.Float64Gauge{}}
	e.providers[key] = p
	return p, nil
}
