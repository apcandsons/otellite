package usecase

import (
	"errors"
	"strconv"
	"sync"

	"github.com/apcandsons/otellite/internal/domain"
)

// Notifier delivers a notification to the channel named in its rule.
type Notifier interface {
	Notify(domain.Notification) error
}

// Alerter watches ingested samples against the configured rules. It is
// safe for concurrent use.
type Alerter struct {
	mu       sync.Mutex
	monitors map[domain.StreamID][]*domain.Monitor
	notifier Notifier
}

func NewAlerter(rules []domain.Rule, notifier Notifier) *Alerter {
	a := &Alerter{monitors: map[domain.StreamID][]*domain.Monitor{}, notifier: notifier}
	for _, r := range rules {
		a.monitors[r.Stream] = append(a.monitors[r.Stream], domain.NewMonitor(r))
	}
	return a
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

	var errs []error
	for _, nt := range pending {
		if err := a.notifier.Notify(nt); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
