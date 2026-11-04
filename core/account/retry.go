package account

import (
	"context"
	"errors"
	"time"

	"github.com/aws/smithy-go"
)

const (
	defaultThrottleRetries         = 6
	defaultTargetNotFoundRetries   = 6
	defaultRetryInitialDelay       = 500 * time.Millisecond
	defaultRetryMaxDelay           = 8 * time.Second
)

func isThrottleError(err error) bool {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	switch apiErr.ErrorCode() {
	case "TooManyRequestsException", "Throttling", "ThrottlingException", "RequestLimitExceeded":
		return true
	default:
		return false
	}
}

func isTargetNotFoundError(err error) bool {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.ErrorCode() == "TargetNotFoundException"
}

// retryOnThrottle retries Organizations (and similar) calls that hit AWS throttle
// codes, with exponential backoff. Non-throttle errors and context cancel stop immediately.
func retryOnThrottle(ctx context.Context, attempts int, fn func() error) error {
	return retryOn(ctx, attempts, defaultThrottleRetries, isThrottleError, fn)
}

// retryOnTargetNotFound retries calls that hit TargetNotFoundException with exponential
// backoff. Useful right after an account joins an organization, when TagResource can race
// with Organizations eventual consistency. Other errors and context cancel stop immediately.
func retryOnTargetNotFound(ctx context.Context, attempts int, fn func() error) error {
	return retryOn(ctx, attempts, defaultTargetNotFoundRetries, isTargetNotFoundError, fn)
}

func retryOn(
	ctx context.Context,
	attempts, defaultAttempts int,
	retryable func(error) bool,
	fn func() error,
) error {
	if attempts <= 0 {
		attempts = defaultAttempts
	}
	delay := defaultRetryInitialDelay
	var err error
	for i := 0; i < attempts; i++ {
		err = fn()
		if err == nil {
			return nil
		}
		if !retryable(err) || i == attempts-1 {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
		if delay < defaultRetryMaxDelay {
			delay *= 2
		}
	}
	return err
}
