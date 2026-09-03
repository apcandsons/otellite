package client_test

import (
	"bytes"
	"context"
	"log"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/apcandsons/otellite/client"
	"github.com/apcandsons/otellite/client/clienttest"
	"github.com/apcandsons/otellite/internal/domain"
)

const (
	testInterval = 20 * time.Millisecond
	waitFor      = 5 * time.Second
)

// buffer is a goroutine-safe bytes.Buffer for capturing the app's handler.
type buffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *buffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *buffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

// iamEnv is the tenant contract: identity comes from the standard OTEL_*
// variables, never from code.
func iamEnv(t *testing.T) {
	t.Helper()
	t.Setenv("OTEL_SERVICE_NAME", "iam-api")
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "service.namespace=iam")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_METRIC_EXPORT_INTERVAL", "")
}

// start runs client.Start against the receiver with a short export
// interval and shuts it down when the test ends.
func start(t *testing.T, rcv *clienttest.Receiver, o client.Options) func(context.Context) error {
	t.Helper()
	if o.Endpoint == "" {
		o.Endpoint = rcv.URL()
	}
	if o.Interval == 0 {
		o.Interval = testInterval
	}
	shutdown, err := client.Start(context.Background(), o)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = shutdown(context.Background()) })
	return shutdown
}

func values(samples []domain.Sample) []string {
	out := make([]string, len(samples))
	for i, s := range samples {
		out[i] = s.Value
	}
	return out
}

// sum adds up a delta counter's samples: the export interval may fall
// between two increments, so only the total is deterministic.
func sum(samples []domain.Sample) float64 {
	var total float64
	for _, s := range samples {
		v, _ := strconv.ParseFloat(s.Value, 64)
		total += v
	}
	return total
}

