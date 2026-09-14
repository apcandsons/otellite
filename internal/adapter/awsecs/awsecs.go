// Package awsecs reads an ECS service's task counts. Running/desired/
// pending counts are not CloudWatch metrics without Container Insights, so
// they come from DescribeServices. It is the only place the ECS SDK types
// appear.
package awsecs

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecs"

	"github.com/apcandsons/otellite/internal/usecase"
)

// API is the slice of the ECS client the adapter uses.
type API interface {
	DescribeServices(ctx context.Context, in *ecs.DescribeServicesInput, optFns ...func(*ecs.Options)) (*ecs.DescribeServicesOutput, error)
}

// ErrNotFound is returned when the cluster or service does not exist.
var ErrNotFound = errors.New("service not found")

// Client implements usecase.ServiceCounts over the ECS API.
type Client struct{ api API }

// New wraps api.
func New(api API) *Client { return &Client{api: api} }

// Counts describes one service. A service missing from the response (ECS
// reports it under Failures rather than as an error) is ErrNotFound.
func (c *Client) Counts(ctx context.Context, cluster, service string) (usecase.Counts, error) {
	out, err := c.api.DescribeServices(ctx, &ecs.DescribeServicesInput{Cluster: aws.String(cluster), Services: []string{service}})
	if err != nil {
		return usecase.Counts{}, fmt.Errorf("DescribeServices %s/%s: %w", cluster, service, err)
	}
	for _, s := range out.Services {
		if aws.ToString(s.ServiceName) == service {
			return usecase.Counts{Running: int(s.RunningCount), Desired: int(s.DesiredCount), Pending: int(s.PendingCount)}, nil
		}
	}
	reason := "not in response"
	if len(out.Failures) > 0 {
		reason = aws.ToString(out.Failures[0].Reason)
	}
	return usecase.Counts{}, fmt.Errorf("%s/%s (%s): %w", cluster, service, reason, ErrNotFound)
}
