package main

import (
	"errors"
	"fmt"

	"github.com/apcandsons/otellite/internal/adapter/alertconf"
)

// validateAlerts parses the rules file and returns a one-line summary, so
// an image build can fail on a bad alert.conf before anything runs.
func validateAlerts(path string) (string, error) {
	if path == "" {
		return "", errors.New("-validate needs -alerts <file>")
	}
	cfg, err := alertconf.Load(path)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d rules, %d channels", len(cfg.Rules), len(cfg.Channels)), nil
}
