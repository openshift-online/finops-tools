// Package gmailauth sends mail via the Gmail API using gcloud Application Default Credentials.
package gmailauth

import (
	"context"
	"errors"
	"fmt"

	"google.golang.org/api/gmail/v1"
)

// ErrNotAuthorized means gcloud Application Default Credentials are missing Gmail scopes.
var ErrNotAuthorized = errors.New("gmail not authorized")

// Sender sends messages through Gmail.
type Sender struct {
	service *gmail.Service
	from    string
}

// NewSender builds a Gmail sender from gcloud Application Default Credentials.
func NewSender(ctx context.Context) (*Sender, error) {
	sender, err := newSenderFromADC(ctx, ResolveQuotaProject())
	if err != nil {
		return nil, fmt.Errorf("%w: %v\n\nfinops does not modify Application Default Credentials.\nSign in with gcloud:\n  %s", ErrNotAuthorized, err, ADCLoginHint())
	}
	return sender, nil
}

// FromAddress returns the authenticated Gmail sender address.
func (s *Sender) FromAddress() string {
	return s.from
}

// SendRaw delivers a base64url-encoded RFC 822 message.
func (s *Sender) SendRaw(ctx context.Context, raw string) error {
	_, err := s.service.Users.Messages.Send("me", &gmail.Message{
		Raw: raw,
	}).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("gmail send: %w", err)
	}
	return nil
}
