// Package clienttest is a test double for the OTel Lite system of record:
// an httptest server running the real OTLP/HTTP receiver over a recording
// sink. Point client.Start at URL() and assert on Streams() and Samples().
//
//	rcv := clienttest.NewReceiver(t)
//	shutdown, _ := client.Start(ctx, client.Options{Endpoint: rcv.URL(), Interval: 20 * time.Millisecond})
//	// ... exercise the code under test, then shutdown(ctx) or rcv.Wait(...)
package clienttest

import (
	"net/http/httptest"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/apcandsons/otellite/internal/adapter/otlp"
	"github.com/apcandsons/otellite/internal/domain"
)

// Receiver records everything the OTLP receiver ingests.
type Receiver struct {
	srv *httptest.Server

	mu      sync.Mutex
	samples map[string][]domain.Sample
}

// NewReceiver starts a receiver that is closed when the test ends.
func NewReceiver(t testing.TB) *Receiver {
	t.Helper()
	r := &Receiver{samples: map[string][]domain.Sample{}}
	r.srv = httptest.NewServer(otlp.NewHandler(r, time.Now))
	t.Cleanup(r.srv.Close)
	return r
}

// Ingest implements the receiver's sink.
func (r *Receiver) Ingest(id domain.StreamID, s domain.Sample) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p := id.Path().String()
	r.samples[p] = append(r.samples[p], s)
}

// URL is the base URL to export to, e.g. http://127.0.0.1:53211.
func (r *Receiver) URL() string { return r.srv.URL }

// Streams lists the paths that have received at least one sample, sorted,
// in the SoR's layout: /<namespace>/<service>/{logs,metrics}/<name>.dat.
func (r *Receiver) Streams() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.samples))
	for p := range r.samples {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// Samples returns every sample ingested for the path, oldest first.
func (r *Receiver) Samples(path string) []domain.Sample {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]domain.Sample(nil), r.samples[path]...)
}

// Wait polls until the path has at least n samples or the timeout passes,
// and returns what it has at that point.
func (r *Receiver) Wait(path string, n int, timeout time.Duration) []domain.Sample {
	deadline := time.Now().Add(timeout)
	for {
		if s := r.Samples(path); len(s) >= n || time.Now().After(deadline) {
			return s
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Reset forgets every recorded sample.
func (r *Receiver) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.samples = map[string][]domain.Sample{}
}
