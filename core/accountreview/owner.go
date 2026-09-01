package accountreview

import (
	"errors"
	"fmt"
	"net/mail"
	"strings"

	coreaccount "github.com/openshift-online/finops-tools/core/account"
)

const (
	defaultEmailDomain = "@redhat.com"
	// OwnerTagKey is the Organizations tag key used to derive the owner email.
	OwnerTagKey = "owner"
)

var (
	ErrOwnerTagMissing   = errors.New("owner tag missing")
	ErrOwnerTagEmpty     = errors.New("owner tag empty")
	ErrOwnerEmailInvalid = errors.New("owner email invalid")
)

// ResolveOwnerEmail derives a recipient mailbox from the Organizations owner tag.
//
// A value without '@' is treated as a local part and appended to emailDomain
// (default @redhat.com). RFC 5322 forms such as "Jane Doe <jdoe@redhat.com>"
// are accepted; the returned string is always the parsed mailbox, lowercased,
// so Gmail send and --group-by owner use the same address.
func ResolveOwnerEmail(tags []coreaccount.Tag, emailDomain string) (string, error) {
	domain := strings.TrimSpace(emailDomain)
	if domain == "" {
		domain = defaultEmailDomain
	}
	if !strings.HasPrefix(domain, "@") {
		domain = "@" + domain
	}

	var value string
	found := false
	for _, tag := range tags {
		if strings.EqualFold(strings.TrimSpace(tag.Key), OwnerTagKey) {
			found = true
			value = strings.TrimSpace(tag.Value)
			break
		}
	}
	if !found {
		return "", ErrOwnerTagMissing
	}
	if value == "" {
		return "", ErrOwnerTagEmpty
	}

	email := value
	if !strings.Contains(email, "@") {
		email = value + domain
	}
	addr, err := mail.ParseAddress(email)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrOwnerEmailInvalid, err)
	}
	return strings.ToLower(addr.Address), nil
}

// OwnerErrorStatus maps owner resolution errors to delivery status.
func OwnerErrorStatus(err error) (DeliveryStatus, string) {
	switch {
	case errors.Is(err, ErrOwnerTagMissing):
		return StatusOwnerNotFound, "owner tag not found"
	case errors.Is(err, ErrOwnerTagEmpty):
		return StatusOwnerNotFound, "owner tag is empty"
	case errors.Is(err, ErrOwnerEmailInvalid):
		return StatusInvalidOwner, err.Error()
	default:
		if err != nil {
			return StatusInvalidOwner, err.Error()
		}
		return StatusOwnerNotFound, "owner not found"
	}
}
