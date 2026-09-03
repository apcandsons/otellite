// Package slack delivers notifications to Slack, either through incoming
// webhooks or through the Web API with a bot token. With a bot token a
// resolved alert edits the original ALERT message instead of posting a
// new one, keeping both the detection and the resolution time.
package slack

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/apcandsons/otellite/internal/domain"
)

const (
	timeLayout     = "2006-01-02 15:04:05 MST"
	defaultAPIBase = "https://slack.com/api"
)

// Destination is where a channel name delivers: an incoming webhook URL,
// or a bot token (chat:write) plus the channel ID to post into.
type Destination struct {
	Webhook   string
	Token     string
	ChannelID string
}

// Notifier posts to one destination per channel name. It remembers the
// alerts it has posted so that resolutions can refer back to them; that
// memory lives only as long as the process.
type Notifier struct {
	dests map[string]Destination
	http  *http.Client
	loc   *time.Location
	// APIBase is the Slack Web API root; tests point it at a fake.
	APIBase string

	mu   sync.Mutex
	open map[string]posted // rule key -> the ALERT message
}

type posted struct {
	fired domain.Notification
	ts    string // message timestamp, empty for webhooks
}

func New(dests map[string]Destination, hc *http.Client, loc *time.Location) *Notifier {
	if hc == nil {
		hc = &http.Client{Timeout: 5 * time.Second}
	}
	if loc == nil {
		loc = time.Local
	}
	return &Notifier{dests: dests, http: hc, loc: loc, APIBase: defaultAPIBase, open: map[string]posted{}}
}

// Notify delivers the notification to its channel. A Fired notification
// posts a new message. A Resolved one edits the matching ALERT message
// when it was posted through a bot token, and otherwise posts a new
// message that still quotes when the alert was detected.
func (n *Notifier) Notify(nt domain.Notification) error {
	dest, ok := n.dests[nt.Rule.Channel]
	if !ok {
		return fmt.Errorf("slack: no destination for channel %q", nt.Rule.Channel)
	}
	key := ruleKey(nt.Rule)
	if nt.Event != domain.Resolved {
		ts, err := n.post(dest, Text(nt, n.loc))
		if err != nil {
			return err
		}
		n.mu.Lock()
		n.open[key] = posted{fired: nt, ts: ts}
		n.mu.Unlock()
		return nil
	}

	n.mu.Lock()
	prev, had := n.open[key]
	delete(n.open, key)
	n.mu.Unlock()
	if !had {
		_, err := n.post(dest, Text(nt, n.loc))
		return err
	}
	text := ResolvedText(prev.fired, nt, n.loc)
	if prev.ts != "" && dest.Token != "" {
		return n.api(dest, "chat.update", map[string]string{"channel": dest.ChannelID, "ts": prev.ts, "text": text}, nil)
	}
	_, err := n.post(dest, text)
	return err
}

// post sends a new message and returns its timestamp (bot only).
func (n *Notifier) post(dest Destination, text string) (string, error) {
	if dest.Token != "" {
		var resp struct {
			TS string `json:"ts"`
		}
		err := n.api(dest, "chat.postMessage", map[string]string{"channel": dest.ChannelID, "text": text}, &resp)
		return resp.TS, err
	}
	body, _ := json.Marshal(map[string]string{"text": text})
	resp, err := n.http.Post(dest.Webhook, "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("slack: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return "", fmt.Errorf("slack: webhook returned %d: %s", resp.StatusCode, bytes.TrimSpace(msg))
	}
	return "", nil
}

// api calls a Web API method with the bot token and decodes the reply.
func (n *Notifier) api(dest Destination, method string, params map[string]string, into any) error {
	body, _ := json.Marshal(params)
	req, err := http.NewRequest(http.MethodPost, n.APIBase+"/"+method, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("slack: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Authorization", "Bearer "+dest.Token)
	resp, err := n.http.Do(req)
	if err != nil {
		return fmt.Errorf("slack: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return fmt.Errorf("slack: %w", err)
	}
	var status struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(raw, &status); err != nil || resp.StatusCode/100 != 2 {
		return fmt.Errorf("slack: %s returned %d: %s", method, resp.StatusCode, bytes.TrimSpace(raw))
	}
	if !status.OK {
		return fmt.Errorf("slack: %s failed: %s", method, status.Error)
	}
	if into != nil {
		return json.Unmarshal(raw, into)
	}
	return nil
}

func ruleKey(r domain.Rule) string {
	return fmt.Sprintf("%s|%s|%v|%s", r.Stream.Path(), r.Op, r.Threshold, r.Channel)
}

// Text renders a fresh message. Every message starts with "ALERT:<path>"
// or "RESOLVED:<path>" after its icon, so Slack keyword notifications can
// key on either word.
func Text(nt domain.Notification, loc *time.Location) string {
	r := nt.Rule
	value := domain.Display(nt.Value, nt.Unit)
	at := nt.Time.In(loc).Format(timeLayout)
	path := r.Stream.Path()
	if r.Op == domain.OpAbsent {
		if nt.Event == domain.Resolved {
			return fmt.Sprintf(":white_check_mark: RESOLVED:%s is reporting again\nlatest: %s at [%s]", path, value, at)
		}
		return fmt.Sprintf(":rotating_light: ALERT:%s has had no samples for %s\nchecked at [%s]", path, r.For, at)
	}
	if nt.Event == domain.Resolved {
		return fmt.Sprintf(":white_check_mark: RESOLVED:%s is no longer %s %s\nlatest: %s at [%s]",
			path, r.Op, threshold(r, nt.Unit), value, at)
	}
	return fmt.Sprintf(":rotating_light: ALERT:%s has been %s %s for %s\nlatest: %s at [%s]",
		path, r.Op, threshold(r, nt.Unit), r.For, value, at)
}

// ResolvedText renders the resolution of a known alert: the RESOLVED line
// first, then the original ALERT line, the detection and the resolution.
// It replaces the ALERT message when Slack allows editing.
func ResolvedText(fired, resolved domain.Notification, loc *time.Location) string {
	r := fired.Rule
	path := r.Stream.Path()
	detectedAt := fired.Time.In(loc).Format(timeLayout)
	resolvedAt := resolved.Time.In(loc).Format(timeLayout)
	latest := domain.Display(resolved.Value, resolved.Unit)
	if r.Op == domain.OpAbsent {
		return fmt.Sprintf(":white_check_mark: RESOLVED:%s is reporting again\nALERT:%s had no samples for %s\ndetected at [%s]\nresolved: %s at [%s]",
			path, path, r.For, detectedAt, latest, resolvedAt)
	}
	th := threshold(r, fired.Unit)
	return fmt.Sprintf(":white_check_mark: RESOLVED:%s is no longer %s %s\nALERT:%s was %s %s for %s\ndetected: %s at [%s]\nresolved: %s at [%s]",
		path, r.Op, th, path, r.Op, th, r.For, domain.Display(fired.Value, fired.Unit), detectedAt, latest, resolvedAt)
}

func threshold(r domain.Rule, unit string) string {
	if domain.IsBytes(unit) {
		return domain.Bytes(r.Threshold)
	}
	return strconv.FormatFloat(r.Threshold, 'f', -1, 64)
}
