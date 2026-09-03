package client

import (
	"context"
	"runtime"
	runtimemetrics "runtime/metrics"
	"sync"
	"time"

	"go.opentelemetry.io/otel/metric"
)

// registerProcess adds the common resource gauges every service reports:
// CPU utilization, Go memory in use, goroutines, and an uptime counter
// that doubles as a heartbeat for "absent" alert rules.
func registerProcess(m metric.Meter) {
	start := time.Now()
	cpu := &cpuSampler{lastWall: start}
	if c, ok := cpuTime(); ok {
		cpu.lastCPU = c
	}
	report := func(name string, err error) {
		if err != nil {
			Logger().Error("client: process gauge", "name", name, "err", err)
		}
	}

	_, err := m.Float64ObservableGauge("process.cpu.utilization",
		metric.WithUnit("1"), metric.WithDescription("CPU time used over wall time, across all CPUs, 0..1"),
		metric.WithFloat64Callback(func(_ context.Context, o metric.Float64Observer) error {
			o.Observe(cpu.utilization(time.Now()))
			return nil
		}))
	report("process.cpu.utilization", err)

	_, err = m.Int64ObservableGauge("go.memory.used",
		metric.WithUnit("By"), metric.WithDescription("Memory mapped by the Go runtime and not released to the OS"),
		metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
			o.Observe(memoryUsed())
			return nil
		}))
	report("go.memory.used", err)

	_, err = m.Int64ObservableGauge("go.goroutines",
		metric.WithUnit("1"), metric.WithDescription("Live goroutines"),
		metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
			o.Observe(int64(runtime.NumGoroutine()))
			return nil
		}))
	report("go.goroutines", err)

	_, err = m.Float64ObservableCounter("process.uptime",
		metric.WithUnit("s"), metric.WithDescription("Seconds since Start"),
		metric.WithFloat64Callback(func(_ context.Context, o metric.Float64Observer) error {
			o.Observe(time.Since(start).Seconds())
			return nil
		}))
	report("process.uptime", err)
}

// cpuSampler turns cumulative CPU time into utilization over the interval
// since the previous observation.
type cpuSampler struct {
	mu       sync.Mutex
	lastWall time.Time
	lastCPU  time.Duration
}

func (c *cpuSampler) utilization(now time.Time) float64 {
	cpu, ok := cpuTime()
	if !ok {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	wall := now.Sub(c.lastWall)
	used := cpu - c.lastCPU
	c.lastWall, c.lastCPU = now, cpu
	if wall <= 0 {
		return 0
	}
	u := used.Seconds() / (wall.Seconds() * float64(runtime.NumCPU()))
	return max(0, min(1, u))
}

// memoryUsed is what the runtime holds from the OS: total mapped memory
// minus heap pages already released.
func memoryUsed() int64 {
	samples := []runtimemetrics.Sample{
		{Name: "/memory/classes/total:bytes"},
		{Name: "/memory/classes/heap/released:bytes"},
	}
	runtimemetrics.Read(samples)
	if samples[0].Value.Kind() != runtimemetrics.KindUint64 || samples[1].Value.Kind() != runtimemetrics.KindUint64 {
		return 0
	}
	return int64(samples[0].Value.Uint64() - samples[1].Value.Uint64())
}
