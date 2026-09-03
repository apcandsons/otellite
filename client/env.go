package client

import (
	"os"
	"strconv"
	"strings"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

const (
	defaultInterval = 60 * time.Second
	defaultScope    = "default"
)

// Temporality makes counters and histograms export the count since the
// previous export instead of a lifetime total, so a sample reads as
// "events per interval" and threshold alerts on it can resolve.
// Everything else stays cumulative.
func Temporality(kind sdkmetric.InstrumentKind) metricdata.Temporality {
	switch kind {
	case sdkmetric.InstrumentKindCounter, sdkmetric.InstrumentKindHistogram:
		return metricdata.DeltaTemporality
	}
	return metricdata.CumulativeTemporality
}

// resolveInterval prefers the option, then OTEL_METRIC_EXPORT_INTERVAL
// (milliseconds), then 60 s.
func resolveInterval(d time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	if ms, err := strconv.Atoi(os.Getenv("OTEL_METRIC_EXPORT_INTERVAL")); err == nil && ms > 0 {
		return time.Duration(ms) * time.Millisecond
	}
	return defaultInterval
}

// resolveScope prefers the option, then OTEL_SERVICE_NAME, then "default",
// and makes the result usable as a path segment.
func resolveScope(s string) string {
	if s == "" {
		s = os.Getenv("OTEL_SERVICE_NAME")
	}
	if s == "" {
		s = defaultScope
	}
	return strings.ReplaceAll(s, "/", "-")
}
