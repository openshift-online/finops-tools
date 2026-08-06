package account

import (
	"fmt"
	"regexp"
)

// Single source of truth for AWS Organizations OU and root ID shapes.
// Callers in CLI/configstore must use ValidateOUID / ValidateParentID — do not redefine these patterns.
var (
	ouIDPattern   = regexp.MustCompile(`^ou-[0-9a-z]{4,32}-[0-9a-z]{8,32}$`)
	rootIDPattern = regexp.MustCompile(`^r-[0-9a-z]{4,32}$`)
)

// ValidateOUID reports whether ouID is a well-formed AWS Organizational Unit ID (not a root).
func ValidateOUID(ouID string) error {
	if ouID == "" {
		return fmt.Errorf("OU ID is required")
	}
	if !ouIDPattern.MatchString(ouID) {
		return fmt.Errorf("invalid OU ID %q (expected format ou-xxxx-yyyyy)", ouID)
	}
	return nil
}

// ValidateParentID reports whether parentID is a well-formed OU or organization root ID.
func ValidateParentID(parentID string) error {
	if parentID == "" {
		return fmt.Errorf("parent ID is required")
	}
	if ouIDPattern.MatchString(parentID) || rootIDPattern.MatchString(parentID) {
		return nil
	}
	return fmt.Errorf("invalid parent ID %q (expected ou-xxxx-yyyyy or r-xxxx)", parentID)
}
