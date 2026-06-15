package snowflake

import (
	"fmt"
	"regexp"
	"strings"
)

// unquotedIdent matches one Snowflake unquoted identifier segment (ASCII letters,
// digits, underscore, dollar sign; must not start with a digit).
var unquotedIdent = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_$]*$`)

// ValidateQualifiedIdentifier checks that name is a safe Snowflake object reference:
// 1 to maxParts dot-separated unquoted identifiers. Object names cannot be bound as
// query parameters, so callers must validate before interpolating into SQL text.
func ValidateQualifiedIdentifier(name string, maxParts int) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("identifier is required")
	}
	if maxParts < 1 {
		maxParts = 1
	}
	parts := strings.Split(name, ".")
	if len(parts) > maxParts {
		return fmt.Errorf("identifier %q has %d parts, maximum is %d", name, len(parts), maxParts)
	}
	for _, part := range parts {
		if part == "" {
			return fmt.Errorf("identifier %q has empty segment", name)
		}
		if !unquotedIdent.MatchString(part) {
			return fmt.Errorf("identifier %q contains invalid segment %q", name, part)
		}
	}
	return nil
}
