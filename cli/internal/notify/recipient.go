package notify

import (
	"fmt"
	"regexp"
	"strings"
)

const defaultRedirectDomain = "redhat.com"

var (
	redirectLocalSanitizer = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)
	redirectPrefixPattern  = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)
)

// ValidateRedirectPrefix reports whether prefix is safe to use in a plus-address local part.
func ValidateRedirectPrefix(prefix string) error {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return fmt.Errorf("redirect prefix is required")
	}
	if !redirectPrefixPattern.MatchString(prefix) {
		return fmt.Errorf("redirect prefix %q is invalid (use letters, digits, dot, underscore, or hyphen)", prefix)
	}
	return nil
}

// RedirectRecipientAddress builds PREFIX+<owner>@redhat.com for test delivery.
func RedirectRecipientAddress(prefix, ownerValue string) (string, error) {
	if err := ValidateRedirectPrefix(prefix); err != nil {
		return "", err
	}
	prefix = strings.TrimSpace(prefix)
	local := ownerLocalPart(ownerValue)
	local = redirectLocalSanitizer.ReplaceAllString(local, "")
	if local == "" {
		return "", fmt.Errorf("owner value %q cannot be used in a redirect recipient address", ownerValue)
	}
	return fmt.Sprintf("%s+%s@%s", prefix, local, defaultRedirectDomain), nil
}

// OwnerLocalPart returns the username portion of an owner tag value or email address.
func OwnerLocalPart(ownerValue string) string {
	return ownerLocalPart(ownerValue)
}

func ownerLocalPart(ownerValue string) string {
	ownerValue = strings.TrimSpace(ownerValue)
	if ownerValue == "" {
		return ""
	}
	if at := strings.Index(ownerValue, "@"); at >= 0 {
		return strings.TrimSpace(ownerValue[:at])
	}
	return ownerValue
}

// ApplyRedirectPrefix rewrites each message to PREFIX+<owner-local>@redhat.com.
// IntendedTo stays the owner address so the delivery summary can show both.
func ApplyRedirectPrefix(messages []Message, prefix string) ([]Message, error) {
	out := make([]Message, len(messages))
	for i, msg := range messages {
		intended := msg.IntendedTo
		if intended == "" {
			intended = msg.To
		}
		delivery, err := RedirectRecipientAddress(prefix, intended)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", intended, err)
		}
		out[i] = msg
		out[i].IntendedTo = intended
		out[i].DeliveryTo = delivery
		out[i].To = delivery
	}
	return out, nil
}

// ApplyOwnerRecipients sets delivery addresses to the resolved owner emails.
func ApplyOwnerRecipients(messages []Message) []Message {
	out := make([]Message, len(messages))
	for i, msg := range messages {
		intended := msg.IntendedTo
		if intended == "" {
			intended = msg.To
		}
		out[i] = msg
		out[i].IntendedTo = intended
		out[i].DeliveryTo = intended
		out[i].To = intended
	}
	return out
}
