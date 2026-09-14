// Package bridgeconf parses bridge.conf, the line-oriented file that tells
// awsbridge which AWS resources to poll and where their samples land:
//
//	region <aws-region>
//	every <duration>                      # default 60s
//	ecs <cluster>/<service> as <ns>/<svc>/<prefix>
//	sqs <queue-name>        as <ns>/<svc>/<prefix>
//	rds <db-instance-id>    as <ns>/<svc>/<prefix>
//
// Samples land at /<ns>/<svc>/metrics/<prefix>.<name>.dat. Blank lines and
// lines starting with # are ignored; like alert.conf, comments are whole
// lines only. There is no environment expansion: nothing in this file is a
// secret.
package bridgeconf

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/apcandsons/otellite/internal/domain"
)

// Config is the parsed file.
type Config struct {
	Region  string
	Every   time.Duration
	Targets []domain.Target
}

// DefaultEvery is the poll interval when the file has no `every` line: one
// CloudWatch period, and the SoR's per-minute cadence.
const DefaultEvery = time.Minute

// Load parses the file at path.
func Load(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}
	defer f.Close()
	return Parse(f)
}

// Parse reads a config from r. A region is required; a duplicate
// <ns>/<svc>/<prefix> is an error because two targets would write the
// same streams.
func Parse(r io.Reader) (Config, error) {
	cfg := Config{Every: DefaultEvery}
	seen := map[string]int{}
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		var err error
		switch fields[0] {
		case "region":
			if len(fields) != 2 {
				err = errors.New("want: region <aws-region>")
			} else {
				cfg.Region = fields[1]
			}
		case "every":
			cfg.Every, err = parseEvery(fields[1:])
		default:
			var t domain.Target
			if t, err = parseTarget(fields); err == nil {
				if prev, dup := seen[t.Path()]; dup {
					err = fmt.Errorf("%s already declared on line %d", t.Path(), prev)
				} else {
					seen[t.Path()] = n
					cfg.Targets = append(cfg.Targets, t)
				}
			}
		}
		if err != nil {
			return Config{}, fmt.Errorf("bridge.conf line %d: %w", n, err)
		}
	}
	if err := sc.Err(); err != nil {
		return Config{}, err
	}
	if cfg.Region == "" {
		return Config{}, errors.New("bridge.conf: missing region")
	}
	return cfg, nil
}

func parseEvery(args []string) (time.Duration, error) {
	if len(args) != 1 {
		return 0, errors.New("want: every <duration>")
	}
	d, err := time.ParseDuration(args[0])
	if err != nil {
		return 0, fmt.Errorf("every: %w", err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("every %s: must be positive", args[0])
	}
	return d, nil
}

const targetUsage = "want: <ecs|sqs|rds> <id> as <ns>/<svc>/<prefix>"

func parseTarget(fields []string) (domain.Target, error) {
	kind, ok := domain.ParseTargetKind(fields[0])
	if !ok {
		return domain.Target{}, fmt.Errorf("unknown directive %q; %s", fields[0], targetUsage)
	}
	if len(fields) != 4 || fields[2] != "as" {
		return domain.Target{}, errors.New(targetUsage)
	}
	parts := strings.Split(fields[3], "/")
	if len(parts) != 3 {
		return domain.Target{}, fmt.Errorf("%q: %s", fields[3], targetUsage)
	}
	t := domain.Target{Kind: kind, ID: fields[1], Namespace: parts[0], Service: parts[1], Prefix: parts[2]}
	if err := t.Validate(); err != nil {
		return domain.Target{}, err
	}
	return t, nil
}
