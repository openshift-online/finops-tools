package account

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
)

type fakeIAM struct {
	existingPolicy string
	roleName       string
	policy         string
	getErr         error
	updateErr      error
	getCalled      bool
	updateCalled   bool
}

func (f *fakeIAM) GetRole(
	_ context.Context,
	params *iam.GetRoleInput,
	_ ...func(*iam.Options),
) (*iam.GetRoleOutput, error) {
	f.getCalled = true
	f.roleName = aws.ToString(params.RoleName)
	if f.getErr != nil {
		return nil, f.getErr
	}
	doc := f.existingPolicy
	if doc == "" {
		doc = `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"AWS":"arn:aws:iam::123456789012:root"},"Action":"sts:AssumeRole"}]}`
	}
	return &iam.GetRoleOutput{
		Role: &iamtypes.Role{
			RoleName:                 params.RoleName,
			AssumeRolePolicyDocument: aws.String(url.QueryEscape(doc)),
		},
	}, nil
}

func (f *fakeIAM) UpdateAssumeRolePolicy(
	_ context.Context,
	params *iam.UpdateAssumeRolePolicyInput,
	_ ...func(*iam.Options),
) (*iam.UpdateAssumeRolePolicyOutput, error) {
	f.updateCalled = true
	f.roleName = aws.ToString(params.RoleName)
	f.policy = aws.ToString(params.PolicyDocument)
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	return &iam.UpdateAssumeRolePolicyOutput{}, nil
}

func TestUpdateLinkedRoleTrustWithClient(t *testing.T) {
	client := &fakeIAM{}
	if err := updateLinkedRoleTrustWithClient(context.Background(), client, "OrganizationAccountAccessRole", "987654321098"); err != nil {
		t.Fatalf("update: %v", err)
	}
	if !client.getCalled || !client.updateCalled {
		t.Fatalf("getCalled=%v updateCalled=%v", client.getCalled, client.updateCalled)
	}
	if client.roleName != "OrganizationAccountAccessRole" {
		t.Fatalf("role = %q", client.roleName)
	}

	var policy trustPolicyDocument
	if err := json.Unmarshal([]byte(client.policy), &policy); err != nil {
		t.Fatalf("unmarshal policy: %v\n%s", err, client.policy)
	}
	if policy.Version != "2012-10-17" || len(policy.Statement) != 1 {
		t.Fatalf("unexpected policy: %+v", policy)
	}
	var stmt managementAccountTrustStatement
	if err := json.Unmarshal(policy.Statement[0], &stmt); err != nil {
		t.Fatalf("unmarshal statement: %v", err)
	}
	if stmt.Effect != "Allow" || stmt.Action != "sts:AssumeRole" {
		t.Fatalf("unexpected statement: %+v", stmt)
	}
	wantPrincipal := "arn:aws:iam::987654321098:root"
	if stmt.Principal["AWS"] != wantPrincipal {
		t.Fatalf("principal = %q, want %q", stmt.Principal["AWS"], wantPrincipal)
	}
}

func TestUpdateLinkedRoleTrustAcceptsSingleObjectStatement(t *testing.T) {
	// AWS IAM may return Statement as a scalar object when there is only one statement.
	client := &fakeIAM{
		existingPolicy: `{"Version":"2012-10-17","Statement":{"Effect":"Allow","Principal":{"AWS":"arn:aws:iam::123456789012:root"},"Action":"sts:AssumeRole"}}`,
	}
	if err := updateLinkedRoleTrustWithClient(context.Background(), client, "OrganizationAccountAccessRole", "987654321098"); err != nil {
		t.Fatalf("update: %v", err)
	}

	var policy trustPolicyDocument
	if err := json.Unmarshal([]byte(client.policy), &policy); err != nil {
		t.Fatalf("unmarshal policy: %v\n%s", err, client.policy)
	}
	if len(policy.Statement) != 1 {
		t.Fatalf("want 1 statement, got %d: %s", len(policy.Statement), client.policy)
	}
	var stmt managementAccountTrustStatement
	if err := json.Unmarshal(policy.Statement[0], &stmt); err != nil {
		t.Fatalf("unmarshal statement: %v", err)
	}
	if stmt.Principal["AWS"] != "arn:aws:iam::987654321098:root" {
		t.Fatalf("principal = %#v", stmt.Principal)
	}
}

