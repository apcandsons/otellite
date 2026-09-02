package usecase_test

import (
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/apcandsons/otellite/internal/domain"
	"github.com/apcandsons/otellite/internal/usecase"
)

// fakeStore is a minimal in-memory implementation of every store interface
// the use cases need. It is deliberately naive.
type fakeStore struct {
	data map[domain.StreamID][]domain.Sample
}

func newFake() *fakeStore { return &fakeStore{data: map[domain.StreamID][]domain.Sample{}} }

func (f *fakeStore) Append(id domain.StreamID, s domain.Sample) { f.data[id] = append(f.data[id], s) }

func (f *fakeStore) Count() int {
	n := 0
	for _, ss := range f.data {
		n += len(ss)
	}
	return n
}

func (f *fakeStore) DropOldest(n int) int {
	dropped := 0
	for ; n > 0; n-- {
		var oldest domain.StreamID
		found := false
		for id, ss := range f.data {
			if len(ss) > 0 && (!found || ss[0].Time.Before(f.data[oldest][0].Time)) {
				oldest, found = id, true
			}
		}
		if !found {
			break
		}
		f.data[oldest] = f.data[oldest][1:]
		dropped++
	}
	return dropped
}

func (f *fakeStore) DropBefore(cutoff time.Time) int {
	dropped := 0
	for id, ss := range f.data {
		keep := ss[:0]
		for _, s := range ss {
			if s.Time.Before(cutoff) {
				dropped++
			} else {
				keep = append(keep, s)
			}
		}
		f.data[id] = keep
	}
	return dropped
}

func (f *fakeStore) Namespaces() []string {
	set := map[string]bool{}
	for id, ss := range f.data {
		if len(ss) > 0 {
			set[id.Namespace] = true
		}
	}
	return keys(set)
}

func (f *fakeStore) Services(ns string) []string {
	set := map[string]bool{}
	for id, ss := range f.data {
		if len(ss) > 0 && id.Namespace == ns {
			set[id.Service] = true
		}
	}
	return keys(set)
}

func (f *fakeStore) Streams(ns, svc string, kind domain.Kind) []string {
	set := map[string]bool{}
	for id, ss := range f.data {
		if len(ss) > 0 && id.Namespace == ns && id.Service == svc && id.Kind == kind {
			set[id.Name] = true
		}
	}
	return keys(set)
}

func (f *fakeStore) Samples(id domain.StreamID) ([]domain.Sample, bool) {
	ss, ok := f.data[id]
	return ss, ok && len(ss) > 0
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

var (
	t0    = time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	memID = domain.StreamID{Namespace: "iam", Service: "iam-api", Kind: domain.Metrics, Name: "go.memory.used"}
	logID = domain.StreamID{Namespace: "iam", Service: "iam-api", Kind: domain.Logs, Name: "app"}
	webID = domain.StreamID{Namespace: "web", Service: "front", Kind: domain.Metrics, Name: "rps"}
)

func sample(offset time.Duration, v string) domain.Sample {
	return domain.Sample{Time: t0.Add(offset), Value: v, Unit: "Bytes"}
}

func TestIngestAppendsAndKeepsBudget(t *testing.T) {
	st := newFake()
	ing := usecase.NewIngester(st, domain.Budget{MaxSamples: 3})
	for i := 0; i < 5; i++ {
		ing.Ingest(memID, sample(time.Duration(i)*time.Second, "v"))
	}
	if st.Count() != 3 {
		t.Fatalf("count = %d, want 3", st.Count())
	}
	ss, _ := st.Samples(memID)
	if !ss[0].Time.Equal(t0.Add(2 * time.Second)) {
		t.Errorf("oldest surviving sample = %v, want t0+2s", ss[0].Time)
	}
}

func TestIngestUnlimitedBudget(t *testing.T) {
	st := newFake()
	ing := usecase.NewIngester(st, domain.Budget{})
	for i := 0; i < 50; i++ {
		ing.Ingest(memID, sample(0, "v"))
	}
	if st.Count() != 50 {
		t.Fatalf("count = %d", st.Count())
	}
}

func TestEvictDropsOutsideWindow(t *testing.T) {
	st := newFake()
	st.Append(memID, sample(-4*time.Hour, "old"))
	st.Append(memID, sample(-1*time.Hour, "fresh"))
	st.Append(logID, sample(-3*time.Hour-time.Second, "old"))
	ev := usecase.NewEvictor(st, domain.Window{Duration: 3 * time.Hour}, func() time.Time { return t0 })
	if n := ev.Run(); n != 2 {
		t.Fatalf("dropped %d, want 2", n)
	}
	ss, _ := st.Samples(memID)
	if len(ss) != 1 || ss[0].Value != "fresh" {
		t.Errorf("remaining = %+v", ss)
	}
}

func TestBrowseLs(t *testing.T) {
	st := newFake()
	st.Append(memID, sample(0, "1"))
	st.Append(logID, sample(0, "INFO hi"))
	st.Append(webID, sample(0, "2"))
	b := usecase.NewBrowser(st)

	cases := []struct {
		path string
		want []usecase.Entry
	}{
		{"/", []usecase.Entry{{Name: "iam", Dir: true}, {Name: "web", Dir: true}}},
		{"/iam", []usecase.Entry{{Name: "iam-api", Dir: true}}},
		{"/iam/iam-api", []usecase.Entry{{Name: "logs", Dir: true}, {Name: "metrics", Dir: true}}},
		{"/iam/iam-api/metrics", []usecase.Entry{{Name: "go.memory.used.dat"}}},
		{"/iam/iam-api/logs", []usecase.Entry{{Name: "app.dat"}}},
		{"/web/front/logs", []usecase.Entry{}},
	}
	for _, c := range cases {
		got, err := b.Ls(c.path)
		if err != nil {
			t.Fatalf("Ls(%q): %v", c.path, err)
		}
		if len(got) != len(c.want) {
			t.Fatalf("Ls(%q) = %+v, want %+v", c.path, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("Ls(%q)[%d] = %+v, want %+v", c.path, i, got[i], c.want[i])
			}
		}
	}
}

func TestBrowseLsErrors(t *testing.T) {
	st := newFake()
	st.Append(memID, sample(0, "1"))
	b := usecase.NewBrowser(st)
	for path, want := range map[string]error{
		"/nope":                         domain.ErrNotFound,
		"/iam/nope":                     domain.ErrNotFound,
		"/iam/iam-api/metrics/nope.dat": domain.ErrNotFound,
		"/iam/iam-api/metrics/go.memory.used.dat": domain.ErrNotDir,
		"relative": domain.ErrBadPath,
	} {
		if _, err := b.Ls(path); !errors.Is(err, want) {
			t.Errorf("Ls(%q) err = %v, want %v", path, err, want)
		}
	}
}

func TestBrowseCat(t *testing.T) {
	st := newFake()
	st.Append(memID, sample(0, "1"))
	st.Append(memID, sample(time.Second, "2"))
	b := usecase.NewBrowser(st)
	got, err := b.Cat("/iam/iam-api/metrics/go.memory.used.dat")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Value != "1" || got[1].Value != "2" {
		t.Errorf("Cat = %+v", got)
	}
	for path, want := range map[string]error{
		"/iam/iam-api/metrics/missing.dat": domain.ErrNotFound,
		"/iam/iam-api/metrics":             domain.ErrIsDir,
		"/":                                domain.ErrIsDir,
	} {
		if _, err := b.Cat(path); !errors.Is(err, want) {
			t.Errorf("Cat(%q) err = %v, want %v", path, err, want)
		}
	}
}
