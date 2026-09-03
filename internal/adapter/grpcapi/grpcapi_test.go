package grpcapi_test

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/apcandsons/otellite/internal/adapter/grpcapi"
	sorv1 "github.com/apcandsons/otellite/internal/adapter/grpcapi/pb/otellite/v1"
	"github.com/apcandsons/otellite/internal/domain"
	"github.com/apcandsons/otellite/internal/usecase"
)

var (
	t0    = time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC)
	memID = domain.StreamID{Namespace: "iam", Service: "iam-api", Kind: domain.Metrics, Name: "go.memory.used"}
	rule  = domain.Rule{Stream: memID, Op: domain.OpGreater, Threshold: 100, For: time.Minute, Channel: "ops"}
)

type fakeBrowser struct{ samples map[string][]domain.Sample }

func (f fakeBrowser) Ls(path string) ([]usecase.Entry, error) {
	if path != "/" {
		return nil, errors.Join(errors.New(path), domain.ErrNotFound)
	}
	return []usecase.Entry{{Name: "iam", Dir: true}}, nil
}

func (f fakeBrowser) Cat(path string) ([]domain.Sample, error) {
	ss, ok := f.samples[path]
	if !ok {
		return nil, errors.Join(errors.New(path), domain.ErrNotFound)
	}
	return ss, nil
}

type fakeAlerts struct{ status []usecase.RuleStatus }

func (f fakeAlerts) Status() []usecase.RuleStatus { return f.status }

func dial(t *testing.T, srv *grpcapi.Server) sorv1.SoRServiceClient {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	gs := grpc.NewServer()
	srv.Register(gs)
	go gs.Serve(lis)
	conn, err := grpc.NewClient("passthrough:///bufconn",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close(); gs.Stop() })
	return sorv1.NewSoRServiceClient(conn)
}

func TestLsAndCat(t *testing.T) {
	b := fakeBrowser{samples: map[string][]domain.Sample{
		"/iam/iam-api/metrics/go.memory.used.dat": {{Time: t0, Value: "42", Unit: "By"}},
	}}
	c := dial(t, grpcapi.New(b, nil, nil))
	ctx := context.Background()

	ls, err := c.Ls(ctx, &sorv1.LsRequest{Path: "/"})
	if err != nil || len(ls.Entries) != 1 || ls.Entries[0].Name != "iam" || !ls.Entries[0].Dir {
		t.Errorf("ls = %v, %v", ls, err)
	}
	cat, err := c.Cat(ctx, &sorv1.CatRequest{Path: "/iam/iam-api/metrics/go.memory.used.dat"})
	if err != nil || len(cat.Samples) != 1 {
		t.Fatalf("cat = %v, %v", cat, err)
	}
	if s := cat.Samples[0]; s.Value != "42" || s.Unit != "By" || !s.Time.AsTime().Equal(t0) {
		t.Errorf("sample = %v", s)
	}
	_, err = c.Cat(ctx, &sorv1.CatRequest{Path: "/nope.dat"})
	if status.Code(err) != codes.NotFound {
		t.Errorf("missing stream code = %v", status.Code(err))
	}
	_, err = c.Ls(ctx, &sorv1.LsRequest{Path: "relative"})
	if status.Code(err) != codes.NotFound {
		t.Errorf("bad path code = %v", status.Code(err))
	}
}

func TestRulesWithAndWithoutAlerting(t *testing.T) {
	ctx := context.Background()
	c := dial(t, grpcapi.New(fakeBrowser{}, fakeAlerts{[]usecase.RuleStatus{{Rule: rule, Firing: true}}}, nil))
	resp, err := c.Rules(ctx, &sorv1.RulesRequest{})
	if err != nil || len(resp.Rules) != 1 {
		t.Fatalf("rules = %v, %v", resp, err)
	}
	got := resp.Rules[0]
	if !got.Firing || got.Rule.Path != "/iam/iam-api/metrics/go.memory.used.dat" || got.Rule.Op != ">" ||
		got.Rule.Threshold != 100 || got.Rule.HoldFor.AsDuration() != time.Minute || got.Rule.Channel != "ops" {
		t.Errorf("rule = %v", got)
	}

	none := dial(t, grpcapi.New(fakeBrowser{}, nil, nil))
	resp, err = none.Rules(ctx, &sorv1.RulesRequest{})
	if err != nil || len(resp.Rules) != 0 {
		t.Errorf("no alerting: rules = %v, %v", resp, err)
	}
}

func TestWatchStreamsSamplesAndAlerts(t *testing.T) {
	feed := usecase.NewFeed(8)
	c := dial(t, grpcapi.New(fakeBrowser{}, nil, feed))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, err := c.Watch(ctx, &sorv1.WatchRequest{})
	if err != nil {
		t.Fatal(err)
	}
	// Wait until the server has subscribed before publishing.
	deadline := time.Now().Add(2 * time.Second)
	for feed.Subscribers() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	feed.Ingest(memID, domain.Sample{Time: t0, Value: "150", Unit: "By"})
	feed.Notify(domain.Notification{Rule: rule, Event: domain.Fired, Time: t0, Value: "150", Unit: "By"})

	ev, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if ev.Path != "/iam/iam-api/metrics/go.memory.used.dat" || ev.Sample.Value != "150" || ev.Alert != nil {
		t.Errorf("sample event = %v", ev)
	}
	ev, err = stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if ev.Alert == nil || ev.Alert.State != sorv1.AlertState_ALERT_STATE_FIRED || ev.Alert.Rule.Threshold != 100 || ev.Sample.Value != "150" {
		t.Errorf("alert event = %v", ev)
	}

	cancel()
	deadline = time.Now().Add(2 * time.Second)
	for feed.Subscribers() != 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if feed.Subscribers() != 0 {
		t.Error("server should unsubscribe when the client goes away")
	}
}
