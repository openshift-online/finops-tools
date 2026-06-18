package snapshot

import (
	"context"
	"errors"
	"net"
	"strings"
)

// RegionWarning records a region that could not be scanned.
type RegionWarning struct {
	AccountID string `json:"account_id"`
	Region    string `json:"region"`
	Message   string `json:"message"`
}

func isSkippableRegionError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, sub := range []string{
		"i/o timeout",
		"connection refused",
		"connection reset",
		"no such host",
		"network is unreachable",
		"request send failed",
		"exceeded maximum number of attempts",
		"statuscode: 0",
		"could not connect to the endpoint",
		"not available in this region",
		"invalidparametervalue",
	} {
		if strings.Contains(msg, sub) {
			return true
		}
	}
	return false
}

func regionErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if idx := strings.LastIndex(msg, ": "); idx >= 0 {
		tail := strings.TrimSpace(msg[idx+2:])
		if tail != "" {
			return tail
		}
	}
	return msg
}
