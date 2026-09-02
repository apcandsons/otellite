// Package slack delivers notifications through Slack incoming webhooks.
package slack

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/apcandsons/otellite/internal/domain"
)

const timeLayout = "2006-01-02 15:04:05 MST"

// Notifier posts to one webhook URL per channel name.
type Notifier struct {
	webhooks map[string]string
	http     *http.Client
	loc      *time.Location
}

func New(webhooks map[string]string, hc *http.Client, loc *time.Location) *Notifier {
	if hc == nil {
		hc = &http.Client{Timeout: 5 * time.Second}
	}
	if loc == nil {
		loc = time.Local
	}
	return &Notifier{webhooks: webhooks, http: hc, loc: loc}
}

// Notify posts the notification to its channel's webhook.
func (n *Notifier) Notify(nt domain.Notification) error {
	url, ok := n.webhooks[nt.Rule.Channel]
	if !ok {
		return fmt.Errorf("slack: no webhook for channel %q", nt.Rule.Channel)
	}
	body, _ := json.Marshal(map[string]string{"text": Text(nt, n.loc)})
	resp, err := n.http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("slack: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("slack: webhook returned %d: %s", resp.StatusCode, bytes.TrimSpace(msg))
	}
	return nil
}

// Text renders the message body.
func Text(nt domain.Notification, loc *time.Location) string {
	r := nt.Rule
	value := nt.Value
	if nt.Unit != "" {
		value += " " + nt.Unit
	}
	return fmt.Sprintf(":rotating_light: %s has been %s %s for %s\nlatest: %s at [%s]",
		r.Stream.Path(), r.Op, strconv.FormatFloat(r.Threshold, 'f', -1, 64), r.For,
		value, nt.Time.In(loc).Format(timeLayout))
}
