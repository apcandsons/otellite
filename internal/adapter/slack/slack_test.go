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

	n := slack.New(map[string]slack.Destination{"ops": {Webhook: srv.URL}}, srv.Client(), time.FixedZone("JST", 9*3600))
	if err := n.Notify(notification()); err != nil {
		t.Fatal(err)
	}
	text := got["text"]
	for _, want := range []string{"ALERT:/iam/iam-api/metrics/go.memory.used.dat", "> 476.8 MB", "3m", "584.0 MB", "2026-04-01 12:34:56 JST"} {
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
	n := slack.New(map[string]slack.Destination{"ops": {Webhook: srv.URL}}, srv.Client(), nil)
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
	for _, want := range []string{"RESOLVED:/iam/iam-api/metrics/go.memory.used.dat", "> 476.8 MB", "393.2 MB", "2026-04-01 12:34:56 JST"} {
		if !strings.Contains(text, want) {
			t.Errorf("text %q lacks %q", text, want)
		}
	}
	if strings.Contains(text, "rotating_light") {
		t.Errorf("resolved text should not use the alarm icon: %q", text)
	}
	fired := slack.Text(notification(), time.UTC)
	if strings.Contains(fired, "RESOLVED") {
		t.Errorf("fired text should not say RESOLVED: %q", fired)
	}
}

func TestAbsentText(t *testing.T) {
	nt := notification()
	nt.Rule.Op = domain.OpAbsent
	nt.Rule.Threshold = 0
	nt.Rule.For = 30 * time.Second
	nt.Value, nt.Unit = "", ""
	fired := slack.Text(nt, time.UTC)
	for _, want := range []string{"ALERT:/iam/iam-api/metrics/go.memory.used.dat", "no samples for 30s", "2026-04-01 03:34:56 UTC"} {
		if !strings.Contains(fired, want) {
			t.Errorf("fired %q lacks %q", fired, want)
		}
	}
	if strings.Contains(fired, "absent 0") || strings.Contains(fired, "has been absent") {
		t.Errorf("fired text should not render the op like a threshold: %q", fired)
	}
	nt.Event = domain.Resolved
	nt.Value, nt.Unit = "42", "By"
	resolved := slack.Text(nt, time.UTC)
	for _, want := range []string{"RESOLVED:/iam/iam-api/metrics/go.memory.used.dat", "reporting again", "42 B"} {
		if !strings.Contains(resolved, want) {
			t.Errorf("resolved %q lacks %q", resolved, want)
		}
	}
}

type apiCall struct {
	Path string
	Auth string
	Body map[string]string
}

