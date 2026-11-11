package snowflake

import (
	"errors"
	"fmt"
	"testing"

	"github.com/snowflakedb/gosnowflake/v2"
)

func TestIsAuthExpired(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "unrelated", err: errors.New("warehouse suspended"), want: false},
		{
			name: "390114 typed",
			err:  &gosnowflake.SnowflakeError{Number: 390114, Message: "Authentication token has expired"},
			want: true,
		},
		{
			name: "390112 typed",
			err:  &gosnowflake.SnowflakeError{Number: 390112, Message: "Session no longer exists"},
			want: true,
		},
		{
			name: "390114 wrapped string",
			err:  fmt.Errorf("query failed: %w", errors.New("390114: Authentication token has expired. The user must authenticate again.")),
			want: true,
		},
		{
			name: "message only",
			err:  errors.New("Authentication token has expired. The user must authenticate again."),
			want: true,
		},
		{
			name: "unrelated snowflake error",
			err:  &gosnowflake.SnowflakeError{Number: 390201, Message: "Object does not exist"},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := IsAuthExpired(tc.err); got != tc.want {
				t.Fatalf("IsAuthExpired() = %v, want %v", got, tc.want)
			}
		})
	}
}
