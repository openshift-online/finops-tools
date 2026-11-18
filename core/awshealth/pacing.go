package awshealth

import (
	"context"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/service/health"
	"github.com/aws/smithy-go"
	"github.com/aws/smithy-go/middleware"
)

// requestPacer has no burst allowance. Waiting requests do not reserve slots, so
// cancellations do not leave reservations delaying subsequent work.
type requestPacer struct {
	mu       sync.Mutex
	interval time.Duration
	next     time.Time
}

func (p *requestPacer) ID() string { return "HealthRequestPacing" }

func (p *requestPacer) HandleFinalize(ctx context.Context, input middleware.FinalizeInput, next middleware.FinalizeHandler) (middleware.FinalizeOutput, middleware.Metadata, error) {
	if err := p.wait(ctx); err != nil {
		return middleware.FinalizeOutput{}, middleware.Metadata{}, err
	}
	out, metadata, err := next.HandleFinalize(ctx, input)
	if err == nil {
		// Organization APIs can return item-level throttles in HTTP 200 responses.
		// Surface these inside the SDK retry loop so adaptive backoff applies too.
		err = responseThrottle(out.Result)
	}
	return out, metadata, err
}

func (p *requestPacer) wait(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		p.mu.Lock()
		now := time.Now()
		delay := p.next.Sub(now)
		if delay <= 0 {
			p.next = now.Add(p.interval)
			p.mu.Unlock()
			return nil
		}
		p.mu.Unlock()
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func responseThrottle(result any) error {
	throttle := func(name, message *string) error {
		err := &smithy.GenericAPIError{Code: aws.ToString(name), Message: aws.ToString(message)}
		if retry.IsErrorThrottles(retry.DefaultThrottles).IsErrorThrottle(err).Bool() {
			return err
		}
		return nil
	}
	switch out := result.(type) {
	case *health.DescribeEventDetailsForOrganizationOutput:
		for _, failure := range out.FailedSet {
			if err := throttle(failure.ErrorName, failure.ErrorMessage); err != nil {
				// The SDK reuses the decode target between attempts. Clear the
				// failed response so an absent failedSet on success is not stale.
				*out = health.DescribeEventDetailsForOrganizationOutput{}
				return err
			}
		}
	case *health.DescribeAffectedEntitiesForOrganizationOutput:
		for _, failure := range out.FailedSet {
			if err := throttle(failure.ErrorName, failure.ErrorMessage); err != nil {
				*out = health.DescribeAffectedEntitiesForOrganizationOutput{}
				return err
			}
		}
	}
	return nil
}
