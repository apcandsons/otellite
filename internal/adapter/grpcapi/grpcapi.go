// Package grpcapi exposes the system of record over gRPC: the browse use
// case, the alert rules with their state, and a live feed of samples and
// alert transitions. The web UI is its main consumer.
package grpcapi

import (
	"context"
	"errors"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	sorv1 "github.com/apcandsons/otellite/internal/adapter/grpcapi/pb/otellite/v1"
	"github.com/apcandsons/otellite/internal/domain"
	"github.com/apcandsons/otellite/internal/usecase"
)

// Browser answers ls and cat.
type Browser interface {
	Ls(path string) ([]usecase.Entry, error)
	Cat(path string) ([]domain.Sample, error)
}

// Alerts reports the configured rules and their state.
type Alerts interface {
	Status() []usecase.RuleStatus
}

// Feed hands out live event subscriptions.
type Feed interface {
	Subscribe() (<-chan usecase.Event, func())
}

// Server implements the SoRService. Alerts and feed may be nil, in which
// case Rules is empty and Watch blocks until the client goes away.
type Server struct {
	sorv1.UnimplementedSoRServiceServer
	browser Browser
	alerts  Alerts
	feed    Feed
}

func New(b Browser, a Alerts, f Feed) *Server {
	return &Server{browser: b, alerts: a, feed: f}
}

// Register attaches the service to a gRPC server.
func (s *Server) Register(gs *grpc.Server) { sorv1.RegisterSoRServiceServer(gs, s) }

func (s *Server) Ls(_ context.Context, req *sorv1.LsRequest) (*sorv1.LsResponse, error) {
	entries, err := s.browser.Ls(req.Path)
	if err != nil {
		return nil, toStatus(err)
	}
	out := make([]*sorv1.Entry, 0, len(entries))
	for _, e := range entries {
		out = append(out, &sorv1.Entry{Name: e.Name, Dir: e.Dir})
	}
	return &sorv1.LsResponse{Entries: out}, nil
}

func (s *Server) Cat(_ context.Context, req *sorv1.CatRequest) (*sorv1.CatResponse, error) {
	samples, err := s.browser.Cat(req.Path)
	if err != nil {
		return nil, toStatus(err)
	}
	out := make([]*sorv1.Sample, 0, len(samples))
	for _, smp := range samples {
		out = append(out, toSample(smp))
	}
	return &sorv1.CatResponse{Samples: out}, nil
}

func (s *Server) Rules(context.Context, *sorv1.RulesRequest) (*sorv1.RulesResponse, error) {
	resp := &sorv1.RulesResponse{}
	if s.alerts == nil {
		return resp, nil
	}
	for _, rs := range s.alerts.Status() {
		resp.Rules = append(resp.Rules, &sorv1.RuleStatus{Rule: toRule(rs.Rule), Firing: rs.Firing})
	}
	return resp, nil
}

func (s *Server) Watch(_ *sorv1.WatchRequest, stream grpc.ServerStreamingServer[sorv1.WatchResponse]) error {
	if s.feed == nil {
		<-stream.Context().Done()
		return nil
	}
	events, cancel := s.feed.Subscribe()
	defer cancel()
	for {
		select {
		case <-stream.Context().Done():
			return nil
		case ev, ok := <-events:
			if !ok {
				return nil
			}
			if err := stream.Send(toEvent(ev)); err != nil {
				return err
			}
		}
	}
}

func toEvent(ev usecase.Event) *sorv1.WatchResponse {
	out := &sorv1.WatchResponse{Path: ev.Stream.Path().String(), Sample: toSample(ev.Sample)}
	if ev.Alert != nil {
		state := sorv1.AlertState_ALERT_STATE_UNSPECIFIED
		switch ev.Alert.Event {
		case domain.Fired:
			state = sorv1.AlertState_ALERT_STATE_FIRED
		case domain.Resolved:
			state = sorv1.AlertState_ALERT_STATE_RESOLVED
		}
		out.Alert = &sorv1.AlertEvent{Rule: toRule(ev.Alert.Rule), State: state}
	}
	return out
}

func toSample(s domain.Sample) *sorv1.Sample {
	return &sorv1.Sample{Time: timestamppb.New(s.Time), Value: s.Value, Unit: s.Unit}
}

func toRule(r domain.Rule) *sorv1.Rule {
	return &sorv1.Rule{
		Path:      r.Stream.Path().String(),
		Op:        r.Op.String(),
		Threshold: r.Threshold,
		HoldFor:   durationpb.New(r.For),
		Channel:   r.Channel,
	}
}

func toStatus(err error) error {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, domain.ErrBadPath), errors.Is(err, domain.ErrNotDir), errors.Is(err, domain.ErrIsDir):
		return status.Error(codes.InvalidArgument, err.Error())
	}
	return status.Error(codes.Internal, err.Error())
}
