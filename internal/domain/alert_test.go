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
		want  domain.Event
	}{
		{0, 150, domain.NoEvent}, // breach starts
		{2, 150, domain.NoEvent}, // not yet 3 minutes
		{3, 150, domain.Fired},   // sustained for 3 minutes: fire once
		{4, 150, domain.NoEvent}, // still breached, do not re-fire
		{5, 50, domain.Resolved}, // recovered after firing
		{5, 50, domain.NoEvent},  // still recovered, do not re-resolve
		{6, 150, domain.NoEvent}, // new episode begins
		{7, 50, domain.NoEvent},  // short blip that never fired: nothing to resolve
		{8, 150, domain.NoEvent},
		{11, 150, domain.Fired},    // fires again for the new episode
		{11, 100, domain.Resolved}, // exactly threshold is not "over"
	}
	for _, s := range steps {
		if got := m.Observe(at(s.min), s.value); got != s.want {
			t.Errorf("at +%dm value %v: event = %v, want %v", s.min, s.value, got, s.want)
		}
	}
}

func TestMonitorZeroForFiresImmediately(t *testing.T) {
	m := domain.NewMonitor(domain.Rule{Op: domain.OpLess, Threshold: 1})
	if m.Observe(time.Now(), 0) != domain.Fired {
		t.Error("should fire on first breach when For is zero")
	}
}

func TestMonitorReportsFiringState(t *testing.T) {
	m := domain.NewMonitor(domain.Rule{Op: domain.OpGreater, Threshold: 1, For: time.Minute})
	t0 := time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC)
	if m.Firing() {
		t.Fatal("new monitor should not be firing")
	}
	m.Observe(t0, 5)
	if m.Firing() {
		t.Error("breach shorter than For should not be firing")
	}
	m.Observe(t0.Add(time.Minute), 5)
	if !m.Firing() {
		t.Error("should be firing after sustained breach")
	}
	m.Observe(t0.Add(2*time.Minute), 0)
	if m.Firing() {
		t.Error("should stop firing after recovery")
	}
}

func TestAbsentOpParsesAndNeverHolds(t *testing.T) {
	op, ok := domain.ParseOp("absent")
	if !ok || op != domain.OpAbsent || op.String() != "absent" {
		t.Fatalf("ParseOp(absent) = %v %v", op, ok)
	}
	if op.Holds(1, 0) {
		t.Error("absent must not hold on any value")
	}
}

func TestAbsentMonitorFiresOnSilenceAndResolvesOnSample(t *testing.T) {
	m := domain.NewMonitor(domain.Rule{Op: domain.OpAbsent, For: 30 * time.Second})
	t0 := time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC)
	at := func(sec int) time.Time { return t0.Add(time.Duration(sec) * time.Second) }

	if got := m.Check(at(0)); got != domain.NoEvent { // first check arms the clock
		t.Errorf("check at +0 = %v", got)
	}
	if got := m.Observe(at(10), 0.5); got != domain.NoEvent {
		t.Errorf("observe at +10 = %v", got)
	}
	if got := m.Check(at(39)); got != domain.NoEvent { // 29s since last sample
		t.Errorf("check at +39 = %v", got)
	}
	if got := m.Check(at(40)); got != domain.Fired {
		t.Errorf("check at +40 = %v, want Fired", got)
	}
	if got := m.Check(at(50)); got != domain.NoEvent { // fires once
		t.Errorf("check at +50 = %v", got)
	}
	if !m.Firing() {
		t.Error("should be firing")
	}
	if got := m.Observe(at(60), 0.5); got != domain.Resolved {
		t.Errorf("observe at +60 = %v, want Resolved", got)
	}
	if got := m.Check(at(89)); got != domain.NoEvent {
		t.Errorf("check at +89 = %v", got)
	}
	if got := m.Check(at(90)); got != domain.Fired { // re-arms for a new silence
		t.Errorf("check at +90 = %v, want Fired", got)
	}
}

func TestAbsentMonitorFiresIfStreamNeverReports(t *testing.T) {
	m := domain.NewMonitor(domain.Rule{Op: domain.OpAbsent, For: time.Minute})
	t0 := time.Now()
	m.Check(t0)
	if m.Check(t0.Add(time.Minute)) != domain.Fired {
		t.Error("should fire one For after the first check when nothing ever arrives")
	}
}

func TestThresholdMonitorIgnoresCheck(t *testing.T) {
	m := domain.NewMonitor(domain.Rule{Op: domain.OpGreater, Threshold: 1, For: time.Second})
	if m.Check(time.Now().Add(time.Hour)) != domain.NoEvent {
		t.Error("threshold rules are driven by samples only")
	}
}
