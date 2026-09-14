package usecase

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sort"
	"strings"
	"testing"

	"github.com/apcandsons/otellite/internal/domain"
)

type fakeMetrics struct {
	queries []MetricQuery
	values  map[string]float64
	err     error
}

func (f *fakeMetrics) Fetch(_ context.Context, qs []MetricQuery) (map[string]float64, error) {
	f.queries = append(f.queries, qs...)
	return f.values, f.err
}

type fakeCounts struct {
	counts map[string]Counts // key cluster/service
	err    error
}

func (f *fakeCounts) Counts(_ context.Context, cluster, service string) (Counts, error) {
	if f.err != nil {
		return Counts{}, f.err
	}
	c, ok := f.counts[cluster+"/"+service]
	if !ok {
		return Counts{}, errors.New("no such service")
	}
	return c, nil
}

type fakeExporter struct {
	gauges  map[string]float64 // "<path>:<metric>" -> value
	flushed int
}

func (f *fakeExporter) Gauge(t domain.Target, name string, v float64) {
	if f.gauges == nil {
		f.gauges = map[string]float64{}
	}
	f.gauges[t.Path()+":"+t.MetricName(name)] = v
}

func (f *fakeExporter) Flush(context.Context) error { f.flushed++; return nil }

var (
	ecsTarget = domain.Target{Kind: domain.TargetECS, ID: "comm-staging/comm-bff", Namespace: "comm", Service: "comm-bff", Prefix: "ecs"}
	sqsTarget = domain.Target{Kind: domain.TargetSQS, ID: "comm-staging-embed-dlq", Namespace: "comm", Service: "queues", Prefix: "embed_dlq"}
	rdsTarget = domain.Target{Kind: domain.TargetRDS, ID: "comm-staging-db", Namespace: "comm", Service: "comm-sor", Prefix: "rds"}
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func keys(m map[string]float64) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestTickBatchesEveryCloudWatchQueryIntoOneFetch(t *testing.T) {
	m := &fakeMetrics{values: map[string]float64{}}
	b := NewBridge([]domain.Target{ecsTarget, sqsTarget, rdsTarget}, m, &fakeCounts{counts: map[string]Counts{"comm-staging/comm-bff": {}}}, &fakeExporter{}, quiet())
	if err := b.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	// 2 ecs + 3 sqs + 4 rds = 9 queries, all in one call.
	if len(m.queries) != 9 {
		t.Fatalf("want 9 queries in one fetch, got %d", len(m.queries))
	}
	byID := map[string]MetricQuery{}
	for _, q := range m.queries {
		byID[q.ID] = q
	}
	cpu := byID[ecsTarget.Path()+"/cpu.utilization"]
	if cpu.Namespace != "AWS/ECS" || cpu.Metric != "CPUUtilization" || cpu.Stat != "Average" || cpu.Dimensions["ClusterName"] != "comm-staging" || cpu.Dimensions["ServiceName"] != "comm-bff" {
		t.Errorf("ecs cpu query: %+v", cpu)
	}
	vis := byID[sqsTarget.Path()+"/visible"]
	if vis.Namespace != "AWS/SQS" || vis.Metric != "ApproximateNumberOfMessagesVisible" || vis.Stat != "Maximum" || vis.Dimensions["QueueName"] != "comm-staging-embed-dlq" {
		t.Errorf("sqs visible query: %+v", vis)
	}
	stor := byID[rdsTarget.Path()+"/free_storage"]
	if stor.Namespace != "AWS/RDS" || stor.Metric != "FreeStorageSpace" || stor.Stat != "Average" || stor.Dimensions["DBInstanceIdentifier"] != "comm-staging-db" {
		t.Errorf("rds storage query: %+v", stor)
	}
}

func TestTickExportsCountsAndDatapointsUnderPrefixedNames(t *testing.T) {
	m := &fakeMetrics{values: map[string]float64{
		ecsTarget.Path() + "/cpu.utilization":    12.5,
		ecsTarget.Path() + "/memory.utilization": 40,
		sqsTarget.Path() + "/visible":            3,
		sqsTarget.Path() + "/in_flight":          1,
		sqsTarget.Path() + "/oldest_age":         120,
		rdsTarget.Path() + "/connections":        7,
	}}
	c := &fakeCounts{counts: map[string]Counts{"comm-staging/comm-bff": {Running: 1, Desired: 1, Pending: 0}}}
	e := &fakeExporter{}
	b := NewBridge([]domain.Target{ecsTarget, sqsTarget, rdsTarget}, m, c, e, quiet())
	if err := b.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := map[string]float64{
		"comm/comm-bff/ecs:ecs.running_tasks":        1,
		"comm/comm-bff/ecs:ecs.desired_tasks":        1,
		"comm/comm-bff/ecs:ecs.pending_tasks":        0,
		"comm/comm-bff/ecs:ecs.cpu.utilization":      12.5,
		"comm/comm-bff/ecs:ecs.memory.utilization":   40,
		"comm/queues/embed_dlq:embed_dlq.visible":    3,
		"comm/queues/embed_dlq:embed_dlq.in_flight":  1,
		"comm/queues/embed_dlq:embed_dlq.oldest_age": 120,
		"comm/comm-sor/rds:rds.connections":          7,
		// rds cpu/free_storage/freeable_memory had no datapoint: skipped.
	}
	if got := keys(e.gauges); strings.Join(got, ",") != strings.Join(keys(want), ",") {
		t.Fatalf("exported %v\nwant %v", got, keys(want))
	}
	for k, v := range want {
		if e.gauges[k] != v {
			t.Errorf("%s = %v, want %v", k, e.gauges[k], v)
		}
	}
	if e.flushed != 1 {
		t.Fatalf("flushed %d times, want 1", e.flushed)
	}
}

func TestTickExportsZeroForAnIdleQueue(t *testing.T) {
	e := &fakeExporter{}
	b := NewBridge([]domain.Target{sqsTarget}, &fakeMetrics{values: map[string]float64{}}, &fakeCounts{}, e, quiet())
	if err := b.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"visible", "in_flight", "oldest_age"} {
		k := sqsTarget.Path() + ":" + sqsTarget.MetricName(n)
		if v, ok := e.gauges[k]; !ok || v != 0 {
			t.Errorf("%s: got %v (present=%v), want 0", k, v, ok)
		}
	}
}

