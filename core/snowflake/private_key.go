package snowflake

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"strings"
)

// ParsePrivateKey decodes an unencrypted PEM-encoded RSA private key (PKCS#8 or PKCS#1).
//
// Encrypted PEM (RFC 1423 / legacy Proc-Type headers) is not supported: decrypt the key
// externally (for example with openssl pkcs8 -in key.pem -out key_unencrypted.pem) before
// loading it. This avoids deprecated crypto/x509 PEM decryption APIs and weak legacy ciphers.
func ParsePrivateKey(pemText string) (*rsa.PrivateKey, error) {
	pemText = strings.TrimSpace(pemText)
	if pemText == "" {
		return nil, fmt.Errorf("private key PEM is empty")
	}

	block, _ := pem.Decode([]byte(pemText))
	if block == nil {
		return nil, fmt.Errorf("private key is not valid PEM")
	}

	if isLegacyEncryptedPEMBlock(block) || block.Type == "ENCRYPTED PRIVATE KEY" {
		return nil, fmt.Errorf("encrypted private keys are not supported; decrypt the PEM before use")
	}

	keyBytes := block.Bytes

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

// isLegacyEncryptedPEMBlock reports RFC 1423 Proc-Type encrypted PEM without using the
// deprecated x509.IsEncryptedPEMBlock helper.
func isLegacyEncryptedPEMBlock(block *pem.Block) bool {
	procType, ok := block.Headers["Proc-Type"]
	return ok && strings.HasPrefix(procType, "4,")
}
