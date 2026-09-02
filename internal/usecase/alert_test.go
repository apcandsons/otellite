package usecase_test

import (
	"errors"
	"testing"
	"time"

	"github.com/apcandsons/otellite/internal/domain"
	"github.com/apcandsons/otellite/internal/usecase"
)

type fakeNotifier struct {
	sent []domain.Notification
	err  error
}

func (f *fakeNotifier) Notify(n domain.Notification) error {
	f.sent = append(f.sent, n)
	return f.err
}

func TestAlerterNotifiesOnSustainedBreach(t *testing.T) {
	nf := &fakeNotifier{}
	rule := domain.Rule{Stream: memID, Op: domain.OpGreater, Threshold: 100, For: 3 * time.Minute, Channel: "ops"}
	al := usecase.NewAlerter([]domain.Rule{rule}, nf)

	at := func(min int) time.Time { return t0.Add(time.Duration(min) * time.Minute) }
	for _, step := range []struct {
		min int
		v   string
	}{{0, "150"}, {2, "150"}, {3, "160"}, {4, "170"}} {
		if err := al.Observe(memID, domain.Sample{Time: at(step.min), Value: step.v, Unit: "Bytes"}); err != nil {
			t.Fatal(err)
		}
	}
	if len(nf.sent) != 1 {
		t.Fatalf("sent %d notifications, want 1: %+v", len(nf.sent), nf.sent)
	}
	n := nf.sent[0]
	if n.Rule != rule || n.Value != "160" || n.Unit != "Bytes" || !n.Time.Equal(at(3)) {
		t.Errorf("notification = %+v", n)
	}
}

func TestAlerterIgnoresOtherStreamsAndNonNumbers(t *testing.T) {
	nf := &fakeNotifier{}
	al := usecase.NewAlerter([]domain.Rule{{Stream: memID, Op: domain.OpGreater, Threshold: 0}}, nf)
	if err := al.Observe(webID, domain.Sample{Time: t0, Value: "5"}); err != nil {
		t.Fatal(err)
	}
	if err := al.Observe(logID, domain.Sample{Time: t0, Value: "ERROR boom"}); err != nil {
		t.Fatal(err)
	}
	if err := al.Observe(memID, domain.Sample{Time: t0, Value: "not-a-number"}); err != nil {
		t.Fatal(err)
	}
	if len(nf.sent) != 0 {
		t.Errorf("unexpected notifications: %+v", nf.sent)
	}
}

func TestAlerterReturnsNotifierError(t *testing.T) {
	boom := errors.New("webhook down")
	nf := &fakeNotifier{err: boom}
	al := usecase.NewAlerter([]domain.Rule{{Stream: memID, Op: domain.OpGreater, Threshold: 0}}, nf)
	if err := al.Observe(memID, domain.Sample{Time: t0, Value: "1"}); !errors.Is(err, boom) {
		t.Errorf("err = %v", err)
	}
}

func TestAlerterMultipleRulesSameStream(t *testing.T) {
	nf := &fakeNotifier{}
	al := usecase.NewAlerter([]domain.Rule{
		{Stream: memID, Op: domain.OpGreater, Threshold: 10, Channel: "a"},
		{Stream: memID, Op: domain.OpGreater, Threshold: 100, Channel: "b"},
	}, nf)
	al.Observe(memID, domain.Sample{Time: t0, Value: "50"})
	if len(nf.sent) != 1 || nf.sent[0].Rule.Channel != "a" {
		t.Errorf("sent = %+v", nf.sent)
	}
}

func TestAlerterNotifiesResolvedAfterRecovery(t *testing.T) {
	nf := &fakeNotifier{}
	rule := domain.Rule{Stream: memID, Op: domain.OpGreater, Threshold: 100, For: time.Minute, Channel: "ops"}
	al := usecase.NewAlerter([]domain.Rule{rule}, nf)

	at := func(min int) time.Time { return t0.Add(time.Duration(min) * time.Minute) }
	for _, step := range []struct {
		min int
		v   string
	}{{0, "150"}, {1, "150"}, {2, "150"}, {3, "90"}, {4, "80"}} {
		if err := al.Observe(memID, domain.Sample{Time: at(step.min), Value: step.v, Unit: "Bytes"}); err != nil {
			t.Fatal(err)
		}
	}
	if len(nf.sent) != 2 {
		t.Fatalf("sent %d notifications, want 2: %+v", len(nf.sent), nf.sent)
	}
	if nf.sent[0].Event != domain.Fired {
		t.Errorf("first = %+v, want Fired", nf.sent[0])
	}
	r := nf.sent[1]
	if r.Event != domain.Resolved || r.Rule != rule || r.Value != "90" || r.Unit != "Bytes" || !r.Time.Equal(at(3)) {
		t.Errorf("resolved = %+v", r)
	}
}
