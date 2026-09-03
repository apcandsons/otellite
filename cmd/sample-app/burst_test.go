package main

import (
	"testing"
	"time"
)

func TestBurstLifecycle(t *testing.T) {
	t0 := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	at := func(sec int) time.Time { return t0.Add(time.Duration(sec) * time.Second) }
	b := newBurst(15 * time.Second)

	if b.active(at(0)) {
		t.Fatal("new burst should be idle")
	}
	if ended := b.tick(at(0)); ended {
		t.Fatal("idle burst should not report ended")
	}

	if !b.start(at(0)) {
		t.Fatal("start on idle burst should succeed")
	}
	if b.start(at(1)) {
		t.Error("start while active should be ignored")
	}
	for _, sec := range []int{0, 5, 14} {
		if !b.active(at(sec)) {
			t.Errorf("should be active at +%ds", sec)
		}
		if b.tick(at(sec)) {
			t.Errorf("should not report ended at +%ds", sec)
		}
	}
	if b.active(at(15)) {
		t.Error("should be idle at +15s")
	}
	if !b.tick(at(15)) {
		t.Error("tick at +15s should report ended once")
	}
	if b.tick(at(16)) {
		t.Error("ended should be reported only once")
	}
	if !b.start(at(20)) {
		t.Error("should be able to start a new burst after the last ended")
	}
}

func TestBurstStartForOverridesDuration(t *testing.T) {
	t0 := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	b := newBurst(time.Hour)
	if !b.startFor(t0, 10*time.Second) {
		t.Fatal("startFor on idle burst should succeed")
	}
	if !b.active(t0.Add(9 * time.Second)) {
		t.Error("should be active at +9s")
	}
	if b.active(t0.Add(10 * time.Second)) {
		t.Error("should be idle at +10s")
	}
	if !b.tick(t0.Add(10 * time.Second)) {
		t.Error("should report ended at +10s")
	}
}
