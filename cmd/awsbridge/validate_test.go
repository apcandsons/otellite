package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeConf(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "bridge.conf")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestValidateBridgeSummarisesAGoodFile(t *testing.T) {
	p := writeConf(t, `region ap-northeast-1
every 30s
ecs comm-staging/comm-bff as comm/comm-bff/ecs
sqs comm-staging-embed-dlq as comm/queues/embed_dlq
`)
	got, err := validateBridge(p)
	if err != nil {
		t.Fatal(err)
	}
	if got != "2 targets, region ap-northeast-1, every 30s" {
		t.Fatalf("got %q", got)
	}
}

func TestValidateBridgeRejectsABadFile(t *testing.T) {
	if _, err := validateBridge(writeConf(t, "sqs q as a/b/c\n")); err == nil {
		t.Fatal("expected an error for a missing region")
	}
	if _, err := validateBridge(writeConf(t, "region r\nec2 i as a/b/c\n")); err == nil {
		t.Fatal("expected an error for an unknown kind")
	}
	if _, err := validateBridge(filepath.Join(t.TempDir(), "missing.conf")); err == nil {
		t.Fatal("expected an error for a missing file")
	}
	if _, err := validateBridge(""); err == nil {
		t.Fatal("expected an error when no -conf path is given")
	}
}
