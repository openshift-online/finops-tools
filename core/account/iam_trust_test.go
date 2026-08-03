package account

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
)

type fakeIAM struct {
	roleName string
	policy   string
	err      error
}

func (f *fakeIAM) UpdateAssumeRolePolicy(
	_ context.Context,
	params *iam.UpdateAssumeRolePolicyInput,
	_ ...func(*iam.Options),
) (*iam.UpdateAssumeRolePolicyOutput, error) {
	f.roleName = aws.ToString(params.RoleName)
	f.policy = aws.ToString(params.PolicyDocument)
	if f.err != nil {
		return nil, f.err
	}
	return &iam.UpdateAssumeRolePolicyOutput{}, nil
}

func TestUpdateLinkedRoleTrustWithClient(t *testing.T) {
	client := &fakeIAM{}
	if err := updateLinkedRoleTrustWithClient(context.Background(), client, "OrganizationAccountAccessRole", "987654321098"); err != nil {
		t.Fatalf("update: %v", err)
	}
	if client.roleName != "OrganizationAccountAccessRole" {
		t.Fatalf("role = %q", client.roleName)
	}

	var policy managementAccountTrustPolicy
	if err := json.Unmarshal([]byte(client.policy), &policy); err != nil {
		t.Fatalf("unmarshal policy: %v\n%s", err, client.policy)
	}
	if policy.Version != "2012-10-17" || len(policy.Statement) != 1 {
		t.Fatalf("unexpected policy: %+v", policy)
	}
	stmt := policy.Statement[0]
	if stmt.Effect != "Allow" || stmt.Action != "sts:AssumeRole" {
		t.Fatalf("unexpected statement: %+v", stmt)
	}
	wantPrincipal := "arn:aws:iam::987654321098:root"
	if stmt.Principal["AWS"] != wantPrincipal {
		t.Fatalf("principal = %q, want %q", stmt.Principal["AWS"], wantPrincipal)
	}
}

func TestUpdateLinkedRoleTrustRejectsInvalidInputs(t *testing.T) {
	client := &fakeIAM{}
	cases := []struct {
		name   string
		role   string
		mgmtID string
		substr string
	}{
		{name: "empty role", role: "", mgmtID: "987654321098", substr: "role name"},
		{name: "role ARN", role: "arn:aws:iam::111111111111:role/X", mgmtID: "987654321098", substr: "invalid role name"},
		{name: "bad mgmt id", role: "OrganizationAccountAccessRole", mgmtID: "bad", substr: "management account"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := updateLinkedRoleTrustWithClient(context.Background(), client, tc.role, tc.mgmtID)
			if err == nil || !strings.Contains(err.Error(), tc.substr) {
				t.Fatalf("err = %v, want substr %q", err, tc.substr)
			}
		})
	}
}

func TestUpdateLinkedRoleTrustPropagatesIAMError(t *testing.T) {
	client := &fakeIAM{err: errors.New("AccessDenied")}
	err := updateLinkedRoleTrustWithClient(context.Background(), client, "OrganizationAccountAccessRole", "987654321098")
	if err == nil || !strings.Contains(err.Error(), "AccessDenied") {
		t.Fatalf("err = %v", err)
	}
}
