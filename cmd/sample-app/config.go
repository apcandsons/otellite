package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// serviceConfig is one simulated service from the config file.
type serviceConfig struct {
	Namespace string
	Service   string
	RPS       float64
}

// loadConfig reads a config file. Format, one service per line:
//
//	service <namespace> <name> [rps=<n>]
//
// Blank lines and text after # are ignored. defaultRPS applies to services
// that do not set rps.
func loadConfig(path string, defaultRPS float64) ([]serviceConfig, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parseConfig(f, defaultRPS)
}

func parseConfig(r io.Reader, defaultRPS float64) ([]serviceConfig, error) {
	var out []serviceConfig
	seen := map[string]bool{}
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if fields[0] != "service" {
			return nil, fmt.Errorf("line %d: unknown directive %q (want: service <namespace> <name> [rps=<n>])", n, fields[0])
		}
		if len(fields) < 3 {
			return nil, fmt.Errorf("line %d: want: service <namespace> <name> [rps=<n>]", n)
		}
		svc := serviceConfig{Namespace: fields[1], Service: fields[2], RPS: defaultRPS}
		for _, name := range []string{svc.Namespace, svc.Service} {
			if strings.ContainsAny(name, "/ ") {
				return nil, fmt.Errorf("line %d: %q must not contain a slash", n, name)
			}
		}
		for _, opt := range fields[3:] {
			key, val, ok := strings.Cut(opt, "=")
			if !ok || key != "rps" {
				return nil, fmt.Errorf("line %d: unknown option %q (want rps=<n>)", n, opt)
			}
			rps, err := strconv.ParseFloat(val, 64)
			if err != nil || rps <= 0 {
				return nil, fmt.Errorf("line %d: bad rps %q", n, val)
			}
			svc.RPS = rps
		}
		key := svc.Namespace + "/" + svc.Service
		if seen[key] {
			return nil, fmt.Errorf("line %d: %s listed twice", n, key)
		}
		seen[key] = true
		out = append(out, svc)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no services configured")
	}
	return out, nil
}
