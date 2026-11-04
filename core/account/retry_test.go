package account

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/aws/smithy-go"
)

type throttleAPIError struct {
	code string
}

func (e throttleAPIError) Error() string   { return e.code }
func (e throttleAPIError) ErrorCode() string { return e.code }
func (e throttleAPIError) ErrorMessage() string {
	return e.code
}
func (e throttleAPIError) ErrorFault() smithy.ErrorFault {
	return smithy.FaultUnknown
}

func TestIsThrottleError(t *testing.T) {
	if !isThrottleError(throttleAPIError{code: "TooManyRequestsException"}) {
		t.Fatal("expected throttle")
	}
	if isThrottleError(errors.New("boom")) {
		t.Fatal("expected non-throttle")
	}
}

func TestIsTargetNotFoundError(t *testing.T) {
	if !isTargetNotFoundError(throttleAPIError{code: "TargetNotFoundException"}) {
		t.Fatal("expected target not found")
	}
	if isTargetNotFoundError(throttleAPIError{code: "AccessDeniedException"}) {
		t.Fatal("expected non-retryable")
	}
	if isTargetNotFoundError(errors.New("boom")) {
		t.Fatal("expected non-api error")
	}
}

func TestRetryOnThrottleEventuallySucceeds(t *testing.T) {
	calls := 0
	err := retryOnThrottle(context.Background(), 4, func() error {
		calls++
		if calls < 3 {
			return throttleAPIError{code: "TooManyRequestsException"}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("retryOnThrottle() error = %v", err)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
}

func TestRetryOnThrottleRespectsContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := retryOnThrottle(ctx, 4, func() error {
		return throttleAPIError{code: "TooManyRequestsException"}
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("retryOnThrottle() error = %v", err)
	}
}

func TestRetryOnThrottleNonThrottleFailsFast(t *testing.T) {
	want := fmt.Errorf("permanent")
	calls := 0
	err := retryOnThrottle(context.Background(), 4, func() error {
		calls++
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("retryOnThrottle() error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestRetryOnTargetNotFoundEventuallySucceeds(t *testing.T) {
	calls := 0
	err := retryOnTargetNotFound(context.Background(), 4, func() error {
		calls++
		if calls < 3 {
			return throttleAPIError{code: "TargetNotFoundException"}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("retryOnTargetNotFound() error = %v", err)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
}

func TestRetryOnTargetNotFoundNonRetryableFailsFast(t *testing.T) {
	want := fmt.Errorf("access denied")
	calls := 0
	err := retryOnTargetNotFound(context.Background(), 4, func() error {
		calls++
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("retryOnTargetNotFound() error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestRetryOnTargetNotFoundExhaustsAttempts(t *testing.T) {
	calls := 0
	err := retryOnTargetNotFound(context.Background(), 3, func() error {
		calls++
		return throttleAPIError{code: "TargetNotFoundException"}
	})
	if err == nil {
		t.Fatal("expected error after exhausted retries")
	}
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) || apiErr.ErrorCode() != "TargetNotFoundException" {
		t.Fatalf("error = %v", err)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
}
