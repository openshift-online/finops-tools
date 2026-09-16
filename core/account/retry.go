package account

import (
	"context"
	"errors"
	"time"

	"github.com/aws/smithy-go"
)

const defaultThrottleRetries = 6

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

// retryOnThrottle retries Organizations (and similar) calls that hit AWS throttle
// codes, with exponential backoff. Non-throttle errors and context cancel stop immediately.
func retryOnThrottle(ctx context.Context, attempts int, fn func() error) error {
	if attempts <= 0 {
		attempts = defaultThrottleRetries
	}
	delay := 500 * time.Millisecond
	var err error
	for i := 0; i < attempts; i++ {
		err = fn()
		if err == nil {
			return nil
		}
		if !isThrottleError(err) || i == attempts-1 {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
		if delay < 8*time.Second {
			delay *= 2
		}
	}
	return err
}
