// Package fsapi is the wire protocol between the CLI and the system of
// record: a tiny JSON-over-HTTP mirror of the browse use case, with
// Handler on the SoR side and Client on the CLI side.
package fsapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/apcandsons/otellite/internal/domain"
	"github.com/apcandsons/otellite/internal/usecase"
)

// Browser is what the handler serves and what the client provides.
type Browser interface {
	Ls(path string) ([]usecase.Entry, error)
	Cat(path string) ([]domain.Sample, error)
}

const (
	lsPath  = "/fs/ls"
	catPath = "/fs/cat"
)

type entryJSON struct {
	Name string `json:"name"`
	Dir  bool   `json:"dir"`
}

type sampleJSON struct {
	Time  time.Time `json:"time"`
	Value string    `json:"value"`
	Unit  string    `json:"unit,omitempty"`
}

type errorJSON struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

var kinds = map[string]error{
	"bad_path":  domain.ErrBadPath,
	"not_found": domain.ErrNotFound,
	"not_dir":   domain.ErrNotDir,
	"is_dir":    domain.ErrIsDir,
}

func classify(err error) (kind string, status int) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return "not_found", http.StatusNotFound
	case errors.Is(err, domain.ErrNotDir):
		return "not_dir", http.StatusBadRequest
	case errors.Is(err, domain.ErrIsDir):
		return "is_dir", http.StatusBadRequest
	case errors.Is(err, domain.ErrBadPath):
		return "bad_path", http.StatusBadRequest
	}
	return "internal", http.StatusInternalServerError
}

// NewHandler serves the browse API for the given browser.
func NewHandler(b Browser) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(lsPath, func(w http.ResponseWriter, r *http.Request) {
		entries, err := b.Ls(r.URL.Query().Get("path"))
		if err != nil {
			writeErr(w, err)
			return
		}
		out := make([]entryJSON, 0, len(entries))
		for _, e := range entries {
			out = append(out, entryJSON{Name: e.Name, Dir: e.Dir})
		}
		writeJSON(w, http.StatusOK, out)
	})
	mux.HandleFunc(catPath, func(w http.ResponseWriter, r *http.Request) {
		samples, err := b.Cat(r.URL.Query().Get("path"))
		if err != nil {
			writeErr(w, err)
			return
		}
		out := make([]sampleJSON, 0, len(samples))
		for _, s := range samples {
			out = append(out, sampleJSON{Time: s.Time, Value: s.Value, Unit: s.Unit})
		}
		writeJSON(w, http.StatusOK, out)
	})
	return mux
}

func writeErr(w http.ResponseWriter, err error) {
	kind, status := classify(err)
	writeJSON(w, status, errorJSON{Kind: kind, Message: err.Error()})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// Client talks to a remote SoR and satisfies Browser.
type Client struct {
	base string
	http *http.Client
}

func NewClient(baseURL string, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{base: baseURL, http: hc}
}

func (c *Client) Ls(path string) ([]usecase.Entry, error) {
	var raw []entryJSON
	if err := c.get(lsPath, path, &raw); err != nil {
		return nil, err
	}
	out := make([]usecase.Entry, 0, len(raw))
	for _, e := range raw {
		out = append(out, usecase.Entry{Name: e.Name, Dir: e.Dir})
	}
	return out, nil
}

func (c *Client) Cat(path string) ([]domain.Sample, error) {
	var raw []sampleJSON
	if err := c.get(catPath, path, &raw); err != nil {
		return nil, err
	}
	out := make([]domain.Sample, 0, len(raw))
	for _, s := range raw {
		out = append(out, domain.Sample{Time: s.Time, Value: s.Value, Unit: s.Unit})
	}
	return out, nil
}

func (c *Client) get(endpoint, path string, into any) error {
	resp, err := c.http.Get(c.base + endpoint + "?path=" + url.QueryEscape(path))
	if err != nil {
		return fmt.Errorf("sor: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("sor: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		var e errorJSON
		if json.Unmarshal(body, &e) == nil && e.Message != "" {
			if base, ok := kinds[e.Kind]; ok {
				return fmt.Errorf("%s: %w", stripSuffix(e.Message, base), base)
			}
			return errors.New(e.Message)
		}
		return fmt.Errorf("sor: HTTP %d", resp.StatusCode)
	}
	return json.Unmarshal(body, into)
}

// stripSuffix removes ": <base>" from a message so re-wrapping does not
// repeat the sentinel text.
func stripSuffix(msg string, base error) string {
	suffix := ": " + base.Error()
	if len(msg) > len(suffix) && msg[len(msg)-len(suffix):] == suffix {
		return msg[:len(msg)-len(suffix)]
	}
	return msg
}
