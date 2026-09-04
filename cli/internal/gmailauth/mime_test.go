package gmailauth

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestEncodeMIME(t *testing.T) {
	raw := EncodeMIME("sender@redhat.com", "finops+jdoe@redhat.com", "Subject line", "plain", "<p>html</p>")
	if raw == "" {
		t.Fatal("empty raw message")
	}
}

func TestEncodeMIMEStripsCRLFFromAddresses(t *testing.T) {
	raw := EncodeMIME("sender@redhat.com", "to@redhat.com\r\nBcc: evil@example.com", "hi", "plain", "<p>x</p>")
	decoded, err := base64.URLEncoding.DecodeString(raw)
	if err != nil {
		t.Fatal(err)
	}
	body := string(decoded)
	if strings.Contains(body, "\nBcc:") || strings.Contains(body, "\rBcc:") {
		t.Fatalf("injected header survived: %s", body)
	}
}

func TestEncodeMIMEStripsCRLFFromSubject(t *testing.T) {
	raw := EncodeMIME("sender@redhat.com", "to@redhat.com", "Review\r\nBcc: evil@example.com", "plain", "<p>x</p>")
	decoded, err := base64.URLEncoding.DecodeString(raw)
	if err != nil {
		t.Fatal(err)
	}
	body := string(decoded)
	if strings.Contains(body, "\nBcc:") || strings.Contains(body, "\rBcc:") {
		t.Fatalf("injected header survived: %s", body)
	}
}

func TestEncodeMIMEQuotedPrintableUTF8(t *testing.T) {
	raw := EncodeMIME("sender@redhat.com", "to@redhat.com", "Review", "plain café", "<p>café</p>")
	decoded, err := base64.URLEncoding.DecodeString(raw)
	if err != nil {
		t.Fatal(err)
	}
	body := string(decoded)
	if strings.Contains(body, "Content-Transfer-Encoding: 7bit") {
		t.Fatalf("still using 7bit encoding: %s", body)
	}
	if !strings.Contains(body, "Content-Transfer-Encoding: quoted-printable") {
		t.Fatalf("missing quoted-printable: %s", body)
	}
	if !strings.Contains(body, "caf=C3=A9") {
		t.Fatalf("UTF-8 not quoted-printable encoded: %s", body)
	}
}
