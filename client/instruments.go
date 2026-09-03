package client

import (
	"context"

	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
)

// Counter returns a delta counter from Meter. Create it once and keep the
// handle. Creation errors are logged and yield a no-op instrument.
func Counter(name, unit, desc string) metric.Int64Counter {
	c, err := Meter().Int64Counter(name, metric.WithUnit(unit), metric.WithDescription(desc))
	if err != nil {
		Logger().Error("client: counter", "name", name, "err", err)
		return noop.Int64Counter{}
	}
	return c
}

// Histogram returns a delta histogram from Meter; the SoR shows it as
// <name>.count and <name>.sum.
func Histogram(name, unit, desc string) metric.Float64Histogram {
	h, err := Meter().Float64Histogram(name, metric.WithUnit(unit), metric.WithDescription(desc))
	if err != nil {
		Logger().Error("client: histogram", "name", name, "err", err)
		return noop.Float64Histogram{}
	}
	return h
}

// UpDown returns a cumulative up/down counter from Meter, for in-flight
// style gauges the code adjusts itself.
func UpDown(name, unit, desc string) metric.Int64UpDownCounter {
	u, err := Meter().Int64UpDownCounter(name, metric.WithUnit(unit), metric.WithDescription(desc))
	if err != nil {
		Logger().Error("client: updown", "name", name, "err", err)
		return noop.Int64UpDownCounter{}
	}
	return u
}

// Gauge registers an observable gauge that calls fn at every export.
// Errors are logged, never panicked.
func Gauge(name, unit, desc string, fn func() float64) {
	_, err := Meter().Float64ObservableGauge(name, metric.WithUnit(unit), metric.WithDescription(desc),
		metric.WithFloat64Callback(func(_ context.Context, o metric.Float64Observer) error {
			o.Observe(fn())
			return nil
		}))
	if err != nil {
		Logger().Error("client: gauge", "name", name, "err", err)
	}
}

// millis renders a duration as the SoR's histogram unit.
func millis(d durationLike) float64 { return float64(d.Nanoseconds()) / 1e6 }

type durationLike interface{ Nanoseconds() int64 }