// waitSum polls until the delta counter at path sums to at least want.
func waitSum(t *testing.T, rcv *clienttest.Receiver, path string, want float64) float64 {
	t.Helper()
	deadline := time.Now().Add(waitFor)
	for {
		got := sum(rcv.Samples(path))
		if got >= want || time.Now().After(deadline) {
			return got
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestStartWithoutEndpointIsANoOpExporter(t *testing.T) {
	iamEnv(t)
	var buf buffer
	shutdown, err := client.Start(context.Background(), client.Options{
		Handler: slog.NewTextHandler(&buf, nil),
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if shutdown == nil {
		t.Fatal("shutdown is nil")
	}
	client.Logger().Info("hello", "password", "hunter2")
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "msg=hello") {
		t.Fatalf("app handler did not receive the record: %q", out)
	}
	if strings.Contains(out, "hunter2") || !strings.Contains(out, "REDACTED") {
		t.Fatalf("redaction must apply without an endpoint: %q", out)
	}
	// Instruments still work; they just go nowhere.
	client.Counter("noop.requests", "1", "").Add(context.Background(), 1)
}

func TestDeltaCountersExportPerIntervalCounts(t *testing.T) {
	iamEnv(t)
	rcv := clienttest.NewReceiver(t)
	start(t, rcv, client.Options{Handler: slog.NewTextHandler(&buffer{}, nil)})
	ctx := context.Background()
	const path = "/iam/iam-api/metrics/x.dat"

	c := client.Counter("x", "1", "demo counter")
	c.Add(ctx, 3)
	if s := rcv.Wait(path, 1, waitFor); len(s) == 0 {
		t.Fatalf("no export at %s; streams: %v", path, rcv.Streams())
	}
	c.Add(ctx, 2)
	got := values(rcv.Wait(path, 2, waitFor))

	has := func(v string) bool {
		for _, g := range got {
			if g == v {
				return true
			}
		}
		return false
	}
	if !has("3") || !has("2") || has("5") {
		t.Fatalf("want delta samples 3 then 2 (never 5), got %v", got)
	}
	if s := rcv.Samples(path); s[0].Unit != "1" {
		t.Fatalf("unit = %q, want 1", s[0].Unit)
	}
}

func TestEndpointEnvIsHonouredAndOptionOverrides(t *testing.T) {
	iamEnv(t)
	rcv := clienttest.NewReceiver(t)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", rcv.URL())
	t.Setenv("OTEL_METRIC_EXPORT_INTERVAL", "20")
	shutdown, err := client.Start(context.Background(), client.Options{Handler: slog.NewTextHandler(&buffer{}, nil)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = shutdown(context.Background()) })
	client.Counter("env.requests", "1", "").Add(context.Background(), 1)
	if s := rcv.Wait("/iam/iam-api/metrics/env.requests.dat", 1, waitFor); len(s) == 0 {
		t.Fatalf("env endpoint not used; streams %v", rcv.Streams())
	}

	// An explicit Endpoint wins over the environment.
	other := clienttest.NewReceiver(t)
	shutdown2, err := client.Start(context.Background(), client.Options{Endpoint: other.URL(), Interval: testInterval, Handler: slog.NewTextHandler(&buffer{}, nil)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = shutdown2(context.Background()) })
	client.Counter("opt.requests", "1", "").Add(context.Background(), 1)
	if s := other.Wait("/iam/iam-api/metrics/opt.requests.dat", 1, waitFor); len(s) == 0 {
		t.Fatalf("Options.Endpoint not used; streams %v", other.Streams())
	}
}

func TestLogScopeIsSanitisedForThePath(t *testing.T) {
	iamEnv(t)
	t.Setenv("OTEL_SERVICE_NAME", "gateway")
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "service.namespace=gsm")
	rcv := clienttest.NewReceiver(t)
	shutdown := start(t, rcv, client.Options{Scope: "gsm/gateway", Handler: slog.NewTextHandler(&buffer{}, nil)})

	client.Logger().Warn("tls chain stale", "age", 42)
	if err := shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	const path = "/gsm/gateway/logs/gsm-gateway.dat"
	s := rcv.Samples(path)
	if len(s) != 1 {
		t.Fatalf("want one log sample at %s, streams %v", path, rcv.Streams())
	}
	if s[0].Value != "WARN tls chain stale age=42" {
		t.Fatalf("body = %q", s[0].Value)
	}
}

func TestScopeDefaultsToServiceNameThenDefault(t *testing.T) {
	iamEnv(t)
	rcv := clienttest.NewReceiver(t)
	shutdown := start(t, rcv, client.Options{Handler: slog.NewTextHandler(&buffer{}, nil)})
	client.Logger().Info("one")
	_ = shutdown(context.Background())
	if s := rcv.Samples("/iam/iam-api/logs/iam-api.dat"); len(s) != 1 {
		t.Fatalf("scope should default to OTEL_SERVICE_NAME; streams %v", rcv.Streams())
	}

	t.Setenv("OTEL_SERVICE_NAME", "")
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "")
	rcv2 := clienttest.NewReceiver(t)
	shutdown2 := start(t, rcv2, client.Options{Handler: slog.NewTextHandler(&buffer{}, nil)})
	client.Logger().Info("two")
	_ = shutdown2(context.Background())
	if s := rcv2.Samples("/default/unknown/logs/default.dat"); len(s) != 1 {
		t.Fatalf("scope should fall back to default; streams %v", rcv2.Streams())
	}
}

func TestRedactionAppliesToBothSinksIncludingGroups(t *testing.T) {
	iamEnv(t)
	rcv := clienttest.NewReceiver(t)
	var buf buffer
	shutdown := start(t, rcv, client.Options{Handler: slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})})

	l := client.Logger()
	l.Info("login", "user", "bob", "password", "hunter2", slog.Group("auth", slog.String("token", "abc")))
	l.WithGroup("auth").Error("refused", "token", "zzz", "reason", "expired")
	l.With("api_key", "k123").Debug("with attrs")
	if err := shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	for _, secret := range []string{"hunter2", "abc", "zzz", "k123"} {
		if strings.Contains(out, secret) {
			t.Errorf("app handler leaked %q: %s", secret, out)
		}
	}
	if !strings.Contains(out, "user=bob") || !strings.Contains(out, "reason=expired") {
		t.Errorf("non-secret attrs must survive: %s", out)
	}

	got := values(rcv.Samples("/iam/iam-api/logs/iam-api.dat"))
	want := []string{
		"INFO login user=bob password=[REDACTED] auth.token=[REDACTED]",
		"ERROR refused auth.token=[REDACTED] auth.reason=expired",
		"DEBUG with attrs api_key=[REDACTED]",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("otlp bodies:\n got %q\nwant %q", got, want)
	}
}

