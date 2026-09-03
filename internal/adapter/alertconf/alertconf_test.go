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

func TestParseAbsentRule(t *testing.T) {
	cfg, err := alertconf.Parse(strings.NewReader(`
channel ops slack https://example.com/hook
alert /iam/iam-api/metrics/process.cpu.utilization.dat absent for 30s to ops
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Rules) != 1 {
		t.Fatalf("rules = %+v", cfg.Rules)
	}
	r := cfg.Rules[0]
	if r.Op != domain.OpAbsent || r.Threshold != 0 || r.For != 30*time.Second || r.Channel != "ops" || r.Stream.Name != "process.cpu.utilization" {
		t.Errorf("rule = %+v", r)
	}
	for _, bad := range []string{
		"alert /iam/iam-api/metrics/x.dat absent 30s to ops",       // missing "for"
		"alert /iam/iam-api/metrics/x.dat absent for 30s ops",      // missing "to"
		"alert /iam/iam-api/metrics/x.dat absent 5 for 30s to ops", // no threshold allowed
	} {
		if _, err := alertconf.Parse(strings.NewReader("channel ops slack https://example.com/hook\n" + bad)); err == nil {
			t.Errorf("%q: expected error", bad)
		}
	}
}

func TestParseSlackBotChannel(t *testing.T) {
	cfg, err := alertconf.Parse(strings.NewReader(`
channel ops slack https://example.com/hook
channel oncall slack-bot xoxb-123-abc C0123456
alert /iam/iam-api/metrics/go.memory.used.dat > 1 for 1s to oncall
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Channels) != 2 {
		t.Fatalf("channels = %+v", cfg.Channels)
	}
	bot := cfg.Channels[1]
	if bot.Name != "oncall" || bot.Type != "slack-bot" || bot.Token != "xoxb-123-abc" || bot.ChannelID != "C0123456" || bot.URL != "" {
		t.Errorf("bot channel = %+v", bot)
	}
	for _, bad := range []string{
		"channel oncall slack-bot xoxb-123",           // missing channel id
		"channel oncall slack-bot xoxb-123 C01 extra", // too many
		"channel oncall teams https://example.com",    // unknown type
	} {
		if _, err := alertconf.Parse(strings.NewReader(bad)); err == nil {
			t.Errorf("%q: expected error", bad)
		}
	}
}
