package aws

import (
	"errors"

	"github.com/aws/smithy-go"
)

// IsCredentialError identifies invalid/expired credentials, excluding permission failures.
func IsCredentialError(err error) bool {
	if errors.Is(err, ErrCredentialsInvalid) {
		return true
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "ExpiredToken", "ExpiredTokenException", "InvalidClientTokenId", "UnrecognizedClientException":
			return true
		}
	}
	return false
}
