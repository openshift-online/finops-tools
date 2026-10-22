package apilog

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/health"
)

type healthAPI interface {
	DescribeServiceLifecycle(context.Context, *health.DescribeServiceLifecycleInput, ...func(*health.Options)) (*health.DescribeServiceLifecycleOutput, error)
}

type loggingHealth struct{ inner healthAPI }

// WrapHealth logs lifecycle catalog requests without exposing pagination tokens.
func WrapHealth(inner healthAPI) healthAPI {
	if inner == nil {
		return nil
	}
	return loggingHealth{inner: inner}
}

func (l loggingHealth) DescribeServiceLifecycle(ctx context.Context, in *health.DescribeServiceLifecycleInput, opts ...func(*health.Options)) (*health.DescribeServiceLifecycleOutput, error) {
	message := "Health.DescribeServiceLifecycle"
	if in.Filter != nil {
		message += " service=" + aws.ToString(in.Filter.Service)
	}
	if aws.ToString(in.NextToken) != "" {
		message += " page=next"
	}
	Log(ctx, message)
	return l.inner.DescribeServiceLifecycle(ctx, in, opts...)
}
