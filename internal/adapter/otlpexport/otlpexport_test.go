package otlpexport

import (
	"context"
	"net/http/httptest"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/apcandsons/otellite/internal/adapter/otlp"
	"github.com/apcandsons/otellite/internal/domain"
)

// receiver is the real OTLP handler over a recording sink (client/ is a
// leaf, so clienttest cannot be used from under internal/).
type receiver struct {
	srv *httptest.Server
	mu  sync.Mutex
	got map[string][]domain.Sample
}

func newReceiver(t *testing.T) *receiver {
	r := &receiver{got: map[string][]domain.Sample{}}
	r.srv = httptest.NewServer(otlp.NewHandler(r, time.Now))
	t.Cleanup(r.srv.Close)
	return r
}

func (r *receiver) Ingest(id domain.StreamID, s domain.Sample) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p := id.Path().String()
	r.got[p] = append(r.got[p], s)
}

func (r *receiver) URL() string { return r.srv.URL }

func (r *receiver) Streams() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for p := range r.got {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func (r *receiver) Wait(path string, n int, timeout time.Duration) []domain.Sample {
	deadline := time.Now().Add(timeout)
	for {
		r.mu.Lock()
		s := append([]domain.Sample(nil), r.got[path]...)
		r.mu.Unlock()
		if len(s) >= n || time.Now().After(deadline) {
			return s
		}
		time.Sleep(10 * time.Millisecond)
	}
}

var (
	ecsTarget = domain.Target{Kind: domain.TargetECS, ID: "comm-staging/comm-bff", Namespace: "comm", Service: "comm-bff", Prefix: "ecs"}
	sqsTarget = domain.Target{Kind: domain.TargetSQS, ID: "comm-staging-embed-dlq", Namespace: "comm", Service: "queues", Prefix: "embed_dlq"}
	sqsTwo    = domain.Target{Kind: domain.TargetSQS, ID: "comm-staging-push-dlq", Namespace: "comm", Service: "queues", Prefix: "push_dlq"}
)

func TestGaugesLandUnderEachTargetsPath(t *testing.T) {
	rcv := newReceiver(t)
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "deployment.environment=staging")
	ctx := context.Background()
	e, err := New(ctx, rcv.URL())
	if err != nil {
		t.Fatal(err)
	}
	defer e.Shutdown(ctx)

	e.Gauge(ecsTarget, "running_tasks", 1)
	e.Gauge(sqsTarget, "visible", 3)
	e.Gauge(sqsTwo, "visible", 0) // same <ns>/<svc>, different prefix: shares a provider
	if err := e.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{
		"/comm/comm-bff/metrics/ecs.running_tasks.dat": "1",
		"/comm/queues/metrics/embed_dlq.visible.dat":   "3",
		"/comm/queues/metrics/push_dlq.visible.dat":    "0",
	} {
		s := rcv.Wait(path, 1, 2*time.Second)
		if len(s) == 0 {
			t.Errorf("%s: nothing received; streams: %v", path, rcv.Streams())
			continue
		}
		if s[len(s)-1].Value != want {
			t.Errorf("%s = %q, want %q", path, s[len(s)-1].Value, want)
		}
	}

	// A second tick re-records: gauges report their latest value each flush.
	e.Gauge(sqsTarget, "visible", 5)
	if err := e.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	s := rcv.Wait("/comm/queues/metrics/embed_dlq.visible.dat", 2, 2*time.Second)
	if len(s) < 2 || s[len(s)-1].Value != "5" {
		t.Fatalf("second flush: %+v", s)
	}
}

func TestNewRejectsAnEmptyEndpoint(t *testing.T) {
	if _, err := New(context.Background(), ""); err == nil {
		t.Fatal("expected an error: the bridge is useless without a SoR")
	}
}