// fakeSlackAPI records chat.postMessage / chat.update calls.
func fakeSlackAPI(t *testing.T, ok bool) (*httptest.Server, *[]apiCall) {
	t.Helper()
	var calls []apiCall
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		raw, _ := io.ReadAll(r.Body)
		json.Unmarshal(raw, &body)
		calls = append(calls, apiCall{Path: r.URL.Path, Auth: r.Header.Get("Authorization"), Body: body})
		w.Header().Set("Content-Type", "application/json")
		if !ok {
			w.Write([]byte(`{"ok":false,"error":"invalid_auth"}`))
			return
		}
		w.Write([]byte(`{"ok":true,"channel":"C0123","ts":"1717171717.000100"}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func botNotifier(srv *httptest.Server) *slack.Notifier {
	n := slack.New(map[string]slack.Destination{"ops": {Token: "xoxb-secret", ChannelID: "C0123"}}, srv.Client(), time.UTC)
	n.APIBase = srv.URL
	return n
}

func TestBotPostsAlertThenEditsItOnResolve(t *testing.T) {
	srv, calls := fakeSlackAPI(t, true)
	n := botNotifier(srv)

	fired := notification()
	if err := n.Notify(fired); err != nil {
		t.Fatal(err)
	}
	resolved := notification()
	resolved.Event = domain.Resolved
	resolved.Value = "412345678"
	resolved.Time = fired.Time.Add(7 * time.Minute)
	if err := n.Notify(resolved); err != nil {
		t.Fatal(err)
	}

	if len(*calls) != 2 {
		t.Fatalf("calls = %+v", *calls)
	}
	post, update := (*calls)[0], (*calls)[1]
	if post.Path != "/chat.postMessage" || post.Auth != "Bearer xoxb-secret" || post.Body["channel"] != "C0123" || !strings.Contains(post.Body["text"], "ALERT:/iam/iam-api/metrics/go.memory.used.dat") {
		t.Errorf("post = %+v", post)
	}
	if update.Path != "/chat.update" || update.Auth != "Bearer xoxb-secret" || update.Body["channel"] != "C0123" || update.Body["ts"] != "1717171717.000100" {
		t.Errorf("update = %+v", update)
	}
	text := update.Body["text"]
	for _, want := range []string{
		"RESOLVED:/iam/iam-api/metrics/go.memory.used.dat",
		"ALERT:/iam/iam-api/metrics/go.memory.used.dat",
		"> 476.8 MB for 3m",
		"detected: 584.0 MB at [2026-04-01 03:34:56 UTC]",
		"resolved: 393.2 MB at [2026-04-01 03:41:56 UTC]",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("edited text %q lacks %q", text, want)
		}
	}
	if strings.Contains(text, ":rotating_light:") {
		t.Errorf("edited text should swap the alarm icon: %q", text)
	}
}

func TestBotReportsAPIErrors(t *testing.T) {
	srv, _ := fakeSlackAPI(t, false)
	if err := botNotifier(srv).Notify(notification()); err == nil || !strings.Contains(err.Error(), "invalid_auth") {
		t.Errorf("err = %v", err)
	}
}

func TestWebhookResolveKeepsDetectionTimeInANewMessage(t *testing.T) {
	var texts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		raw, _ := io.ReadAll(r.Body)
		json.Unmarshal(raw, &body)
		texts = append(texts, body["text"])
		w.Write([]byte("ok"))
	}))
	defer srv.Close()
	n := slack.New(map[string]slack.Destination{"ops": {Webhook: srv.URL}}, srv.Client(), time.UTC)

	fired := notification()
	n.Notify(fired)
	resolved := fired
	resolved.Event = domain.Resolved
	resolved.Value = "412345678"
	resolved.Time = fired.Time.Add(time.Minute)
	if err := n.Notify(resolved); err != nil {
		t.Fatal(err)
	}
	if len(texts) != 2 {
		t.Fatalf("texts = %q", texts)
	}
	for _, want := range []string{"RESOLVED:", "detected: 584.0 MB at [2026-04-01 03:34:56 UTC]", "resolved: 393.2 MB at [2026-04-01 03:35:56 UTC]"} {
		if !strings.Contains(texts[1], want) {
			t.Errorf("resolved text %q lacks %q", texts[1], want)
		}
	}
}

func TestResolveWithoutKnownAlertPostsPlainText(t *testing.T) {
	srv, calls := fakeSlackAPI(t, true)
	n := botNotifier(srv)
	resolved := notification()
	resolved.Event = domain.Resolved
	if err := n.Notify(resolved); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 || (*calls)[0].Path != "/chat.postMessage" || strings.Contains((*calls)[0].Body["text"], "detected") {
		t.Errorf("calls = %+v", *calls)
	}
}

func TestAbsentResolvedEditKeepsDetectionTime(t *testing.T) {
	srv, calls := fakeSlackAPI(t, true)
	n := botNotifier(srv)
	fired := notification()
	fired.Rule.Op, fired.Rule.For, fired.Value, fired.Unit = domain.OpAbsent, 30*time.Second, "", ""
	n.Notify(fired)
	resolved := fired
	resolved.Event, resolved.Value, resolved.Unit = domain.Resolved, "0.5", "1"
	resolved.Time = fired.Time.Add(45 * time.Second)
	n.Notify(resolved)
	text := (*calls)[1].Body["text"]
	for _, want := range []string{"RESOLVED:", "reporting again", "no samples for 30s", "detected at [2026-04-01 03:34:56 UTC]", "resolved: 0.5 at [2026-04-01 03:35:41 UTC]"} {
		if !strings.Contains(text, want) {
			t.Errorf("edited text %q lacks %q", text, want)
		}
	}
}
