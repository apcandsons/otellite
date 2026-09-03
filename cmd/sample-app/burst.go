package main

import (
	"sync"
	"time"
)

// burst is a timed window of synthetic overload. It is safe for concurrent
// use: the key reader starts it, the request loop ticks it, and the SDK's
// collector goroutine asks whether it is active.
type burst struct {
	mu       sync.Mutex
	duration time.Duration
	until    time.Time // zero when idle
}

func newBurst(d time.Duration) *burst { return &burst{duration: d} }

// start begins a burst of the default duration at now. It reports false
// if one is already running.
func (b *burst) start(now time.Time) bool { return b.startFor(now, b.duration) }

// startFor begins a burst lasting d. It reports false if one is already
// running.
func (b *burst) startFor(now time.Time, d time.Duration) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.until.IsZero() && now.Before(b.until) {
		return false
	}
	b.until = now.Add(d)
	return true
}

// active reports whether a burst is in progress at now.
func (b *burst) active(now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return !b.until.IsZero() && now.Before(b.until)
}

// tick advances the clock and reports true exactly once, on the first call
// at or after the moment the current burst expires.
func (b *burst) tick(now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.until.IsZero() || now.Before(b.until) {
		return false
	}
	b.until = time.Time{}
	return true
}
