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

// AccountWarning records an account that could not be prepared for scanning.
type AccountWarning struct {
	AccountID    string `json:"account_id"`
	DisplayAlias string `json:"display_alias,omitempty"`
	Message      string `json:"message"`
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

// collapseRegionWarnings merges identical per-region failures into one row when every region failed.
func collapseRegionWarnings(accountID string, regions []string, warnings []RegionWarning) []RegionWarning {
	if len(regions) <= 1 || len(warnings) != len(regions) {
		return warnings
	}
	msg := warnings[0].Message
	for _, warning := range warnings[1:] {
		if warning.Message != msg {
			return warnings
		}
	}
	return []RegionWarning{{
		AccountID: accountID,
		Region:    "all",
		Message:   msg,
	}}
}
