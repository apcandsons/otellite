package slack_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/apcandsons/otellite/internal/adapter/slack"
	"github.com/apcandsons/otellite/internal/domain"
	"github.com/apcandsons/otellite/internal/usecase"
)

var _ usecase.Notifier = (*slack.Notifier)(nil)

func notification() domain.Notification {
	return domain.Notification{
		Rule: domain.Rule{
			Stream:    domain.StreamID{Namespace: "iam", Service: "iam-api", Kind: domain.Metrics, Name: "go.memory.used"},
			Op:        domain.OpGreater,
			Threshold: 500000000,
			For:       3 * time.Minute,
			Channel:   "ops",
		},
		Time:  time.Date(2026, 4, 1, 3, 34, 56, 0, time.UTC),
		Value: "612345678",
		Unit:  "Bytes",
	}
}

func TestPostsWebhookPayload(t *testing.T) {
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &got)
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("content type = %q", r.Header.Get("Content-Type"))
		}
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	n := slack.New(map[string]string{"ops": srv.URL}, srv.Client(), time.FixedZone("JST", 9*3600))
	if err := n.Notify(notification()); err != nil {
		t.Fatal(err)
	}
	text := got["text"]
	for _, want := range []string{"/iam/iam-api/metrics/go.memory.used.dat", "> 500000000", "3m", "612345678 Bytes", "2026-04-01 12:34:56 JST"} {
		if !strings.Contains(text, want) {
			t.Errorf("text %q lacks %q", text, want)
		}
	}
}

func TestUnknownChannelAndHTTPFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "invalid_token", http.StatusForbidden)
	}))
	defer srv.Close()
	n := slack.New(map[string]string{"ops": srv.URL}, srv.Client(), nil)
	if err := n.Notify(notification()); err == nil || !strings.Contains(err.Error(), "403") {
		t.Errorf("http failure err = %v", err)
	}
	nn := notification()
	nn.Rule.Channel = "ghost"
	if err := n.Notify(nn); err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Errorf("unknown channel err = %v", err)
	}
}

func TestResolvedText(t *testing.T) {
	nt := notification()
	nt.Event = domain.Resolved
	nt.Value = "412345678"
	text := slack.Text(nt, time.FixedZone("JST", 9*3600))
	for _, want := range []string{"resolved", "/iam/iam-api/metrics/go.memory.used.dat", "> 500000000", "412345678 Bytes", "2026-04-01 12:34:56 JST"} {
		if !strings.Contains(text, want) {
			t.Errorf("text %q lacks %q", text, want)
		}
	}
	if strings.Contains(text, "rotating_light") {
		t.Errorf("resolved text should not use the alarm icon: %q", text)
	}
	fired := slack.Text(notification(), time.UTC)
	if strings.Contains(fired, "resolved") {
		t.Errorf("fired text should not say resolved: %q", fired)
	}
}
