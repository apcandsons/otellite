// Package memstore is the in-memory system of record. Samples live in a
// slice per stream; everything is forgotten when the process exits.
package memstore

import (
	"sort"
	"sync"
	"time"

	"github.com/apcandsons/otellite/internal/domain"
)

// Store keeps every stream in memory. It is safe for concurrent use.
type Store struct {
	mu      sync.RWMutex
	streams map[domain.StreamID][]domain.Sample
	count   int
}

func New() *Store {
	return &Store{streams: map[domain.StreamID][]domain.Sample{}}
}

// Append adds a sample, keeping the stream ordered by time. Samples usually
// arrive in order, so the common case is a plain append.
func (s *Store) Append(id domain.StreamID, sample domain.Sample) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ss := s.streams[id]
	i := len(ss)
	for i > 0 && sample.Time.Before(ss[i-1].Time) {
		i--
	}
	ss = append(ss, domain.Sample{})
	copy(ss[i+1:], ss[i:])
	ss[i] = sample
	s.streams[id] = ss
	s.count++
}

func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.count
}

// DropOldest removes the n oldest samples across all streams.
func (s *Store) DropOldest(n int) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	dropped := 0
	for ; n > 0; n-- {
		var oldest domain.StreamID
		found := false
		for id, ss := range s.streams {
			if !found || ss[0].Time.Before(s.streams[oldest][0].Time) {
				oldest, found = id, true
			}
		}
		if !found {
			break
		}
		s.streams[oldest] = s.streams[oldest][1:]
		if len(s.streams[oldest]) == 0 {
			delete(s.streams, oldest)
		}
		s.count--
		dropped++
	}
	return dropped
}

// DropBefore forgets every sample older than cutoff.
func (s *Store) DropBefore(cutoff time.Time) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	dropped := 0
	for id, ss := range s.streams {
		i := sort.Search(len(ss), func(i int) bool { return !ss[i].Time.Before(cutoff) })
		if i == 0 {
			continue
		}
		dropped += i
		s.count -= i
		if i == len(ss) {
			delete(s.streams, id)
			continue
		}
		s.streams[id] = append([]domain.Sample(nil), ss[i:]...)
	}
	return dropped
}

func (s *Store) Namespaces() []string {
	return s.collect(func(id domain.StreamID) (string, bool) { return id.Namespace, true })
}

func (s *Store) Services(ns string) []string {
	return s.collect(func(id domain.StreamID) (string, bool) { return id.Service, id.Namespace == ns })
}

func (s *Store) Streams(ns, svc string, kind domain.Kind) []string {
	return s.collect(func(id domain.StreamID) (string, bool) {
		return id.Name, id.Namespace == ns && id.Service == svc && id.Kind == kind
	})
}

// Samples returns a copy of the stream, oldest first.
func (s *Store) Samples(id domain.StreamID) ([]domain.Sample, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ss, ok := s.streams[id]
	if !ok {
		return nil, false
	}
	return append([]domain.Sample(nil), ss...), true
}

func (s *Store) collect(pick func(domain.StreamID) (string, bool)) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	set := map[string]struct{}{}
	for id := range s.streams {
		if name, ok := pick(id); ok {
			set[name] = struct{}{}
		}
	}
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for name := range set {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
