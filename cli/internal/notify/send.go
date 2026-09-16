package notify

import (
	"context"
	"fmt"

	"github.com/openshift-online/finops-tools/cli/internal/gmailauth"
)

// GmailSender sends rendered owner notification messages via Gmail.
type GmailSender struct {
	inner *gmailauth.Sender
}

// NewGmailSender authenticates with gcloud and returns a Gmail-backed sender.
func NewGmailSender(ctx context.Context) (*GmailSender, error) {
	inner, err := gmailauth.NewSender(ctx)
	if err != nil {
		return nil, err
	}
	return &GmailSender{inner: inner}, nil
}

// Send delivers one message through Gmail.
// DeliveryTo is preferred over To so --redirect-prefix does not send to the real owner.
func (s *GmailSender) Send(ctx context.Context, msg Message) error {
	to := msg.DeliveryTo
	if to == "" {
		to = msg.To
	}
	if to == "" {
		return fmt.Errorf("recipient is required")
	}
	raw := gmailauth.EncodeMIME(s.inner.FromAddress(), to, msg.Subject, msg.TextBody, msg.HTMLBody)
	return s.inner.SendRaw(ctx, raw)
}
