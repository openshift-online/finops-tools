package snowflake

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"strings"
)

// ParsePrivateKey decodes a PEM-encoded RSA private key (PKCS#8 or PKCS#1).
func ParsePrivateKey(pemText, passphrase string) (*rsa.PrivateKey, error) {
	pemText = strings.TrimSpace(pemText)
	if pemText == "" {
		return nil, fmt.Errorf("private key PEM is empty")
	}

	block, _ := pem.Decode([]byte(pemText))
	if block == nil {
		return nil, fmt.Errorf("private key is not valid PEM")
	}

	keyBytes := block.Bytes
	if x509.IsEncryptedPEMBlock(block) {
		pass := []byte(passphrase)
		if len(pass) == 0 {
			return nil, fmt.Errorf("private key is encrypted; passphrase is required")
		}
		var err error
		keyBytes, err = x509.DecryptPEMBlock(block, pass)
		if err != nil {
			return nil, fmt.Errorf("decrypt private key: %w", err)
		}
	}

	switch block.Type {
	case "PRIVATE KEY":
		key, err := x509.ParsePKCS8PrivateKey(keyBytes)
		if err != nil {
			return nil, fmt.Errorf("parse PKCS#8 private key: %w", err)
		}
		rsaKey, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("private key is not RSA")
		}
		return rsaKey, nil
	case "RSA PRIVATE KEY":
		key, err := x509.ParsePKCS1PrivateKey(keyBytes)
		if err != nil {
			return nil, fmt.Errorf("parse PKCS#1 private key: %w", err)
		}
		return key, nil
	default:
		return nil, fmt.Errorf("unsupported private key type %q", block.Type)
	}
}
