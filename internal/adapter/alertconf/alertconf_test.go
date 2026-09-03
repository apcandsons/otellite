package alertconf_test

import (
	"os"
	"path/filepath"
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

func envOf(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

func TestParseEnvExpandsChannelLine(t *testing.T) {
	env := envOf(map[string]string{"SLACK_WEBHOOK_URL": "https://hooks.slack.com/services/T000/B000/a=b&c=d"})
	cfg, err := alertconf.ParseEnv(strings.NewReader("channel ops slack ${SLACK_WEBHOOK_URL}\n"), env)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Channels) != 1 || cfg.Channels[0].URL != "https://hooks.slack.com/services/T000/B000/a=b&c=d" {
		t.Errorf("channels = %+v", cfg.Channels)
	}
}

func TestParseEnvExpandsInsideRulePath(t *testing.T) {
	env := envOf(map[string]string{"NS": "iam", "SVC": "iam-api", "LIMIT": "500"})
	cfg, err := alertconf.ParseEnv(strings.NewReader(`
channel ops slack https://example.com/hook
alert /${NS}/${SVC}/metrics/go.memory.used.dat > ${LIMIT} for 3m to ops
`), env)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Rules) != 1 {
		t.Fatalf("rules = %+v", cfg.Rules)
	}
	r := cfg.Rules[0]
	if r.Stream.Namespace != "iam" || r.Stream.Service != "iam-api" || r.Threshold != 500 {
		t.Errorf("rule = %+v", r)
	}
}

func TestParseEnvUnsetVariableNamesVariableAndLine(t *testing.T) {
	src := "# comment\nchannel ops slack https://example.com/hook\nchannel oncall slack ${SLACK_WEBHOOK_URL}\n"
	_, err := alertconf.ParseEnv(strings.NewReader(src), envOf(nil))
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "alert.conf line 3: ${SLACK_WEBHOOK_URL} is not set" {
		t.Errorf("err = %q", err)
	}
}

func TestParseEnvLeavesBareDollarAlone(t *testing.T) {
	env := envOf(map[string]string{"X": "expanded"})
	cfg, err := alertconf.ParseEnv(strings.NewReader("channel ops slack https://example.com/$X/${X}\n"), env)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Channels[0].URL != "https://example.com/$X/expanded" {
		t.Errorf("url = %q", cfg.Channels[0].URL)
	}
}

func TestParseEnvLiteralTextUnchanged(t *testing.T) {
	lookup := func(string) (string, bool) { t.Error("lookup called without a reference"); return "", false }
	cfg, err := alertconf.ParseEnv(strings.NewReader(good), lookup)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := alertconf.Parse(strings.NewReader(good))
	if len(cfg.Channels) != len(want.Channels) || cfg.Channels[0] != want.Channels[0] || len(cfg.Rules) != len(want.Rules) || cfg.Rules[0] != want.Rules[0] {
		t.Errorf("got %+v, want %+v", cfg, want)
	}
}

func TestParseEnvSkipsComments(t *testing.T) {
	src := "# set ${NOT_A_VAR} before running\nchannel ops slack https://example.com/hook\n"
	if _, err := alertconf.ParseEnv(strings.NewReader(src), envOf(nil)); err != nil {
		t.Errorf("comment should not be expanded: %v", err)
	}
}

func TestParseWithoutLookupDoesNotExpand(t *testing.T) {
	cfg, err := alertconf.Parse(strings.NewReader("channel ops slack ${SLACK_WEBHOOK_URL}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Channels[0].URL != "${SLACK_WEBHOOK_URL}" {
		t.Errorf("url = %q", cfg.Channels[0].URL)
	}
}

func TestLoadExpandsFromProcessEnvironment(t *testing.T) {
	t.Setenv("OTELLITE_TEST_HOOK", "https://example.com/from-env")
	p := filepath.Join(t.TempDir(), "alert.conf")
	if err := os.WriteFile(p, []byte("channel ops slack ${OTELLITE_TEST_HOOK}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := alertconf.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Channels[0].URL != "https://example.com/from-env" {
		t.Errorf("url = %q", cfg.Channels[0].URL)
	}
	if err := os.WriteFile(p, []byte("channel ops slack ${OTELLITE_TEST_UNSET}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := alertconf.Load(p); err == nil || !strings.Contains(err.Error(), "${OTELLITE_TEST_UNSET} is not set") {
		t.Errorf("err = %v", err)
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
