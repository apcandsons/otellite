package usecase

import (
	"errors"
	"sync"

	"github.com/apcandsons/otellite/internal/domain"
)

// Event is one item on the live feed: either a freshly ingested sample or
// an alert transition. Alert is nil for sample events.
type Event struct {
	Stream domain.StreamID
	Sample domain.Sample
	Alert  *domain.Notification
}

// Feed fans ingested samples and alert notifications out to live
// subscribers. Delivery is best effort: a subscriber that falls behind
// its buffer misses events rather than slowing ingestion down.
type Feed struct {
	mu     sync.Mutex
	buffer int
	subs   map[*subscriber]struct{}
}

type subscriber struct {
	ch     chan Event
	closed bool
}

func NewFeed(buffer int) *Feed {
	return &Feed{buffer: buffer, subs: map[*subscriber]struct{}{}}
}

// Subscribe returns a channel of future events and a cancel func that
// closes it. Cancel is safe to call more than once.
func (f *Feed) Subscribe() (<-chan Event, func()) {
	s := &subscriber{ch: make(chan Event, f.buffer)}
	f.mu.Lock()
	f.subs[s] = struct{}{}
	f.mu.Unlock()
	cancel := func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		if s.closed {
			return
		}
		s.closed = true
		delete(f.subs, s)
		close(s.ch)
	}
	return s.ch, cancel
}

// Subscribers reports how many live subscriptions exist.
func (f *Feed) Subscribers() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.subs)
}

// Ingest publishes a sample event.
func (f *Feed) Ingest(id domain.StreamID, s domain.Sample) {
	f.publish(Event{Stream: id, Sample: s})
}

// Notify publishes an alert event, so Feed can sit alongside a real
// notifier in a Notifiers list.
func (f *Feed) Notify(nt domain.Notification) error {
	f.publish(Event{Stream: nt.Rule.Stream, Sample: domain.Sample{Time: nt.Time, Value: nt.Value, Unit: nt.Unit}, Alert: &nt})
	return nil
}

func (f *Feed) publish(ev Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for s := range f.subs {
		select {
		case s.ch <- ev:
		default: // subscriber is behind; forgetting is a feature
		}
	}
}

// Notifiers delivers each notification to every notifier in turn.
type Notifiers []Notifier

func (ns Notifiers) Notify(nt domain.Notification) error {
	var errs []error
	for _, n := range ns {
		if err := n.Notify(nt); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