func TestTickSkipsATargetWhoseServiceIsMissing(t *testing.T) {
	missing := domain.Target{Kind: domain.TargetECS, ID: "comm-staging/gone", Namespace: "comm", Service: "gone", Prefix: "ecs"}
	m := &fakeMetrics{values: map[string]float64{missing.Path() + "/cpu.utilization": 1}}
	c := &fakeCounts{counts: map[string]Counts{"comm-staging/comm-bff": {Running: 1, Desired: 1}}}
	e := &fakeExporter{}
	b := NewBridge([]domain.Target{missing, ecsTarget}, m, c, e, quiet())
	if err := b.Tick(context.Background()); err != nil {
		t.Fatalf("a missing service must not fail the tick: %v", err)
	}
	if _, ok := e.gauges["comm/gone/ecs:ecs.cpu.utilization"]; ok {
		t.Error("the missing target must be skipped entirely")
	}
	if e.gauges["comm/comm-bff/ecs:ecs.running_tasks"] != 1 {
		t.Error("the healthy target must still be exported")
	}
}

func TestTickReportsAFetchErrorButStillExportsCounts(t *testing.T) {
	m := &fakeMetrics{err: errors.New("throttled")}
	c := &fakeCounts{counts: map[string]Counts{"comm-staging/comm-bff": {Running: 2, Desired: 2}}}
	e := &fakeExporter{}
	b := NewBridge([]domain.Target{ecsTarget, sqsTarget}, m, c, e, quiet())
	err := b.Tick(context.Background())
	if err == nil || !strings.Contains(err.Error(), "throttled") {
		t.Fatalf("want the fetch error, got %v", err)
	}
	if e.gauges["comm/comm-bff/ecs:ecs.running_tasks"] != 2 {
		t.Error("counts must be exported even when CloudWatch fails")
	}
	if _, ok := e.gauges["comm/queues/embed_dlq:embed_dlq.visible"]; ok {
		t.Error("no datapoints may be invented when the fetch failed")
	}
	if e.flushed != 1 {
		t.Errorf("flushed %d, want 1", e.flushed)
	}
}
