package main

import (
	"context"
	"testing"
	"time"

	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func gaugeNames(t *testing.T, reader *sdkmetric.ManualReader) map[string]bool {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			out[m.Name] = true
		}
	}
	return out
}

func TestPausedSimulatorStopsReportingGauges(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	defer mp.Shutdown(context.Background())
	lp := sdklog.NewLoggerProvider()
	sim, err := newSimulator(mp.Meter(scope), lp.Logger(scope), newBurst(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	sim.request(context.Background())

	got := gaugeNames(t, reader)
	for _, name := range []string{"go.memory.used", "process.cpu.utilization", "http.server.requests", "process.uptime"} {
		if !got[name] {
			t.Errorf("%s missing while running: %v", name, got)
		}
	}

	sim.togglePause()
	sim.request(context.Background())
	got = gaugeNames(t, reader)
	if got["go.memory.used"] || got["process.cpu.utilization"] {
		t.Errorf("gauges should be absent while paused: %v", got)
	}
	if !got["http.server.requests"] || !got["process.uptime"] {
		t.Errorf("counters should keep reporting while paused: %v", got)
	}

	sim.togglePause()
	if got = gaugeNames(t, reader); !got["process.cpu.utilization"] {
		t.Errorf("gauges should return after resume: %v", got)
	}
}
