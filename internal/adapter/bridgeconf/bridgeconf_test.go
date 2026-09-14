package bridgeconf

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/apcandsons/otellite/internal/domain"
)

const sample = `# comm on staging
region ap-northeast-1
every 30s
ecs comm-staging/comm-bff   as comm/comm-bff/ecs
sqs comm-staging-embed-dlq  as comm/queues/embed_dlq
rds comm-staging-db         as comm/comm-sor/rds
`

func TestParseReadsTargetsRegionAndInterval(t *testing.T) {
	cfg, err := Parse(strings.NewReader(sample))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Region != "ap-northeast-1" || cfg.Every != 30*time.Second {
		t.Fatalf("region/every: %+v", cfg)
	}
	want := []domain.Target{
		{Kind: domain.TargetECS, ID: "comm-staging/comm-bff", Namespace: "comm", Service: "comm-bff", Prefix: "ecs"},
		{Kind: domain.TargetSQS, ID: "comm-staging-embed-dlq", Namespace: "comm", Service: "queues", Prefix: "embed_dlq"},
		{Kind: domain.TargetRDS, ID: "comm-staging-db", Namespace: "comm", Service: "comm-sor", Prefix: "rds"},
	}
	if len(cfg.Targets) != len(want) {
		t.Fatalf("got %d targets: %+v", len(cfg.Targets), cfg.Targets)
	}
	for i := range want {
		if cfg.Targets[i] != want[i] {
			t.Errorf("target %d: got %+v want %+v", i, cfg.Targets[i], want[i])
		}
	}
}

func TestParseDefaultsEveryToOneMinute(t *testing.T) {
	cfg, err := Parse(strings.NewReader("region eu-west-1\nsqs q as a/b/c\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Every != time.Minute {
		t.Fatalf("every = %v", cfg.Every)
	}
}

func TestParseErrorsNameTheLine(t *testing.T) {
	cases := map[string]string{
		"missing region":     "sqs q as a/b/c\n",
		"unknown directive":  "region r\nec2 i-1 as a/b/c\n",
		"missing as":         "region r\nsqs q a/b/c\n",
		"short path":         "region r\nsqs q as a/b\n",
		"bad prefix":         "region r\nsqs q as a/b/C-1\n",
		"ecs without slash":  "region r\necs cluster as a/b/c\n",
		"duplicate path":     "region r\nsqs q1 as a/b/c\nsqs q2 as a/b/c\n",
		"bad every":          "region r\nevery soon\n",
		"non-positive every": "region r\nevery 0s\n",
		"trailing comment":   "region r\nsqs q as a/b/c # not allowed\n",
	}
	for name, body := range cases {
		_, err := Parse(strings.NewReader(body))
		if err == nil {
			t.Errorf("%s: expected an error", name)
			continue
		}
		if name != "missing region" && !strings.Contains(err.Error(), "bridge.conf line ") {
			t.Errorf("%s: error should name the line: %v", name, err)
		}
	}
}

func TestLoadReadsAFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bridge.conf")
	if err := os.WriteFile(p, []byte(sample), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Targets) != 3 {
		t.Fatalf("got %d targets", len(cfg.Targets))
	}
	if _, err := Load(filepath.Join(t.TempDir(), "missing.conf")); err == nil {
		t.Fatal("expected an error for a missing file")
	}
}
