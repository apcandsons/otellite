// Package usecase contains the application logic of OTel Lite: ingesting
// samples, forgetting old ones, and browsing the virtual filesystem. Each use
// case declares the narrow store interface it needs; adapters satisfy them.
package usecase

import (
	"time"

	"github.com/apcandsons/otellite/internal/domain"
)

// AppendStore is what Ingester needs from a store.
type AppendStore interface {
	Append(domain.StreamID, domain.Sample)
	Count() int
	// DropOldest removes the n globally oldest samples and reports how many
	// were actually removed.
	DropOldest(n int) int
}

// Ingester writes samples and enforces the memory budget by forgetting the
// oldest samples once the budget is exceeded.
type Ingester struct {
	store  AppendStore
	budget domain.Budget
}

func NewIngester(store AppendStore, budget domain.Budget) *Ingester {
	return &Ingester{store: store, budget: budget}
}

// Ingest stores one sample, then trims the store back into budget.
func (i *Ingester) Ingest(id domain.StreamID, s domain.Sample) {
	i.store.Append(id, s)
	if n := i.budget.Excess(i.store.Count()); n > 0 {
		i.store.DropOldest(n)
	}
}

// PruneStore is what Evictor needs from a store.
type PruneStore interface {
	// DropBefore removes every sample older than cutoff and reports the count.
	DropBefore(cutoff time.Time) int
}

// Evictor applies the time-based retention window.
type Evictor struct {
	store  PruneStore
	window domain.Window
	now    func() time.Time
}

func NewEvictor(store PruneStore, window domain.Window, now func() time.Time) *Evictor {
	if now == nil {
		now = time.Now
	}
	return &Evictor{store: store, window: window, now: now}
}

// Run forgets everything outside the window and returns how much was dropped.
func (e *Evictor) Run() int {
	return e.store.DropBefore(e.window.Cutoff(e.now()))
}
