package notify

import (
	"testing"
)

func TestRedirectRecipientAddress(t *testing.T) {
	got, err := RedirectRecipientAddress("finops", "jdoe")
	if err != nil {
		t.Fatalf("RedirectRecipientAddress() error = %v", err)
	}
	if got != "finops+jdoe@redhat.com" {
		t.Fatalf("got %q", got)
	}
}

func TestRedirectRecipientAddressFromEmail(t *testing.T) {
	got, err := RedirectRecipientAddress("finops", "joe@redhat.com")
	if err != nil {
		t.Fatalf("RedirectRecipientAddress() error = %v", err)
	}
	if got != "finops+joe@redhat.com" {
		t.Fatalf("got %q", got)
	}
}

func TestApplyRedirectPrefix(t *testing.T) {
	messages := []Message{{
		To:         "jdoe@redhat.com",
		IntendedTo: "jdoe@redhat.com",
		Subject:    "test",
	}}
	out, err := ApplyRedirectPrefix(messages, "finops")
	if err != nil {
		t.Fatalf("ApplyRedirectPrefix() error = %v", err)
	}
	if out[0].DeliveryTo != "finops+jdoe@redhat.com" {
		t.Fatalf("DeliveryTo = %q", out[0].DeliveryTo)
	}
	if out[0].To != "finops+jdoe@redhat.com" {
		t.Fatalf("To = %q", out[0].To)
	}
}

func TestApplyOwnerRecipients(t *testing.T) {
	messages := []Message{{
		To:         "jdoe@redhat.com",
		IntendedTo: "jdoe@redhat.com",
		Subject:    "test",
	}}
	out := ApplyOwnerRecipients(messages)
	if out[0].DeliveryTo != "jdoe@redhat.com" {
		t.Fatalf("DeliveryTo = %q", out[0].DeliveryTo)
	}
}

func TestRedirectRecipientAddressRejectsCRLF(t *testing.T) {
	_, err := RedirectRecipientAddress("finops\r\nBcc:evil", "jdoe")
	if err == nil {
		t.Fatal("expected error")
	}
}
