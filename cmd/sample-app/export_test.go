package main

import (
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestCountersExportDeltaEverythingElseCumulative(t *testing.T) {
	want := map[sdkmetric.InstrumentKind]metricdata.Temporality{
		sdkmetric.InstrumentKindCounter:                 metricdata.DeltaTemporality,
		sdkmetric.InstrumentKindHistogram:               metricdata.DeltaTemporality,
		sdkmetric.InstrumentKindUpDownCounter:           metricdata.CumulativeTemporality,
		sdkmetric.InstrumentKindObservableCounter:       metricdata.CumulativeTemporality,
		sdkmetric.InstrumentKindObservableGauge:         metricdata.CumulativeTemporality,
		sdkmetric.InstrumentKindObservableUpDownCounter: metricdata.CumulativeTemporality,
	}
	for kind, w := range want {
		if got := temporality(kind); got != w {
			t.Errorf("%v: got %v, want %v", kind, got, w)
		}
	}
}
