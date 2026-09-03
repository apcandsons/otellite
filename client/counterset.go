package client

import (
	"context"
	"errors"
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// CounterSet is one delta counter per dimension value, named
// "<prefix>.<name>". OTel Lite drops attributes, so this is how a
// "reason" or "outcome" dimension becomes browsable streams.
type CounterSet struct {
	counters map[string]metric.Int64Counter
}

// NewCounterSet creates the counters up front so a typo in a name fails
// at startup rather than silently at the first Add.
func NewCounterSet(prefix string, names ...string) (*CounterSet, error) {
	if prefix == "" {
		return nil, errors.New("client: counter set needs a prefix")
	}
	m := Meter()
	cs := &CounterSet{counters: make(map[string]metric.Int64Counter, len(names))}
	for _, name := range names {
		if name == "" {
			return nil, fmt.Errorf("client: counter set %q: empty name", prefix)
		}
		c, err := m.Int64Counter(prefix+"."+name, metric.WithUnit("1"))
		if err != nil {
			return nil, fmt.Errorf("client: counter set %q: %w", prefix, err)
		}
		cs.counters[name] = c
	}
	return cs, nil
}

// Add increments the counter for name. Unknown names (and a nil set) are
// ignored, so a hot path never has to check.
func (c *CounterSet) Add(ctx context.Context, name string, n int64) {
	if c == nil {
		return
	}
	if ctr, ok := c.counters[name]; ok {
		ctr.Add(ctx, n)
	}
}
