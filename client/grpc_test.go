package client_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/apcandsons/otellite/client"
	"github.com/apcandsons/otellite/client/clienttest"
)

type fakeServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (f fakeServerStream) Context() context.Context { return f.ctx }

// fakeClientStream ends with the given status on RecvMsg, the way a real
// stream surfaces its final status.
type fakeClientStream struct {
	grpc.ClientStream
	final error
}

func (f fakeClientStream) RecvMsg(any) error { return f.final }

func TestGRPCServerInterceptorsClassifyStatusCodes(t *testing.T) {
	iamEnv(t)
	rcv := clienttest.NewReceiver(t)
	start(t, rcv, client.Options{Handler: slog.NewTextHandler(&buffer{}, nil)})
	ctx := context.Background()

	unary := client.UnaryServerInterceptor(client.WithSkipMethods("/grpc.health.v1.Health/"))
	stream := client.StreamServerInterceptor(client.WithSkipMethods("/grpc.health.v1.Health/"))

	call := func(method string, err error) {
		t.Helper()
		_, got := unary(ctx, nil, &grpc.UnaryServerInfo{FullMethod: method}, func(context.Context, any) (any, error) { return nil, err })
		if !errors.Is(got, err) {
			t.Fatalf("interceptor swallowed the error: %v", got)
		}
	}
	call("/iam.v1.Authz/Check", nil)
	call("/iam.v1.Authz/Check", nil)
	call("/iam.v1.Authz/Check", status.Error(codes.PermissionDenied, "no"))
	call("/iam.v1.Authz/Check", status.Error(codes.NotFound, "no"))
	call("/iam.v1.Authz/Check", status.Error(codes.Internal, "boom"))
	call("/iam.v1.Authz/Check", errors.New("plain error is Unknown"))
	call("/grpc.health.v1.Health/Check", status.Error(codes.Internal, "skipped"))

	got := stream(nil, fakeServerStream{ctx: ctx}, &grpc.StreamServerInfo{FullMethod: "/iam.v1.Sync/Watch"}, func(any, grpc.ServerStream) error {
		return status.Error(codes.Canceled, "client went away")
	})
	if status.Code(got) != codes.Canceled {
		t.Fatalf("stream error not returned: %v", got)
	}
	if err := stream(nil, fakeServerStream{ctx: ctx}, &grpc.StreamServerInfo{FullMethod: "/iam.v1.Sync/Watch"}, func(any, grpc.ServerStream) error { return nil }); err != nil {
		t.Fatal(err)
	}
	_ = stream(nil, fakeServerStream{ctx: ctx}, &grpc.StreamServerInfo{FullMethod: "/grpc.health.v1.Health/Watch"}, func(any, grpc.ServerStream) error {
		return status.Error(codes.Unavailable, "skipped")
	})

	const m = "/iam/iam-api/metrics/rpc.server."
	// 6 unary + 2 streams counted; the health calls are skipped.
	if got := waitSum(t, rcv, m+"requests.dat", 8); got != 8 {
		t.Fatalf("requests = %v", got)
	}
	// Internal + Unknown are server errors.
	if got := waitSum(t, rcv, m+"errors.dat", 2); got != 2 {
		t.Fatalf("errors = %v", got)
	}
	// PermissionDenied, NotFound, Canceled are the caller's fault.
	if got := waitSum(t, rcv, m+"client_errors.dat", 3); got != 3 {
		t.Fatalf("client_errors = %v", got)
	}
	if got := waitSum(t, rcv, m+"duration.count.dat", 8); got != 8 {
		t.Fatalf("duration.count = %v", got)
	}
	s := rcv.Wait(m+"active.dat", 1, waitFor)
	if len(s) == 0 || s[len(s)-1].Value != "0" {
		t.Fatalf("active should settle at 0: %+v", s)
	}
}

func TestGRPCClientInterceptorsUsePrefix(t *testing.T) {
	iamEnv(t)
	rcv := clienttest.NewReceiver(t)
	start(t, rcv, client.Options{Handler: slog.NewTextHandler(&buffer{}, nil)})
	ctx := context.Background()

	unary := client.UnaryClientInterceptor("mkms")
	invoke := func(err error) {
		got := unary(ctx, "/gsm.v1.MasterKMS/Unwrap", nil, nil, nil, func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
			return err
		})
		if !errors.Is(got, err) {
			t.Fatalf("interceptor swallowed the error: %v", got)
		}
	}
	invoke(nil)
	invoke(status.Error(codes.Unavailable, "down"))
	invoke(status.Error(codes.InvalidArgument, "ours"))

	streamer := client.StreamClientInterceptor("mkms")
	open := func(final error) grpc.ClientStream {
		cs, err := streamer(ctx, &grpc.StreamDesc{}, nil, "/gsm.v1.MasterKMS/Watch", func(context.Context, *grpc.StreamDesc, *grpc.ClientConn, string, ...grpc.CallOption) (grpc.ClientStream, error) {
			return fakeClientStream{final: final}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return cs
	}
	if err := open(io.EOF).RecvMsg(nil); err != io.EOF {
		t.Fatalf("EOF must pass through: %v", err)
	}
	if err := open(status.Error(codes.DeadlineExceeded, "late")).RecvMsg(nil); status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("stream status must pass through: %v", err)
	}
	if _, err := streamer(ctx, &grpc.StreamDesc{}, nil, "/gsm.v1.MasterKMS/Watch", func(context.Context, *grpc.StreamDesc, *grpc.ClientConn, string, ...grpc.CallOption) (grpc.ClientStream, error) {
		return nil, status.Error(codes.Unavailable, "cannot open")
	}); status.Code(err) != codes.Unavailable {
		t.Fatalf("streamer error must pass through: %v", err)
	}

	const m = "/iam/iam-api/metrics/mkms."
	if got := waitSum(t, rcv, m+"requests.dat", 6); got != 6 {
		t.Fatalf("requests = %v", got)
	}
	// Unavailable (unary), DeadlineExceeded (stream), Unavailable (open).
	if got := waitSum(t, rcv, m+"errors.dat", 3); got != 3 {
		t.Fatalf("errors = %v", got)
	}
	if got := waitSum(t, rcv, m+"duration.count.dat", 6); got != 6 {
		t.Fatalf("duration.count = %v", got)
	}
}
