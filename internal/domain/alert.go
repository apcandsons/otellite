package domain

import "time"

// Op is a threshold comparison.
type Op int

const (
	OpGreater Op = iota + 1
	OpGreaterEq
	OpLess
	OpLessEq
	// OpAbsent fires when the stream has had no samples for Rule.For. It
	// has no threshold and is evaluated by Monitor.Check on a clock rather
	// than by samples.
	OpAbsent
)

var opNames = map[Op]string{OpGreater: ">", OpGreaterEq: ">=", OpLess: "<", OpLessEq: "<=", OpAbsent: "absent"}

func (o Op) String() string { return opNames[o] }

// ParseOp maps a comparison symbol to an Op.
func ParseOp(s string) (Op, bool) {
	for op, name := range opNames {
		if name == s {
			return op, true
		}
	}
	return 0, false
}

// Holds reports whether "value op threshold" is true.
func (o Op) Holds(value, threshold float64) bool {
	switch o {
	case OpGreater:
		return value > threshold
	case OpGreaterEq:
		return value >= threshold
	case OpLess:
		return value < threshold
	case OpLessEq:
		return value <= threshold
	}
	return false
}

// Rule says: when Stream has been "Op Threshold" continuously for at least
// For, notify Channel. With OpAbsent: when Stream has been silent for For.
type Rule struct {
	Stream    StreamID
	Op        Op
	Threshold float64
	For       time.Duration
	Channel   string
}

// Event is what a Monitor reports after observing a sample.
type Event int

const (
	NoEvent  Event = iota
	Fired          // the breach has been sustained for Rule.For
	Resolved       // the stream recovered after Fired
)

var eventNames = map[Event]string{NoEvent: "none", Fired: "fired", Resolved: "resolved"}

func (e Event) String() string { return eventNames[e] }

// Monitor tracks one rule across successive samples. It fires once per
// breach episode, reports Resolved when a fired episode ends, and re-arms.
// Threshold rules are driven by Observe; absent rules by Check, with
// Observe marking the stream as alive.
type Monitor struct {
	Rule     Rule
	since    time.Time // threshold rules: start of the current breach
	lastSeen time.Time // absent rules: last sample, or the first Check
	fired    bool
}

func NewMonitor(rule Rule) *Monitor { return &Monitor{Rule: rule} }

// Firing reports whether the monitor has fired and not yet resolved.
func (m *Monitor) Firing() bool { return m.fired }

// Observe feeds one sample and reports the resulting event, if any.
func (m *Monitor) Observe(t time.Time, value float64) Event {
	if m.Rule.Op == OpAbsent {
		m.lastSeen = t
		if m.fired {
			m.fired = false
			return Resolved
		}
		return NoEvent
	}
	if !m.Rule.Op.Holds(value, m.Rule.Threshold) {
		wasFired := m.fired
		m.since, m.fired = time.Time{}, false
		if wasFired {
			return Resolved
		}
		return NoEvent
	}
	if m.since.IsZero() {
		m.since = t
	}
	if m.fired || t.Sub(m.since) < m.Rule.For {
		return NoEvent
	}
	m.fired = true
	return Fired
}

// Check evaluates an absent rule against the clock. The first call arms
// it, so a stream that never reports fires one For later. Threshold rules
// ignore Check.
func (m *Monitor) Check(now time.Time) Event {
	if m.Rule.Op != OpAbsent {
		return NoEvent
	}
	if m.lastSeen.IsZero() {
		m.lastSeen = now
		return NoEvent
	}
	if m.fired || now.Sub(m.lastSeen) < m.Rule.For {
		return NoEvent
	}
	m.fired = true
	return Fired
}

// Notification is what gets sent to a channel when a rule fires or resolves.
// For absent rules Value is empty.
// Time, Value and Unit describe the sample that triggered the event.
type Notification struct {
	Rule  Rule
	Event Event
	Time  time.Time
	Value string
	Unit  string
}
