// Package awscw reads the latest datapoint of many CloudWatch metrics in
// one GetMetricData call. It is the only place the CloudWatch SDK types
// appear.
package awscw

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"

	"github.com/apcandsons/otellite/internal/usecase"
)

// API is the slice of the CloudWatch client the adapter uses.
type API interface {
	GetMetricData(ctx context.Context, in *cloudwatch.GetMetricDataInput, optFns ...func(*cloudwatch.Options)) (*cloudwatch.GetMetricDataOutput, error)
}

const (
	period = 60              // seconds; the finest period for the AWS/* namespaces
	window = 5 * time.Minute // how far back to look for the latest datapoint
)

// Client implements usecase.CloudMetrics over the CloudWatch API.
type Client struct {
	api API
	now func() time.Time
}

// New wraps api. now is injectable for tests; nil means time.Now.
func New(api API, now func() time.Time) *Client {
	if now == nil {
		now = time.Now
	}
	return &Client{api: api, now: now}
}

// Fetch asks for every query in one request (ids are q<i>, mapped back to
// the caller's) and returns the newest value per query; a query with no
// datapoint in the window is absent from the result.
func (c *Client) Fetch(ctx context.Context, queries []usecase.MetricQuery) (map[string]float64, error) {
	out := map[string]float64{}
	if len(queries) == 0 {
		return out, nil
	}
	ids := make(map[string]string, len(queries))
	in := &cloudwatch.GetMetricDataInput{
		StartTime: aws.Time(c.now().Add(-window)),
		EndTime:   aws.Time(c.now()),
		ScanBy:    types.ScanByTimestampDescending,
	}
	for i, q := range queries {
		id := "q" + strconv.Itoa(i)
		ids[id] = q.ID
		dims := make([]types.Dimension, 0, len(q.Dimensions))
		for k, v := range q.Dimensions {
			dims = append(dims, types.Dimension{Name: aws.String(k), Value: aws.String(v)})
		}
		in.MetricDataQueries = append(in.MetricDataQueries, types.MetricDataQuery{
			Id: aws.String(id),
			MetricStat: &types.MetricStat{
				Metric: &types.Metric{Namespace: aws.String(q.Namespace), MetricName: aws.String(q.Metric), Dimensions: dims},
				Period: aws.Int32(period),
				Stat:   aws.String(q.Stat),
			},
		})
	}
	res, err := c.api.GetMetricData(ctx, in)
	if err != nil {
		return nil, fmt.Errorf("GetMetricData: %w", err)
	}
	for _, r := range res.MetricDataResults {
		if len(r.Values) == 0 {
			continue
		}
		if id, ok := ids[aws.ToString(r.Id)]; ok {
			out[id] = r.Values[0] // newest first (ScanByTimestampDescending)
		}
	}
	return out, nil
}
