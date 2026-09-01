package gmailauth

import (
	"encoding/base64"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"strings"
)

// EncodeMIME builds a multipart/alternative RFC 822 message and returns Gmail raw encoding.
// From, To, and Subject have CR/LF stripped so an Organizations account name cannot
// inject extra headers. Bodies are quoted-printable so UTF-8 (account names, OU paths)
// matches the declared charset.
func EncodeMIME(from, to, subject, textBody, htmlBody string) string {
	from = rfc822HeaderValue(from)
	to = rfc822HeaderValue(to)
	subject = rfc822HeaderValue(subject)
	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("From: %s\r\n", from))
	msg.WriteString(fmt.Sprintf("To: %s\r\n", to))
	msg.WriteString(fmt.Sprintf("Subject: %s\r\n", mime.QEncoding.Encode("utf-8", subject)))
	msg.WriteString("MIME-Version: 1.0\r\n")

	boundary := "finops-mail-boundary"
	msg.WriteString(fmt.Sprintf("Content-Type: multipart/alternative; boundary=%q\r\n\r\n", boundary))

	writePart := func(contentType, body string) {
		msg.WriteString("--" + boundary + "\r\n")
		msg.WriteString("Content-Type: " + contentType + "\r\n")
		msg.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
		msg.WriteString(encodeQuotedPrintable(body))
		msg.WriteString("\r\n")
	}

	writePart("text/plain; charset=UTF-8", textBody)
	writePart("text/html; charset=UTF-8", htmlBody)
	msg.WriteString("--" + boundary + "--\r\n")

	return base64.URLEncoding.EncodeToString([]byte(msg.String()))
}

func encodeQuotedPrintable(body string) string {
	var buf strings.Builder
	w := quotedprintable.NewWriter(&buf)
	_, _ = w.Write([]byte(body))
	_ = w.Close()
	return buf.String()
}

// rfc822HeaderValue strips CR/LF so header fields cannot be used for injection.
func rfc822HeaderValue(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' {
			return -1
		}
		return r
	}, s)
}
