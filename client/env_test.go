package client_test

import (
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/apcandsons/otellite/client"
)

func TestTemporalityCountersAndHistogramsAreDeltaEverythingElseCumulative(t *testing.T) {
	want := map[sdkmetric.InstrumentKind]metricdata.Temporality{
		sdkmetric.InstrumentKindCounter:                 metricdata.DeltaTemporality,
		sdkmetric.InstrumentKindHistogram:               metricdata.DeltaTemporality,
		sdkmetric.InstrumentKindUpDownCounter:           metricdata.CumulativeTemporality,
		sdkmetric.InstrumentKindObservableCounter:       metricdata.CumulativeTemporality,
		sdkmetric.InstrumentKindObservableGauge:         metricdata.CumulativeTemporality,
		sdkmetric.InstrumentKindObservableUpDownCounter: metricdata.CumulativeTemporality,
		sdkmetric.InstrumentKindGauge:                   metricdata.CumulativeTemporality,
	}
	for kind, w := range want {
		if got := client.Temporality(kind); got != w {
			t.Errorf("%v: got %v, want %v", kind, got, w)
		}
	}
}