func TestTrustPolicyDocumentUnmarshalStatementForms(t *testing.T) {
	t.Run("array", func(t *testing.T) {
		var policy trustPolicyDocument
		err := json.Unmarshal([]byte(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow"},{"Effect":"Deny"}]}`), &policy)
		if err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(policy.Statement) != 2 {
			t.Fatalf("got %d statements", len(policy.Statement))
		}
	})
	t.Run("single object", func(t *testing.T) {
		var policy trustPolicyDocument
		err := json.Unmarshal([]byte(`{"Version":"2012-10-17","Statement":{"Effect":"Allow","Action":"sts:AssumeRole"}}`), &policy)
		if err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(policy.Statement) != 1 {
			t.Fatalf("got %d statements", len(policy.Statement))
		}
		var stmt map[string]string
		if err := json.Unmarshal(policy.Statement[0], &stmt); err != nil {
			t.Fatalf("unmarshal statement: %v", err)
		}
		if stmt["Effect"] != "Allow" || stmt["Action"] != "sts:AssumeRole" {
			t.Fatalf("unexpected statement: %#v", stmt)
		}
	})
}

func TestUpdateLinkedRoleTrustPreservesOtherStatements(t *testing.T) {
	client := &fakeIAM{
		existingPolicy: `{
			"Version":"2012-10-17",
			"Statement":[
				{"Effect":"Allow","Principal":{"AWS":"arn:aws:iam::123456789012:root"},"Action":"sts:AssumeRole"},
				{"Sid":"EC2","Effect":"Allow","Principal":{"Service":"ec2.amazonaws.com"},"Action":"sts:AssumeRole"}
			]
		}`,
	}
	if err := updateLinkedRoleTrustWithClient(context.Background(), client, "OrganizationAccountAccessRole", "987654321098"); err != nil {
		t.Fatalf("update: %v", err)
	}

	var policy trustPolicyDocument
	if err := json.Unmarshal([]byte(client.policy), &policy); err != nil {
		t.Fatalf("unmarshal policy: %v\n%s", err, client.policy)
	}
	if len(policy.Statement) != 2 {
		t.Fatalf("want 2 statements, got %d: %s", len(policy.Statement), client.policy)
	}

	var mgmt managementAccountTrustStatement
	if err := json.Unmarshal(policy.Statement[0], &mgmt); err != nil {
		t.Fatalf("unmarshal mgmt statement: %v", err)
	}
	if mgmt.Principal["AWS"] != "arn:aws:iam::987654321098:root" {
		t.Fatalf("mgmt principal = %#v", mgmt.Principal)
	}

	var other map[string]json.RawMessage
	if err := json.Unmarshal(policy.Statement[1], &other); err != nil {
		t.Fatalf("unmarshal other statement: %v", err)
	}
	if string(other["Sid"]) != `"EC2"` {
		t.Fatalf("Sid not preserved: %s", other["Sid"])
	}
	if !strings.Contains(string(other["Principal"]), "ec2.amazonaws.com") {
		t.Fatalf("service principal not preserved: %s", other["Principal"])
	}
}

func TestUpdateLinkedRoleTrustRejectsInvalidInputs(t *testing.T) {
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
			c := &fakeIAM{}
			err := updateLinkedRoleTrustWithClient(context.Background(), c, tc.role, tc.mgmtID)
			if err == nil || !strings.Contains(err.Error(), tc.substr) {
				t.Fatalf("err = %v, want substr %q", err, tc.substr)
			}
			if c.updateCalled {
				t.Fatal("UpdateAssumeRolePolicy should not be called for invalid inputs")
			}
		})
	}
}

func TestUpdateLinkedRoleTrustPropagatesGetRoleError(t *testing.T) {
	client := &fakeIAM{getErr: errors.New("NoSuchEntity")}
	err := updateLinkedRoleTrustWithClient(context.Background(), client, "OrganizationAccountAccessRole", "987654321098")
	if err == nil || !strings.Contains(err.Error(), "NoSuchEntity") {
		t.Fatalf("err = %v", err)
	}
	if client.updateCalled {
		t.Fatal("UpdateAssumeRolePolicy should not be called after GetRole failure")
	}
}

func TestUpdateLinkedRoleTrustPropagatesIAMError(t *testing.T) {
	client := &fakeIAM{updateErr: errors.New("AccessDenied")}
	err := updateLinkedRoleTrustWithClient(context.Background(), client, "OrganizationAccountAccessRole", "987654321098")
	if err == nil || !strings.Contains(err.Error(), "AccessDenied") {
		t.Fatalf("err = %v", err)
	}
}
