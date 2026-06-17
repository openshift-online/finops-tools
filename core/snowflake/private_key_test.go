package snowflake

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"
)

func TestParsePrivateKeyRejectsEncryptedPEM(t *testing.T) {
	t.Parallel()

	legacyEncrypted := pem.EncodeToMemory(&pem.Block{
		Type: "RSA PRIVATE KEY",
		Headers: map[string]string{
			"Proc-Type": "4,ENCRYPTED",
			"DEK-Info":  "DES-EDE3-CBC,AAAAAAAA",
		},
		Bytes: []byte("ciphertext"),
	})
	_, err := ParsePrivateKey(string(legacyEncrypted))
	if err == nil || !strings.Contains(err.Error(), "encrypted private keys are not supported") {
		t.Fatalf("expected encrypted-key error, got %v", err)
	}

	pkcs8Encrypted := pem.EncodeToMemory(&pem.Block{
		Type:  "ENCRYPTED PRIVATE KEY",
		Bytes: []byte("ciphertext"),
	})
	_, err = ParsePrivateKey(string(pkcs8Encrypted))
	if err == nil || !strings.Contains(err.Error(), "encrypted private keys are not supported") {
		t.Fatalf("expected encrypted PKCS#8 error, got %v", err)
	}
}

func TestParsePrivateKeyPKCS8(t *testing.T) {
	t.Parallel()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	pemText := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))

	parsed, err := ParsePrivateKey(pemText)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.D.Cmp(key.D) != 0 {
		t.Fatal("parsed PKCS#8 key does not match source")
	}
}

func TestParsePrivateKeyPKCS1(t *testing.T) {
	t.Parallel()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der := x509.MarshalPKCS1PrivateKey(key)
	pemText := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: der}))

	parsed, err := ParsePrivateKey(pemText)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.D.Cmp(key.D) != 0 {
		t.Fatal("parsed PKCS#1 key does not match source")
	}
}
