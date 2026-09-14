package domain

import (
	"testing"
)

func TestParseTargetKind(t *testing.T) {
	for _, s := range []string{"ecs", "sqs", "rds"} {
		k, ok := ParseTargetKind(s)
		if !ok || k.String() != s {
			t.Fatalf("%q: got %v %v", s, k, ok)
		}
	}
	if _, ok := ParseTargetKind("ec2"); ok {
		t.Fatal("ec2 is not a target kind")
	}
}

func TestTargetValidate(t *testing.T) {
	good := Target{Kind: TargetSQS, ID: "comm-staging-embed-dlq", Namespace: "comm", Service: "queues", Prefix: "embed_dlq"}
	if err := good.Validate(); err != nil {
		t.Fatalf("valid target rejected: %v", err)
	}
	ecs := Target{Kind: TargetECS, ID: "comm-staging/comm-bff", Namespace: "comm", Service: "comm-bff", Prefix: "ecs"}
	if err := ecs.Validate(); err != nil {
		t.Fatalf("valid ecs target rejected: %v", err)
	}
	bad := []Target{
		{Kind: TargetSQS, Namespace: "comm", Service: "queues", Prefix: "q"},                    // no id
		{Kind: TargetSQS, ID: "q", Service: "queues", Prefix: "q"},                              // no namespace
		{Kind: TargetSQS, ID: "q", Namespace: "comm", Prefix: "q"},                              // no service
		{Kind: TargetSQS, ID: "q", Namespace: "comm", Service: "queues"},                        // no prefix
		{Kind: TargetSQS, ID: "q", Namespace: "comm", Service: "queues", Prefix: "Bad-1"},       // prefix charset
		{Kind: TargetECS, ID: "comm-staging", Namespace: "comm", Service: "bff", Prefix: "ecs"}, // ecs id needs cluster/service
		{ID: "q", Namespace: "comm", Service: "queues", Prefix: "q"},                            // zero kind
	}
	for i, tg := range bad {
		if err := tg.Validate(); err == nil {
			t.Errorf("case %d: expected an error for %+v", i, tg)
		}
	}
}

func TestTargetMetricNameAndPath(t *testing.T) {
	tg := Target{Kind: TargetECS, ID: "comm-staging/comm-bff", Namespace: "comm", Service: "comm-bff", Prefix: "ecs"}
	if got := tg.MetricName("running_tasks"); got != "ecs.running_tasks" {
		t.Fatalf("got %q", got)
	}
	if got := tg.Path(); got != "comm/comm-bff/ecs" {
		t.Fatalf("got %q", got)
	}
	cluster, service := tg.ECSParts()
	if cluster != "comm-staging" || service != "comm-bff" {
		t.Fatalf("got %q %q", cluster, service)
	}
}
