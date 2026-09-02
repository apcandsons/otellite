package domain_test

import (
	"testing"
	"time"

	"github.com/apcandsons/otellite/internal/domain"
)

func TestOpCompare(t *testing.T) {
	cases := []struct {
		op   domain.Op
		v, x float64
		want bool
	}{
		{domain.OpGreater, 5, 4, true}, {domain.OpGreater, 4, 4, false},
		{domain.OpGreaterEq, 4, 4, true}, {domain.OpLess, 3, 4, true},
		{domain.OpLess, 4, 4, false}, {domain.OpLessEq, 4, 4, true},
	}
	for _, c := range cases {
		if got := c.op.Holds(c.v, c.x); got != c.want {
			t.Errorf("%s.Holds(%v, %v) = %v", c.op, c.v, c.x, got)
		}
	}
	for s, want := range map[string]domain.Op{">": domain.OpGreater, ">=": domain.OpGreaterEq, "<": domain.OpLess, "<=": domain.OpLessEq} {
		if got, ok := domain.ParseOp(s); !ok || got != want {
			t.Errorf("ParseOp(%q) = %v %v", s, got, ok)
		}
	}
	if _, ok := domain.ParseOp("=="); ok {
		t.Error("== should not parse")
	}
}

func TestMonitorFiresAfterSustainedBreach(t *testing.T) {
	rule := domain.Rule{
		Stream:    domain.StreamID{Namespace: "iam", Service: "iam-api", Kind: domain.Metrics, Name: "mem"},
		Op:        domain.OpGreater,
		Threshold: 100,
		For:       3 * time.Minute,
		Channel:   "ops",
	}
	m := domain.NewMonitor(rule)
	base := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	at := func(min int) time.Time { return base.Add(time.Duration(min) * time.Minute) }

	steps := []struct {
		min   int
		value float64
		fire  bool
	}{
		{0, 150, false}, // breach starts
		{2, 150, false}, // not yet 3 minutes
		{3, 150, true},  // sustained for 3 minutes: fire once
		{4, 150, false}, // still breached, do not re-fire
		{5, 50, false},  // recovered
		{6, 150, false}, // new episode begins
		{8, 150, false},
		{9, 150, true},  // fires again for the new episode
		{9, 100, false}, // exactly threshold is not "over"
	}
	for _, s := range steps {
		if got := m.Observe(at(s.min), s.value); got != s.fire {
			t.Errorf("at +%dm value %v: fire = %v, want %v", s.min, s.value, got, s.fire)
		}
	}
}

func TestMonitorZeroForFiresImmediately(t *testing.T) {
	m := domain.NewMonitor(domain.Rule{Op: domain.OpLess, Threshold: 1})
	if !m.Observe(time.Now(), 0) {
		t.Error("should fire on first breach when For is zero")
	}
}