func TestCustomRedactPattern(t *testing.T) {
	iamEnv(t)
	var buf buffer
	shutdown, err := client.Start(context.Background(), client.Options{
		Handler: slog.NewTextHandler(&buf, nil),
		Redact:  client.DefaultRedact, // explicit default still redacts
	})
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown(context.Background())
	client.Logger().Info("x", "cookie", "c1", "name", "n1")
	if out := buf.String(); strings.Contains(out, "c1") || !strings.Contains(out, "name=n1") {
		t.Fatalf("%q", out)
	}
}

func TestStdLogBridgeEmitsInfoThroughBothSinks(t *testing.T) {
	iamEnv(t)
	rcv := clienttest.NewReceiver(t)
	var buf buffer
	shutdown := start(t, rcv, client.Options{Handler: slog.NewTextHandler(&buf, nil)})

	log.Printf("legacy line %d", 7)
	if err := shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if out := buf.String(); !strings.Contains(out, `msg="legacy line 7"`) || !strings.Contains(out, "level=INFO") {
		t.Fatalf("std log did not reach the app handler: %q", out)
	}
	if got := values(rcv.Samples("/iam/iam-api/logs/iam-api.dat")); len(got) != 1 || got[0] != "INFO legacy line 7" {
		t.Fatalf("otlp bodies = %q", got)
	}
}

func TestNoStdLogLeavesTheLogPackageAlone(t *testing.T) {
	iamEnv(t)
	var buf buffer
	var std buffer
	log.SetOutput(&std)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	shutdown, err := client.Start(context.Background(), client.Options{Handler: slog.NewTextHandler(&buf, nil), NoStdLog: true})
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown(context.Background())
	log.Print("untouched")
	if !strings.Contains(std.String(), "untouched") || strings.Contains(buf.String(), "untouched") {
		t.Fatalf("std=%q slog=%q", std.String(), buf.String())
	}
}

func TestProcessGaugesAreSane(t *testing.T) {
	iamEnv(t)
	rcv := clienttest.NewReceiver(t)
	start(t, rcv, client.Options{Handler: slog.NewTextHandler(&buffer{}, nil)})

	// Burn a little CPU so utilization has something to measure.
	x := 0
	for i := 0; i < 5_000_000; i++ {
		x += i % 7
	}
	_ = x

	check := func(name, unit string, ok func(v float64) bool) {
		t.Helper()
		path := "/iam/iam-api/metrics/" + name + ".dat"
		s := rcv.Wait(path, 2, waitFor)
		if len(s) < 2 {
			t.Fatalf("%s: no samples; streams %v", name, rcv.Streams())
		}
		last := s[len(s)-1]
		v, err := strconv.ParseFloat(last.Value, 64)
		if err != nil || !ok(v) || last.Unit != unit {
			t.Fatalf("%s: sample %+v not sane", name, last)
		}
	}
	check("process.cpu.utilization", "1", func(v float64) bool { return v >= 0 && v <= 1 })
	check("go.memory.used", "By", func(v float64) bool { return v > 1<<16 })
	check("go.goroutines", "1", func(v float64) bool { return v >= 1 })
	check("process.uptime", "s", func(v float64) bool { return v >= 0 && v < 3600 })
}

