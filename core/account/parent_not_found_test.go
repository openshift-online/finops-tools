package account

import (
	"errors"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/organizations/types"
)

func TestIsParentNotFound(t *testing.T) {
	if IsParentNotFound(nil) || IsParentNotFound(errors.New("AccessDenied")) {
		t.Fatal("expected only ParentNotFoundException to match")
	}
	err := fmt.Errorf("list accounts for parent ou-abcd-12345678: %w", &types.ParentNotFoundException{
		Message: aws.String("missing"),
	})
	if !IsParentNotFound(err) {
		t.Fatal("wrapped ParentNotFoundException was not detected")
	}
}
