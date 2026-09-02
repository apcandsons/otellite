package domain_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/apcandsons/otellite/internal/domain"
)

func TestParsePathDepths(t *testing.T) {
	cases := []struct {
		in   string
		want domain.Path
	}{
		{"/", domain.Path{}},
		{"/iam", domain.Path{Namespace: "iam"}},
		{"/iam/iam-api", domain.Path{Namespace: "iam", Service: "iam-api"}},
		{"/iam/iam-api/metrics", domain.Path{Namespace: "iam", Service: "iam-api", Kind: domain.Metrics}},
		{"/iam/iam-api/logs", domain.Path{Namespace: "iam", Service: "iam-api", Kind: domain.Logs}},
		{"/iam/iam-api/metrics/go.memory.used.dat", domain.Path{Namespace: "iam", Service: "iam-api", Kind: domain.Metrics, Name: "go.memory.used"}},
	}
	for _, c := range cases {
		got, err := domain.ParsePath(c.in)
		if err != nil {
			t.Fatalf("ParsePath(%q): %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("ParsePath(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

func TestParsePathRejectsBadInput(t *testing.T) {
	cases := map[string]error{
		"iam":                          domain.ErrBadPath,
		"//iam":                        domain.ErrBadPath,
		"/iam/svc/traces":              domain.ErrNotFound,
		"/iam/svc/metrics/x.dat/y":     domain.ErrNotFound,
		"/iam/svc/metrics/nodatsuffix": domain.ErrNotFound,
	}
	for in, want := range cases {
		_, err := domain.ParsePath(in)
		if !errors.Is(err, want) {
			t.Errorf("ParsePath(%q) err = %v, want %v", in, err, want)
		}
		if err != nil && !strings.HasSuffix(err.Error(), want.Error()) {
			t.Errorf("ParsePath(%q) message %q should end with the sentinel", in, err)
		}
	}
}

func TestPathDepthAndStreamID(t *testing.T) {
	p, _ := domain.ParsePath("/iam/iam-api/logs/app.dat")
	if p.Depth() != domain.DepthStream {
		t.Fatalf("depth = %v", p.Depth())
	}
	id := p.StreamID()
	want := domain.StreamID{Namespace: "iam", Service: "iam-api", Kind: domain.Logs, Name: "app"}
	if id != want {
		t.Errorf("StreamID = %+v, want %+v", id, want)
	}
	if got := id.Path().String(); got != "/iam/iam-api/logs/app.dat" {
		t.Errorf("round trip = %q", got)
	}
}

func TestWindowCutoff(t *testing.T) {
	w := domain.Window{Duration: 3 * time.Hour}
	now := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	if got := w.Cutoff(now); !got.Equal(now.Add(-3 * time.Hour)) {
		t.Errorf("Cutoff = %v", got)
	}
}

func TestBudgetExcess(t *testing.T) {
	b := domain.Budget{MaxSamples: 10}
	if b.Excess(9) != 0 || b.Excess(10) != 0 || b.Excess(13) != 3 {
		t.Errorf("Excess wrong: %d %d %d", b.Excess(9), b.Excess(10), b.Excess(13))
	}
	if (domain.Budget{}).Excess(1_000_000) != 0 {
		t.Error("zero budget should mean unlimited")
	}
}
