package fsapi_test

import (
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/apcandsons/otellite/internal/adapter/fsapi"
	"github.com/apcandsons/otellite/internal/domain"
	"github.com/apcandsons/otellite/internal/usecase"
)

var t0 = time.Date(2026, 4, 1, 3, 34, 56, 123456789, time.UTC)

type fakeBrowser struct{}

func (fakeBrowser) Ls(path string) ([]usecase.Entry, error) {
	switch path {
	case "/":
		return []usecase.Entry{{Name: "iam", Dir: true}}, nil
	case "/iam/svc/logs":
		return []usecase.Entry{}, nil
	case "/iam/svc/metrics/mem.dat":
		return nil, fmt.Errorf("%s: %w", path, domain.ErrNotDir)
	case "bad":
		return nil, domain.ErrBadPath
	}
	return nil, fmt.Errorf("%s: %w", path, domain.ErrNotFound)
}

func (fakeBrowser) Cat(path string) ([]domain.Sample, error) {
	switch path {
	case "/iam/svc/metrics/mem.dat":
		return []domain.Sample{{Time: t0, Value: "1", Unit: "By"}, {Time: t0.Add(time.Second), Value: "INFO x"}}, nil
	case "/iam":
		return nil, fmt.Errorf("%s: %w", path, domain.ErrIsDir)
	}
	return nil, fmt.Errorf("%s: %w", path, domain.ErrNotFound)
}

func client(t *testing.T) *fsapi.Client {
	srv := httptest.NewServer(fsapi.NewHandler(fakeBrowser{}))
	t.Cleanup(srv.Close)
	return fsapi.NewClient(srv.URL, srv.Client())
}

func TestLsRoundTrip(t *testing.T) {
	c := client(t)
	got, err := c.Ls("/")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != (usecase.Entry{Name: "iam", Dir: true}) {
		t.Errorf("got %+v", got)
	}
	empty, err := c.Ls("/iam/svc/logs")
	if err != nil || len(empty) != 0 {
		t.Errorf("empty dir: %v %v", empty, err)
	}
}

func TestCatRoundTrip(t *testing.T) {
	c := client(t)
	got, err := c.Cat("/iam/svc/metrics/mem.dat")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !got[0].Time.Equal(t0) || got[0].Value != "1" || got[0].Unit != "By" || got[1].Value != "INFO x" || got[1].Unit != "" {
		t.Errorf("got %+v", got)
	}
}

func TestErrorsSurviveTheWire(t *testing.T) {
	c := client(t)
	cases := []struct {
		name string
		err  error
		want error
	}{
		{"ls missing", func() error { _, e := c.Ls("/nope"); return e }(), domain.ErrNotFound},
		{"ls file", func() error { _, e := c.Ls("/iam/svc/metrics/mem.dat"); return e }(), domain.ErrNotDir},
		{"ls bad", func() error { _, e := c.Ls("bad"); return e }(), domain.ErrBadPath},
		{"cat dir", func() error { _, e := c.Cat("/iam"); return e }(), domain.ErrIsDir},
		{"cat missing", func() error { _, e := c.Cat("/x/y/logs/z.dat"); return e }(), domain.ErrNotFound},
	}
	for _, tc := range cases {
		if !errors.Is(tc.err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, tc.err, tc.want)
		}
	}
}

func TestClientReportsUnreachableServer(t *testing.T) {
	c := fsapi.NewClient("http://127.0.0.1:1", nil)
	if _, err := c.Ls("/"); err == nil {
		t.Error("expected connection error")
	}
}
