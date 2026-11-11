package snowflake

import (
	"errors"
	"strings"

	"github.com/snowflakedb/gosnowflake/v2"
)

// Snowflake session error numbers that mean a new connection (same credentials)
// can recover. OAuth access-token codes (390303/390318) are intentionally
// excluded: this codebase loads a static token from config and has no refresh
// path, so reconnecting would close a usable pool and fail again.
const (
	errSessionGone      = 390111
	errSessionExpired   = 390112
	errAuthTokenExpired = 390114
)

// IsAuthExpired reports whether err indicates an expired Snowflake session or
// authentication token that can be recovered by opening a new connection with
// the same configured credentials (e.g. JWT key-pair).
func IsAuthExpired(err error) bool {
	if err == nil {
		return false
	}

	var sfErr *gosnowflake.SnowflakeError
	if errors.As(err, &sfErr) && sfErr.Number != 0 {
		switch sfErr.Number {
		case errSessionGone, errSessionExpired, errAuthTokenExpired:
			return true
		default:
			return false
		}
	}

	msg := err.Error()
	for _, code := range []string{"390111", "390112", "390114"} {
		if strings.Contains(msg, code) {
			return true
		}
	}
	return strings.Contains(strings.ToLower(msg), "authentication token has expired")
}