func TestNoProcessSkipsTheGauges(t *testing.T) {
	iamEnv(t)
	rcv := clienttest.NewReceiver(t)
	start(t, rcv, client.Options{NoProcess: true, Handler: slog.NewTextHandler(&buffer{}, nil)})
	client.Counter("only.this", "1", "").Add(context.Background(), 1)
	rcv.Wait("/iam/iam-api/metrics/only.this.dat", 1, waitFor)
	for _, p := range rcv.Streams() {
		if strings.Contains(p, "process.") || strings.Contains(p, "go.") {
			t.Fatalf("process gauge exported with NoProcess: %v", rcv.Streams())
		}
	}
}

func TestGaugeHelperObservesTheFunc(t *testing.T) {
	iamEnv(t)
	rcv := clienttest.NewReceiver(t)
	start(t, rcv, client.Options{Handler: slog.NewTextHandler(&buffer{}, nil)})
	client.Gauge("sks.cache.hit_ratio", "1", "hits over lookups", func() float64 { return 0.75 })
	s := rcv.Wait("/iam/iam-api/metrics/sks.cache.hit_ratio.dat", 1, waitFor)
	if len(s) == 0 || s[0].Value != "0.75" {
		t.Fatalf("samples %+v; streams %v", s, rcv.Streams())
	}
	// A bad name must not panic; it is logged.
	client.Gauge("", "1", "", func() float64 { return 1 })
}

func TestHistogramAndUpDownHelpers(t *testing.T) {
	iamEnv(t)
	rcv := clienttest.NewReceiver(t)
	start(t, rcv, client.Options{Handler: slog.NewTextHandler(&buffer{}, nil)})
	ctx := context.Background()
	client.Histogram("op.duration", "ms", "").Record(ctx, 12.5)
	client.Histogram("op.duration", "ms", "").Record(ctx, 7.5)
	client.UpDown("op.active", "1", "").Add(ctx, 3)
	client.UpDown("op.active", "1", "").Add(ctx, -1)
	if got := waitSum(t, rcv, "/iam/iam-api/metrics/op.duration.count.dat", 2); got != 2 {
		t.Fatalf("count = %v", got)
	}
	if got := waitSum(t, rcv, "/iam/iam-api/metrics/op.duration.sum.dat", 20); got != 20 {
		t.Fatalf("sum = %v", got)
	}
	s := rcv.Wait("/iam/iam-api/metrics/op.active.dat", 1, waitFor)
	if len(s) == 0 || s[len(s)-1].Value != "2" {
		t.Fatalf("updown %+v", s)
	}
}

func TestCounterSetOneInstrumentPerName(t *testing.T) {
	iamEnv(t)
	rcv := clienttest.NewReceiver(t)
	start(t, rcv, client.Options{Handler: slog.NewTextHandler(&buffer{}, nil)})
	ctx := context.Background()

	cs, err := client.NewCounterSet("authz.deny", "no_grant", "explicit_deny")
	if err != nil {
		t.Fatal(err)
	}
	cs.Add(ctx, "no_grant", 2)
	cs.Add(ctx, "explicit_deny", 1)
	cs.Add(ctx, "bogus", 5) // unknown name: no-op, no panic
	if got := waitSum(t, rcv, "/iam/iam-api/metrics/authz.deny.no_grant.dat", 2); got != 2 {
		t.Fatalf("no_grant = %v", got)
	}
	if got := waitSum(t, rcv, "/iam/iam-api/metrics/authz.deny.explicit_deny.dat", 1); got != 1 {
		t.Fatalf("explicit_deny = %v", got)
	}
	for _, p := range rcv.Streams() {
		if strings.Contains(p, "bogus") {
			t.Fatalf("unknown name created a stream: %v", rcv.Streams())
		}
	}
	if _, err := client.NewCounterSet("x", ""); err == nil {
		t.Fatal("empty name should be rejected")
	}
	var nilSet *client.CounterSet
	nilSet.Add(ctx, "anything", 1) // nil receiver is a no-op too
}
