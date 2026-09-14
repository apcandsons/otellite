package awsecs

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"

	"github.com/apcandsons/otellite/internal/usecase"
)

type fakeECS struct {
	in  *ecs.DescribeServicesInput
	out *ecs.DescribeServicesOutput
	err error
}

func (f *fakeECS) DescribeServices(_ context.Context, in *ecs.DescribeServicesInput, _ ...func(*ecs.Options)) (*ecs.DescribeServicesOutput, error) {
	f.in = in
	return f.out, f.err
}

func TestCountsReadsTheServiceCounters(t *testing.T) {
	f := &fakeECS{out: &ecs.DescribeServicesOutput{Services: []types.Service{
		{ServiceName: aws.String("comm-bff"), RunningCount: 1, DesiredCount: 2, PendingCount: 1},
	}}}
	got, err := New(f).Counts(context.Background(), "comm-staging", "comm-bff")
	if err != nil {
		t.Fatal(err)
	}
	if got != (usecase.Counts{Running: 1, Desired: 2, Pending: 1}) {
		t.Fatalf("got %+v", got)
	}
	if aws.ToString(f.in.Cluster) != "comm-staging" || len(f.in.Services) != 1 || f.in.Services[0] != "comm-bff" {
		t.Errorf("request: %+v", f.in)
	}
}

func TestCountsReportsAMissingServiceAsNotFound(t *testing.T) {
	f := &fakeECS{out: &ecs.DescribeServicesOutput{Failures: []types.Failure{{Arn: aws.String("arn:...:service/comm-staging/gone"), Reason: aws.String("MISSING")}}}}
	_, err := New(f).Counts(context.Background(), "comm-staging", "gone")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestCountsReturnsTheClientError(t *testing.T) {
	_, err := New(&fakeECS{err: errors.New("denied")}).Counts(context.Background(), "c", "s")
	if err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("want the client error, got %v", err)
	}
}
