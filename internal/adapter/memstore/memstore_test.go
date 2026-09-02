package memstore_test

import (
	"sync"
	"testing"
	"time"

	"github.com/apcandsons/otellite/internal/adapter/memstore"
	"github.com/apcandsons/otellite/internal/domain"
	"github.com/apcandsons/otellite/internal/usecase"
)

var (
	_ usecase.AppendStore = (*memstore.Store)(nil)
	_ usecase.PruneStore  = (*memstore.Store)(nil)
	_ usecase.ReadStore   = (*memstore.Store)(nil)
)

var (
	t0 = time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	a  = domain.StreamID{Namespace: "iam", Service: "iam-api", Kind: domain.Metrics, Name: "mem"}
	b  = domain.StreamID{Namespace: "iam", Service: "iam-api", Kind: domain.Logs, Name: "app"}
	c  = domain.StreamID{Namespace: "web", Service: "front", Kind: domain.Metrics, Name: "rps"}
	at = func(d time.Duration) domain.Sample { return domain.Sample{Time: t0.Add(d), Value: "x"} }
)

func eq(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestListingsAreSortedAndOnlyNonEmpty(t *testing.T) {
	s := memstore.New()
	s.Append(c, at(0))
	s.Append(a, at(0))
	s.Append(b, at(0))
	eq(t, s.Namespaces(), []string{"iam", "web"})
	eq(t, s.Services("iam"), []string{"iam-api"})
	eq(t, s.Services("nope"), nil)
	eq(t, s.Streams("iam", "iam-api", domain.Metrics), []string{"mem"})
	eq(t, s.Streams("iam", "iam-api", domain.Logs), []string{"app"})
	if s.Count() != 3 {
		t.Fatalf("count = %d", s.Count())
	}
}

func TestSamplesReturnsCopyOldestFirst(t *testing.T) {
	s := memstore.New()
	s.Append(a, at(2*time.Second))
	s.Append(a, at(1*time.Second))
	ss, ok := s.Samples(a)
	if !ok || len(ss) != 2 {
		t.Fatalf("ok=%v ss=%v", ok, ss)
	}
	if !ss[0].Time.Before(ss[1].Time) {
		t.Errorf("not sorted oldest first: %v", ss)
	}
	ss[0].Value = "mutated"
	again, _ := s.Samples(a)
	if again[0].Value == "mutated" {
		t.Error("Samples exposed internal slice")
	}
	if _, ok := s.Samples(c); ok {
		t.Error("missing stream reported present")
	}
}

func TestDropBeforeRemovesEmptyStreams(t *testing.T) {
	s := memstore.New()
	s.Append(a, at(-2*time.Hour))
	s.Append(a, at(0))
	s.Append(c, at(-2*time.Hour))
	if n := s.DropBefore(t0.Add(-time.Hour)); n != 2 {
		t.Fatalf("dropped %d", n)
	}
	eq(t, s.Namespaces(), []string{"iam"})
	if _, ok := s.Samples(c); ok {
		t.Error("empty stream still listed")
	}
}

func TestDropOldestIsGlobal(t *testing.T) {
	s := memstore.New()
	s.Append(a, at(3*time.Second))
	s.Append(c, at(1*time.Second))
	s.Append(b, at(2*time.Second))
	if n := s.DropOldest(2); n != 2 {
		t.Fatalf("dropped %d", n)
	}
	if s.Count() != 1 {
		t.Fatalf("count = %d", s.Count())
	}
	if _, ok := s.Samples(a); !ok {
		t.Error("newest sample was dropped")
	}
	if n := s.DropOldest(5); n != 1 {
		t.Errorf("over-drop returned %d", n)
	}
}

func TestConcurrentAccess(t *testing.T) {
	s := memstore.New()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				s.Append(a, at(time.Duration(i*1000+j)*time.Millisecond))
				s.Samples(a)
				s.Namespaces()
				s.DropOldest(1)
				s.DropBefore(t0)
			}
		}(i)
	}
	wg.Wait()
}
