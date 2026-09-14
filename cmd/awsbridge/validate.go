package main

import (
	"errors"
	"fmt"

	"github.com/apcandsons/otellite/internal/adapter/bridgeconf"
)

// validateBridge parses the targets file and returns a one-line summary,
// so an image build can fail on a bad bridge.conf before anything runs. It
// needs neither AWS credentials nor the network.
func validateBridge(path string) (string, error) {
	if path == "" {
		return "", errors.New("-validate needs -conf <file>")
	}
	cfg, err := bridgeconf.Load(path)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d targets, region %s, every %s", len(cfg.Targets), cfg.Region, cfg.Every), nil
}
