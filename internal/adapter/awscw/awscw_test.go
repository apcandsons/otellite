package awscw

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"

	"github.com/apcandsons/otellite/internal/usecase"
)

type fakeCW struct {
	in  *cloudwatch.GetMetricDataInput
	out *cloudwatch.GetMetricDataOutput
	err error
}

func (f *fakeCW) GetMetricData(_ context.Context, in *cloudwatch.GetMetricDataInput, _ ...func(*cloudwatch.Options)) (*cloudwatch.GetMetricDataOutput, error) {
	f.in = in
	return f.out, f.err
}

func TestFetchBuildsOneBatchedRequestAndMapsIdsBack(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	f := &fakeCW{out: &cloudwatch.GetMetricDataOutput{MetricDataResults: []types.MetricDataResult{
		{Id: aws.String("q0"), Timestamps: []time.Time{now.Add(-time.Minute), now.Add(-2 * time.Minute)}, Values: []float64{3, 9}},
		{Id: aws.String("q1")}, // no datapoint
	}}}
	c := New(f, func() time.Time { return now })
	got, err := c.Fetch(context.Background(), []usecase.MetricQuery{
		{ID: "comm/queues/embed_dlq/visible", Namespace: "AWS/SQS", Metric: "ApproximateNumberOfMessagesVisible", Stat: "Maximum", Dimensions: map[string]string{"QueueName": "comm-staging-embed-dlq"}},
		{ID: "comm/comm-bff/ecs/cpu.utilization", Namespace: "AWS/ECS", Metric: "CPUUtilization", Stat: "Average", Dimensions: map[string]string{"ClusterName": "comm-staging", "ServiceName": "comm-bff"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got["comm/queues/embed_dlq/visible"] != 3 {
		t.Fatalf("want the latest value of q0 only, got %v", got)
	}
	in := f.in
	if in.ScanBy != types.ScanByTimestampDescending || !in.EndTime.Equal(now) || !in.StartTime.Equal(now.Add(-5*time.Minute)) {
		t.Errorf("window/scan: %+v", in)
	}
	if len(in.MetricDataQueries) != 2 {
		t.Fatalf("want 2 queries, got %d", len(in.MetricDataQueries))
	}
	q := in.MetricDataQueries[1]
	ms := q.MetricStat
	if aws.ToString(q.Id) != "q1" || aws.ToInt32(ms.Period) != 60 || aws.ToString(ms.Stat) != "Average" || aws.ToString(ms.Metric.Namespace) != "AWS/ECS" || aws.ToString(ms.Metric.MetricName) != "CPUUtilization" {
		t.Errorf("query 1: %+v %+v", q, ms)
	}
	dims := map[string]string{}
	for _, d := range ms.Metric.Dimensions {
		dims[aws.ToString(d.Name)] = aws.ToString(d.Value)
	}
	if dims["ClusterName"] != "comm-staging" || dims["ServiceName"] != "comm-bff" {
		t.Errorf("dimensions: %v", dims)
	}
}

func TestFetchReturnsTheClientError(t *testing.T) {
	c := New(&fakeCW{err: errors.New("throttled")}, time.Now)
	if _, err := c.Fetch(context.Background(), []usecase.MetricQuery{{ID: "x", Namespace: "AWS/SQS", Metric: "m", Stat: "Maximum"}}); err == nil {
		t.Fatal("expected the client error")
	}
}

func TestFetchWithNoQueriesDoesNotCallAWS(t *testing.T) {
	f := &fakeCW{err: errors.New("must not be called")}
	got, err := New(f, time.Now).Fetch(context.Background(), nil)
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v %v", got, err)
	}
}
