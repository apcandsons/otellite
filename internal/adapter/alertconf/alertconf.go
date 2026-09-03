// Package alertconf parses alert.conf, a line-oriented file that declares
// notification channels and threshold rules:
//
//	channel <name> slack <webhook-url>
//	channel <name> slack-bot <bot-token> <channel-id>
//	alert <path> <op> <threshold> for <duration> to <channel>
//	alert <path> absent for <duration> to <channel>
//
// A slack channel posts through an incoming webhook. A slack-bot channel
// posts through the Web API with a bot token (chat:write scope), which
// also lets a resolved alert edit its original message.
//
// Blank lines and lines starting with # are ignored.
package alertconf

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/apcandsons/otellite/internal/domain"
)

// Channel is a named destination for notifications. URL is set for the
// slack type; Token and ChannelID for slack-bot.
type Channel struct {
	Name      string
	Type      string
	URL       string
	Token     string
	ChannelID string
}

// Config is the parsed file.
type Config struct {
	Channels []Channel
	Rules    []domain.Rule
}

// Load parses the file at path.
func Load(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}
	defer f.Close()
	return Parse(f)
}

// Parse reads a config from r. Rules may only reference channels declared
// earlier in the file.
func Parse(r io.Reader) (Config, error) {
	var cfg Config
	seen := map[string]bool{}
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		fields := strings.Fields(sc.Text())
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		var err error
		switch fields[0] {
		case "channel":
			var ch Channel
			if ch, err = parseChannel(fields[1:]); err == nil {
				if seen[ch.Name] {
					err = fmt.Errorf("channel %q declared twice", ch.Name)
				} else {
					seen[ch.Name] = true
					cfg.Channels = append(cfg.Channels, ch)
				}
			}
		case "alert":
			var rule domain.Rule
			if rule, err = parseAlert(fields[1:]); err == nil {
				if !seen[rule.Channel] {
					err = fmt.Errorf("unknown channel %q", rule.Channel)
				} else {
					cfg.Rules = append(cfg.Rules, rule)
				}
			}
		default:
			err = fmt.Errorf("unknown directive %q", fields[0])
		}
		if err != nil {
			return Config{}, fmt.Errorf("alert.conf line %d: %w", n, err)
		}
	}
	if err := sc.Err(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

const channelUsage = "want: channel <name> slack <webhook-url>, or channel <name> slack-bot <bot-token> <channel-id>"

func parseChannel(args []string) (Channel, error) {
	if len(args) < 2 {
		return Channel{}, fmt.Errorf("%s", channelUsage)
	}
	switch args[1] {
	case "slack":
		if len(args) != 3 {
			return Channel{}, fmt.Errorf("%s", channelUsage)
		}
		return Channel{Name: args[0], Type: args[1], URL: args[2]}, nil
	case "slack-bot":
		if len(args) != 4 {
			return Channel{}, fmt.Errorf("%s", channelUsage)
		}
		return Channel{Name: args[0], Type: args[1], Token: args[2], ChannelID: args[3]}, nil
	}
	return Channel{}, fmt.Errorf("unsupported channel type %q (want slack or slack-bot)", args[1])
}

const alertUsage = "want: alert <path> <op> <threshold> for <duration> to <channel>, or alert <path> absent for <duration> to <channel>"

func parseAlert(args []string) (domain.Rule, error) {
	// The absent form has no threshold: splice a placeholder in so both
	// forms share one shape: path op threshold for duration to channel.
	if len(args) >= 2 && args[1] == "absent" {
		args = append([]string{args[0], args[1], "0"}, args[2:]...)
	}
	if len(args) != 7 || args[3] != "for" || args[5] != "to" {
		return domain.Rule{}, fmt.Errorf("%s", alertUsage)
	}
	p, err := domain.ParsePath(args[0])
	if err != nil {
		return domain.Rule{}, err
	}
	if p.Depth() != domain.DepthStream || p.Kind != domain.Metrics {
		return domain.Rule{}, fmt.Errorf("%q is not a metrics .dat file", args[0])
	}
	op, ok := domain.ParseOp(args[1])
	if !ok {
		return domain.Rule{}, fmt.Errorf("bad comparison %q (want >, >=, <, <=, absent)", args[1])
	}
	threshold, err := strconv.ParseFloat(args[2], 64)
	if err != nil {
		return domain.Rule{}, fmt.Errorf("bad threshold %q", args[2])
	}
	d, err := time.ParseDuration(args[4])
	if err != nil {
		return domain.Rule{}, fmt.Errorf("bad duration %q", args[4])
	}
	return domain.Rule{Stream: p.StreamID(), Op: op, Threshold: threshold, For: d, Channel: args[6]}, nil
}
