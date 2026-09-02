package alertconf_test

import (
	"strings"
	"testing"
	"time"

	"github.com/apcandsons/otellite/internal/adapter/alertconf"
	"github.com/apcandsons/otellite/internal/domain"
)

const good = `
# Where alerts go. Only slack is supported for now.
channel ops slack https://hooks.slack.com/services/T000/B000/XXXX
channel oncall slack https://hooks.slack.com/services/T000/B000/YYYY

# alert <path> <op> <threshold> for <duration> to <channel>
alert /iam/iam-api/metrics/go.memory.used.dat > 500000000 for 3m to ops
alert /web/front/metrics/http.duration.sum.dat >= 1.5 for 30s to oncall
`

func TestParseGoodConfig(t *testing.T) {
	cfg, err := alertconf.Parse(strings.NewReader(good))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Channels) != 2 || cfg.Channels[0] != (alertconf.Channel{Name: "ops", Type: "slack", URL: "https://hooks.slack.com/services/T000/B000/XXXX"}) {
		t.Errorf("channels = %+v", cfg.Channels)
	}
	if len(cfg.Rules) != 2 {
		t.Fatalf("rules = %+v", cfg.Rules)
	}
	want := domain.Rule{
		Stream:    domain.StreamID{Namespace: "iam", Service: "iam-api", Kind: domain.Metrics, Name: "go.memory.used"},
		Op:        domain.OpGreater,
		Threshold: 500000000,
		For:       3 * time.Minute,
		Channel:   "ops",
	}
	if cfg.Rules[0] != want {
		t.Errorf("rule[0] = %+v, want %+v", cfg.Rules[0], want)
	}
	if cfg.Rules[1].Op != domain.OpGreaterEq || cfg.Rules[1].Threshold != 1.5 || cfg.Rules[1].For != 30*time.Second || cfg.Rules[1].Channel != "oncall" {
		t.Errorf("rule[1] = %+v", cfg.Rules[1])
	}
}

func TestParseRejectsBadLines(t *testing.T) {
	bad := map[string]string{
		"unknown directive": "frob x y",
		"unknown channel":   "alert /a/b/metrics/c.dat > 1 for 1m to nowhere",
		"non-slack channel": "channel x email ops@example.com\nalert /a/b/metrics/c.dat > 1 for 1m to x",
		"log stream":        "channel x slack http://h\nalert /a/b/logs/c.dat > 1 for 1m to x",
		"bad op":            "channel x slack http://h\nalert /a/b/metrics/c.dat == 1 for 1m to x",
		"bad threshold":     "channel x slack http://h\nalert /a/b/metrics/c.dat > lots for 1m to x",
		"bad duration":      "channel x slack http://h\nalert /a/b/metrics/c.dat > 1 for soon to x",
		"missing to":        "channel x slack http://h\nalert /a/b/metrics/c.dat > 1 for 1m",
		"duplicate channel": "channel x slack http://h\nchannel x slack http://h2",
	}
	for name, src := range bad {
		if _, err := alertconf.Parse(strings.NewReader(src)); err == nil {
			t.Errorf("%s: expected error", name)
		} else if !strings.Contains(err.Error(), "line ") {
			t.Errorf("%s: error should name the line: %v", name, err)
		}
	}
}

func TestParseEmptyIsFine(t *testing.T) {
	cfg, err := alertconf.Parse(strings.NewReader("# nothing here\n\n"))
	if err != nil || len(cfg.Rules) != 0 {
		t.Errorf("cfg=%+v err=%v", cfg, err)
	}
}
