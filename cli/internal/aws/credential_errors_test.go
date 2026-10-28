package aws

import (
	"errors"
	"fmt"
	"testing"

	"github.com/aws/smithy-go"
)

func TestIsCredentialError(t *testing.T) {
	for _, tc := range []struct {
		code string
		want bool
	}{
		{"ExpiredToken", true}, {"ExpiredTokenException", true}, {"InvalidClientTokenId", true}, {"UnrecognizedClientException", true},
		{"AccessDenied", false}, {"ThrottlingException", false}, {"RequestExpired", false},
	} {
		err := fmt.Errorf("assume member role: %w", &smithy.OperationError{ServiceID: "STS", OperationName: "AssumeRole", Err: &smithy.GenericAPIError{Code: tc.code}})
		if got := IsCredentialError(err); got != tc.want {
			t.Errorf("%s: got %v want %v", tc.code, got, tc.want)
		}
	}
	if !IsCredentialError(fmt.Errorf("payer: %w", ErrCredentialsInvalid)) {
		t.Fatal("sentinel not recognized")
	}
	if IsCredentialError(nil) || IsCredentialError(errors.New("ExpiredToken in unrelated text")) {
		t.Fatal("classified an unrelated error")
	}
}
