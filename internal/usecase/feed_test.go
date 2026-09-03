package usecase_test

import (
	"errors"
	"testing"
	"time"

	"github.com/apcandsons/otellite/internal/domain"
	"github.com/apcandsons/otellite/internal/usecase"
)

func TestFeedDeliversSamplesAndAlertsToSubscribers(t *testing.T) {
	f := usecase.NewFeed(8)
	sub, cancel := f.Subscribe()
	defer cancel()

	s := domain.Sample{Time: t0, Value: "1", Unit: "By"}
	f.Ingest(memID, s)
	nt := domain.Notification{Rule: domain.Rule{Stream: memID}, Event: domain.Fired, Time: t0, Value: "1"}
	if err := f.Notify(nt); err != nil {
		t.Fatal(err)
	}

	got := <-sub
	if got.Stream != memID || got.Sample != s || got.Alert != nil {
		t.Errorf("first event = %+v", got)
	}
	got = <-sub
	if got.Alert == nil || *got.Alert != nt {
		t.Errorf("second event = %+v", got)
	}
}

func TestFeedDropsEventsForSlowSubscriber(t *testing.T) {
	f := usecase.NewFeed(1)
	sub, cancel := f.Subscribe()
	defer cancel()
	f.Ingest(memID, domain.Sample{Value: "1"})
	f.Ingest(memID, domain.Sample{Value: "2"}) // must not block
	if got := <-sub; got.Sample.Value != "1" {
		t.Errorf("got %+v", got)
	}
	select {
	case got := <-sub:
		t.Errorf("unexpected second event %+v", got)
	default:
	}
}

func TestFeedCancelStopsDelivery(t *testing.T) {
	f := usecase.NewFeed(1)
	sub, cancel := f.Subscribe()
	cancel()
	f.Ingest(memID, domain.Sample{Value: "1"})
	select {
	case ev, ok := <-sub:
		if ok {
			t.Errorf("delivered after cancel: %+v", ev)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("channel should be closed after cancel")
	}
	cancel() // idempotent
}

func TestNotifiersFanOutAndJoinErrors(t *testing.T) {
	boom := errors.New("boom")
	a, b := &fakeNotifier{}, &fakeNotifier{err: boom}
	ns := usecase.Notifiers{a, b}
	err := ns.Notify(domain.Notification{Value: "x"})
	if !errors.Is(err, boom) {
		t.Errorf("err = %v", err)
	}
	if len(a.sent) != 1 || len(b.sent) != 1 {
		t.Errorf("sent a=%d b=%d", len(a.sent), len(b.sent))
	}
}

func TestFeedCountsSubscribers(t *testing.T) {
	f := usecase.NewFeed(1)
	if f.Subscribers() != 0 {
		t.Fatal("fresh feed should have no subscribers")
	}
	_, cancel := f.Subscribe()
	if f.Subscribers() != 1 {
		t.Error("expected 1 subscriber")
	}
	cancel()
	if f.Subscribers() != 0 {
		t.Error("expected 0 after cancel")
	}
}
