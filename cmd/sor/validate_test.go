package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConf(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "alert.conf")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestValidateAlertsSummarisesAGoodFile(t *testing.T) {
	p := writeConf(t, `channel ops slack https://hooks.slack.com/services/T000/B000/XXXX
alert /iam/iam-api/metrics/go.memory.used.dat > 500000000 for 3m to ops
alert /iam/iam-api/metrics/process.uptime.dat absent for 3m to ops
`)
	got, err := validateAlerts(p)
	if err != nil {
		t.Fatal(err)
	}
	if got != "2 rules, 1 channels" {
		t.Fatalf("got %q", got)
	}
}

func TestValidateAlertsExpandsEnvironment(t *testing.T) {
	p := writeConf(t, "channel ops slack ${SLACK_WEBHOOK_URL}\n")
	if _, err := validateAlerts(p); err == nil || !strings.Contains(err.Error(), "${SLACK_WEBHOOK_URL} is not set") {
		t.Fatalf("unset variable should fail validation: %v", err)
	}
	t.Setenv("SLACK_WEBHOOK_URL", "dummy")
	got, err := validateAlerts(p)
	if err != nil {
		t.Fatal(err)
	}
	if got != "0 rules, 1 channels" {
		t.Fatalf("got %q", got)
	}
}

func TestValidateAlertsRejectsABadFile(t *testing.T) {
	p := writeConf(t, "alert /iam/iam-api/metrics/x.dat > 1 for 3m to nowhere\n")
	if _, err := validateAlerts(p); err == nil {
		t.Fatal("expected an error for an undeclared channel")
	}
	if _, err := validateAlerts(filepath.Join(t.TempDir(), "missing.conf")); err == nil {
		t.Fatal("expected an error for a missing file")
	}
	if _, err := validateAlerts(""); err == nil {
		t.Fatal("expected an error when no -alerts path is given")
	}
}
