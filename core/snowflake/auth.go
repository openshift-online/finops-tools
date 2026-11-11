package snowflake

import (
	"errors"
	"strings"

	"github.com/snowflakedb/gosnowflake/v2"
)

// Snowflake auth/session error numbers that mean the client must re-authenticate.
// 390114 is "Authentication token has expired" (seen on idle JWT sessions).
const (
	errSessionGone             = 390111
	errSessionExpired          = 390112
	errAuthTokenExpired        = 390114
	errInvalidOAuthAccessToken = 390303
	errExpiredOAuthAccessToken = 390318
)

// IsAuthExpired reports whether err indicates an expired Snowflake session or
// authentication token that can be recovered by opening a new connection.
func IsAuthExpired(err error) bool {
	if err == nil {
		return false
	}

	var sfErr *gosnowflake.SnowflakeError
	if errors.As(err, &sfErr) {
		switch sfErr.Number {
		case errSessionGone, errSessionExpired, errAuthTokenExpired,
			errInvalidOAuthAccessToken, errExpiredOAuthAccessToken:
			return true
		}
	}

	msg := err.Error()
	for _, code := range []string{"390111", "390112", "390114", "390303", "390318"} {
		if strings.Contains(msg, code) {
			return true
		}
	}
	return strings.Contains(strings.ToLower(msg), "authentication token has expired")
}
