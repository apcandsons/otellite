package usecase

import (
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/apcandsons/otellite/internal/domain"
)

// Notifier delivers a notification to the channel named in its rule.
type Notifier interface {
	Notify(domain.Notification) error
}

// RuleStatus is a rule together with whether it is currently firing.
type RuleStatus struct {
	Rule   domain.Rule
	Firing bool
}

// Alerter watches ingested samples against the configured rules. It is
// safe for concurrent use.
type Alerter struct {
	mu       sync.Mutex
	ordered  []*domain.Monitor // configuration order, for Status
	monitors map[domain.StreamID][]*domain.Monitor
	notifier Notifier
}

func NewAlerter(rules []domain.Rule, notifier Notifier) *Alerter {
	a := &Alerter{monitors: map[domain.StreamID][]*domain.Monitor{}, notifier: notifier}
	for _, r := range rules {
		m := domain.NewMonitor(r)
		a.ordered = append(a.ordered, m)
		a.monitors[r.Stream] = append(a.monitors[r.Stream], m)
	}
	return a
}

// Status reports every rule in configuration order with its firing state.
func (a *Alerter) Status() []RuleStatus {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]RuleStatus, 0, len(a.ordered))
	for _, m := range a.ordered {
		out = append(out, RuleStatus{Rule: m.Rule, Firing: m.Firing()})
	}
	return out
}

// Check evaluates absent rules against the clock and sends a notification
// for each that fires. Call it periodically.
func (a *Alerter) Check(now time.Time) error {
	var pending []domain.Notification
	a.mu.Lock()
	for _, m := range a.ordered {
		if ev := m.Check(now); ev != domain.NoEvent {
			pending = append(pending, domain.Notification{Rule: m.Rule, Event: ev, Time: now})
		}
	}
	a.mu.Unlock()
	return a.send(pending)
}

func (a *Alerter) send(pending []domain.Notification) error {
	var errs []error
	for _, nt := range pending {
		if err := a.notifier.Notify(nt); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Observe feeds one sample to every rule on its stream and sends a
// notification for each rule that fires or resolves. Non-numeric samples
// are ignored.
func (a *Alerter) Observe(id domain.StreamID, s domain.Sample) error {
	monitors := a.monitors[id]
	if len(monitors) == 0 {
		return nil
	}
	value, err := strconv.ParseFloat(s.Value, 64)
	if err != nil {
		return nil
	}
	var pending []domain.Notification
	a.mu.Lock()
	for _, m := range monitors {
		if ev := m.Observe(s.Time, value); ev != domain.NoEvent {
			pending = append(pending, domain.Notification{Rule: m.Rule, Event: ev, Time: s.Time, Value: s.Value, Unit: s.Unit})
		}
	}
	a.mu.Unlock()
	return a.send(pending)
}
