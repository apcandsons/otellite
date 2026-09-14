package usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/apcandsons/otellite/internal/domain"
)

// MetricQuery is one CloudWatch datapoint request. ID is chosen by the
// caller and keys the result; a missing key means the metric had no
// datapoint in the window.
type MetricQuery struct {
	ID         string
	Namespace  string
	Metric     string
	Stat       string
	Dimensions map[string]string
}

// CloudMetrics fetches the latest value of every query in one round trip.
type CloudMetrics interface {
	Fetch(ctx context.Context, queries []MetricQuery) (map[string]float64, error)
}

// Counts is an ECS service's task counts.
type Counts struct {
	Running, Desired, Pending int
}

// ServiceCounts reads an ECS service's task counts; an unknown service is
// an error.
type ServiceCounts interface {
	Counts(ctx context.Context, cluster, service string) (Counts, error)
}

// GaugeExporter records one sample per (target, metric name) and ships the
// batch on Flush. name is the bare metric name; the exporter applies the
// target's prefix and path.
type GaugeExporter interface {
	Gauge(target domain.Target, name string, value float64)
	Flush(ctx context.Context) error
}

// cloudMetric describes one CloudWatch-backed sample of a target kind.
type cloudMetric struct {
	name         string // exported as <prefix>.<name>
	namespace    string
	metric       string
	stat         string
	zeroIfAbsent bool // export 0 when CloudWatch has no datapoint (idle queues)
}

var cloudMetrics = map[domain.TargetKind][]cloudMetric{
	domain.TargetECS: {
		{name: "cpu.utilization", namespace: "AWS/ECS", metric: "CPUUtilization", stat: "Average"},
		{name: "memory.utilization", namespace: "AWS/ECS", metric: "MemoryUtilization", stat: "Average"},
	},
	domain.TargetSQS: {
		{name: "visible", namespace: "AWS/SQS", metric: "ApproximateNumberOfMessagesVisible", stat: "Maximum", zeroIfAbsent: true},
		{name: "in_flight", namespace: "AWS/SQS", metric: "ApproximateNumberOfMessagesNotVisible", stat: "Maximum", zeroIfAbsent: true},
		{name: "oldest_age", namespace: "AWS/SQS", metric: "ApproximateAgeOfOldestMessage", stat: "Maximum", zeroIfAbsent: true},
	},
	domain.TargetRDS: {
		{name: "cpu.utilization", namespace: "AWS/RDS", metric: "CPUUtilization", stat: "Average"},
		{name: "free_storage", namespace: "AWS/RDS", metric: "FreeStorageSpace", stat: "Average"},
		{name: "connections", namespace: "AWS/RDS", metric: "DatabaseConnections", stat: "Average"},
		{name: "freeable_memory", namespace: "AWS/RDS", metric: "FreeableMemory", stat: "Average"},
	},
}

// dimensions names the CloudWatch dimension(s) that identify a target.
func dimensions(t domain.Target) map[string]string {
	switch t.Kind {
	case domain.TargetECS:
		c, s := t.ECSParts()
		return map[string]string{"ClusterName": c, "ServiceName": s}
	case domain.TargetSQS:
		return map[string]string{"QueueName": t.ID}
	case domain.TargetRDS:
		return map[string]string{"DBInstanceIdentifier": t.ID}
	}
	return nil
}

// queryID keys a target's metric in the batched fetch.
func queryID(t domain.Target, name string) string { return t.Path() + "/" + name }

// Bridge polls AWS for every target and exports the samples as gauges.
type Bridge struct {
	targets  []domain.Target
	metrics  CloudMetrics
	counts   ServiceCounts
	exporter GaugeExporter
	log      *slog.Logger
}

// NewBridge wires the ports. A nil log means slog.Default().
func NewBridge(targets []domain.Target, metrics CloudMetrics, counts ServiceCounts, exporter GaugeExporter, log *slog.Logger) *Bridge {
	if log == nil {
		log = slog.Default()
	}
	return &Bridge{targets: targets, metrics: metrics, counts: counts, exporter: exporter, log: log}
}

// Tick takes one sample of every target: ECS task counts per service
// (a target whose service cannot be described is logged and skipped), then
// one batched CloudWatch fetch for everything else. A failed fetch is
// returned after the counts have been exported, so liveness signals survive
// a CloudWatch outage; nothing is invented for metrics that were not read.
func (b *Bridge) Tick(ctx context.Context) error {
	live := b.targets[:0:0]
	for _, t := range b.targets {
		if t.Kind == domain.TargetECS {
			cluster, service := t.ECSParts()
			c, err := b.counts.Counts(ctx, cluster, service)
			if err != nil {
				b.log.Warn("awsbridge: skipping target", "target", t.Path(), "id", t.ID, "err", err)
				continue
			}
			b.exporter.Gauge(t, "running_tasks", float64(c.Running))
			b.exporter.Gauge(t, "desired_tasks", float64(c.Desired))
			b.exporter.Gauge(t, "pending_tasks", float64(c.Pending))
		}
		live = append(live, t)
	}

	var queries []MetricQuery
	for _, t := range live {
		for _, m := range cloudMetrics[t.Kind] {
			queries = append(queries, MetricQuery{ID: queryID(t, m.name), Namespace: m.namespace, Metric: m.metric, Stat: m.stat, Dimensions: dimensions(t)})
		}
	}
	var fetchErr error
	values := map[string]float64{}
	if len(queries) > 0 {
		v, err := b.metrics.Fetch(ctx, queries)
		if err != nil {
			fetchErr = fmt.Errorf("awsbridge: cloudwatch: %w", err)
		} else {
			values = v
		}
	}
	if fetchErr == nil {
		for _, t := range live {
			for _, m := range cloudMetrics[t.Kind] {
				v, ok := values[queryID(t, m.name)]
				if !ok {
					if !m.zeroIfAbsent {
						continue
					}
					v = 0
				}
				b.exporter.Gauge(t, m.name, v)
			}
		}
	}
	return errors.Join(fetchErr, b.exporter.Flush(ctx))
}
