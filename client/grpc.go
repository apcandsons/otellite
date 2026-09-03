package client

import (
	"context"
	"io"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/metric"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ServerOption configures the server interceptors.
type ServerOption func(*serverOptions)

type serverOptions struct {
	skip []string
}

// WithSkipMethods leaves RPCs whose full method name starts with any of
// the prefixes uninstrumented, e.g. "/grpc.health.v1.Health/" for a
// health check polled every few hundred milliseconds.
func WithSkipMethods(prefixes ...string) ServerOption {
	return func(o *serverOptions) { o.skip = append(o.skip, prefixes...) }
}

func (o *serverOptions) skipped(method string) bool {
	for _, p := range o.skip {
		if strings.HasPrefix(method, p) {
			return true
		}
	}
	return false
}

func applyServerOptions(opts []ServerOption) *serverOptions {
	o := &serverOptions{}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// callerCodes are outcomes the caller brought on itself; they are counted
// apart from server errors so client misuse does not page anyone.
var callerCodes = map[codes.Code]bool{
	codes.InvalidArgument:    true,
	codes.NotFound:           true,
	codes.AlreadyExists:      true,
	codes.PermissionDenied:   true,
	codes.Unauthenticated:    true,
	codes.FailedPrecondition: true,
	codes.OutOfRange:         true,
	codes.Canceled:           true,
}

type outcome int

const (
	outcomeOK outcome = iota
	outcomeCallerError
	outcomeServerError
)

func classify(err error) outcome {
	if err == nil {
		return outcomeOK
	}
	switch code := status.Code(err); {
	case code == codes.OK:
		return outcomeOK
	case callerCodes[code]:
		return outcomeCallerError
	default:
		return outcomeServerError
	}
}

// rpcInstruments is one side's set. clientErrors and active are nil for
// outbound hops, which only report requests, errors, and duration.
type rpcInstruments struct {
	requests, errors, clientErrors metric.Int64Counter
	duration                       metric.Float64Histogram
	active                         metric.Int64UpDownCounter
}

var serverInst lazy[*rpcInstruments]

func newServerInstruments() *rpcInstruments {
	return &rpcInstruments{
		requests:     Counter("rpc.server.requests", "1", "RPCs completed"),
		errors:       Counter("rpc.server.errors", "1", "RPCs that failed on the server side"),
		clientErrors: Counter("rpc.server.client_errors", "1", "RPCs rejected for the caller's mistake"),
		duration:     Histogram("rpc.server.duration", "ms", "RPC latency"),
		active:       UpDown("rpc.server.active", "1", "In-flight RPCs"),
	}
}

func newClientInstruments(prefix string) func() *rpcInstruments {
	return func() *rpcInstruments {
		return &rpcInstruments{
			requests: Counter(prefix+".requests", "1", "Outbound RPCs completed"),
			errors:   Counter(prefix+".errors", "1", "Outbound RPCs that failed"),
			duration: Histogram(prefix+".duration", "ms", "Outbound RPC latency"),
		}
	}
}

// begin marks an RPC in flight and returns the func that records its end.
func (in *rpcInstruments) begin(ctx context.Context) func(error) {
	start := time.Now()
	if in.active != nil {
		in.active.Add(ctx, 1)
	}
	return func(err error) {
		if in.active != nil {
			in.active.Add(ctx, -1)
		}
		in.requests.Add(ctx, 1)
		in.duration.Record(ctx, millis(time.Since(start)))
		switch classify(err) {
		case outcomeServerError:
			in.errors.Add(ctx, 1)
		case outcomeCallerError:
			if in.clientErrors != nil {
				in.clientErrors.Add(ctx, 1)
			}
		}
	}
}

// UnaryServerInterceptor reports rpc.server.{requests, errors,
// client_errors, duration, active} for unary RPCs.
func UnaryServerInterceptor(opts ...ServerOption) grpc.UnaryServerInterceptor {
	o := applyServerOptions(opts)
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if o.skipped(info.FullMethod) {
			return handler(ctx, req)
		}
		done := serverInst.get(newServerInstruments).begin(ctx)
		resp, err := handler(ctx, req)
		done(err)
		return resp, err
	}
}

// StreamServerInterceptor is UnaryServerInterceptor for streams; the
// whole stream counts as one RPC, ended by the handler's return.
func StreamServerInterceptor(opts ...ServerOption) grpc.StreamServerInterceptor {
	o := applyServerOptions(opts)
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if o.skipped(info.FullMethod) {
			return handler(srv, ss)
		}
		done := serverInst.get(newServerInstruments).begin(ss.Context())
		err := handler(srv, ss)
		done(err)
		return err
	}
}

// UnaryClientInterceptor reports <prefix>.{requests, errors, duration}
// for an outbound hop, e.g. prefix "mkms" on the connection to master-kms.
func UnaryClientInterceptor(prefix string) grpc.UnaryClientInterceptor {
	var inst lazy[*rpcInstruments]
	build := newClientInstruments(prefix)
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		done := inst.get(build).begin(ctx)
		err := invoker(ctx, method, req, reply, cc, opts...)
		done(err)
		return err
	}
}

// StreamClientInterceptor is UnaryClientInterceptor for streams. A stream
// ends when RecvMsg returns: io.EOF is success, anything else its status.
func StreamClientInterceptor(prefix string) grpc.StreamClientInterceptor {
	var inst lazy[*rpcInstruments]
	build := newClientInstruments(prefix)
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		done := inst.get(build).begin(ctx)
		cs, err := streamer(ctx, desc, cc, method, opts...)
		if err != nil {
			done(err)
			return nil, err
		}
		return &clientStream{ClientStream: cs, done: done}, nil
	}
}

type clientStream struct {
	grpc.ClientStream
	done func(error)
	once sync.Once
}

func (s *clientStream) RecvMsg(m any) error {
	err := s.ClientStream.RecvMsg(m)
	if err != nil {
		s.once.Do(func() {
			if err == io.EOF {
				s.done(nil)
			} else {
				s.done(err)
			}
		})
	}
	return err
}
