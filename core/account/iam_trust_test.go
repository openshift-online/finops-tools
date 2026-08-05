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

const (
	testSourceManagementAccountID      = "123456789012"
	testDestinationManagementAccountID = "987654321098"
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
	if err := updateLinkedRoleTrustWithClient(context.Background(), client, "OrganizationAccountAccessRole", testSourceManagementAccountID, testDestinationManagementAccountID); err != nil {
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
	if err := updateLinkedRoleTrustWithClient(context.Background(), client, "OrganizationAccountAccessRole", testSourceManagementAccountID, testDestinationManagementAccountID); err != nil {
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
	if err := updateLinkedRoleTrustWithClient(context.Background(), client, "OrganizationAccountAccessRole", testSourceManagementAccountID, testDestinationManagementAccountID); err != nil {
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

func TestUpdateLinkedRoleTrustPreservesOtherAccountRoots(t *testing.T) {
	// Audit/break-glass account roots must not be dropped when rewriting management trust.
	client := &fakeIAM{
		existingPolicy: `{
			"Version":"2012-10-17",
			"Statement":[
				{"Effect":"Allow","Principal":{"AWS":"arn:aws:iam::123456789012:root"},"Action":"sts:AssumeRole"},
				{"Sid":"Audit","Effect":"Allow","Principal":{"AWS":"arn:aws:iam::111111111111:root"},"Action":"sts:AssumeRole"},
				{"Sid":"BreakGlass","Effect":"Allow","Principal":{"AWS":"arn:aws:iam::222222222222:root"},"Action":"sts:AssumeRole"}
			]
		}`,
	}
	if err := updateLinkedRoleTrustWithClient(context.Background(), client, "OrganizationAccountAccessRole", testSourceManagementAccountID, testDestinationManagementAccountID); err != nil {
		t.Fatalf("update: %v", err)
	}

	var policy trustPolicyDocument
	if err := json.Unmarshal([]byte(client.policy), &policy); err != nil {
		t.Fatalf("unmarshal policy: %v\n%s", err, client.policy)
	}
	if len(policy.Statement) != 3 {
		t.Fatalf("want 3 statements, got %d: %s", len(policy.Statement), client.policy)
	}

	var mgmt managementAccountTrustStatement
	if err := json.Unmarshal(policy.Statement[0], &mgmt); err != nil {
		t.Fatalf("unmarshal mgmt statement: %v", err)
	}
	if mgmt.Principal["AWS"] != "arn:aws:iam::987654321098:root" {
		t.Fatalf("mgmt principal = %#v", mgmt.Principal)
	}

	for i, wantSid := range []string{`"Audit"`, `"BreakGlass"`} {
		var other map[string]json.RawMessage
		if err := json.Unmarshal(policy.Statement[i+1], &other); err != nil {
			t.Fatalf("unmarshal statement %d: %v", i+1, err)
		}
		if string(other["Sid"]) != wantSid {
			t.Fatalf("statement %d Sid = %s, want %s", i+1, other["Sid"], wantSid)
		}
	}
	if strings.Contains(client.policy, "arn:aws:iam::123456789012:root") {
		t.Fatalf("source management root still present: %s", client.policy)
	}
}

func TestUpdateLinkedRoleTrustIdempotentWhenAlreadyMigrated(t *testing.T) {
	client := &fakeIAM{
		existingPolicy: `{
			"Version":"2012-10-17",
			"Statement":[
				{"Effect":"Allow","Principal":{"AWS":"arn:aws:iam::987654321098:root"},"Action":"sts:AssumeRole"},
				{"Sid":"Audit","Effect":"Allow","Principal":{"AWS":"arn:aws:iam::111111111111:root"},"Action":"sts:AssumeRole"}
			]
		}`,
	}
	if err := updateLinkedRoleTrustWithClient(context.Background(), client, "OrganizationAccountAccessRole", testSourceManagementAccountID, testDestinationManagementAccountID); err != nil {
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
	if !strings.Contains(client.policy, "arn:aws:iam::111111111111:root") {
		t.Fatalf("audit trust not preserved: %s", client.policy)
	}
}

func TestUpdateLinkedRoleTrustRewritesArrayPrincipal(t *testing.T) {
	// Source + unrelated account in one Principal.AWS array must rewrite source only.
	client := &fakeIAM{
		existingPolicy: `{
			"Version":"2012-10-17",
			"Statement":[
				{
					"Sid":"SharedTrust",
					"Effect":"Allow",
					"Principal":{"AWS":[
						"arn:aws:iam::123456789012:root",
						"arn:aws:iam::111111111111:root"
					]},
					"Action":"sts:AssumeRole"
				}
			]
		}`,
	}
	if err := updateLinkedRoleTrustWithClient(context.Background(), client, "OrganizationAccountAccessRole", testSourceManagementAccountID, testDestinationManagementAccountID); err != nil {
		t.Fatalf("update: %v", err)
	}

	var policy trustPolicyDocument
	if err := json.Unmarshal([]byte(client.policy), &policy); err != nil {
		t.Fatalf("unmarshal policy: %v\n%s", err, client.policy)
	}
	if len(policy.Statement) != 1 {
		t.Fatalf("want 1 statement, got %d: %s", len(policy.Statement), client.policy)
	}

	var stmt map[string]json.RawMessage
	if err := json.Unmarshal(policy.Statement[0], &stmt); err != nil {
		t.Fatalf("unmarshal statement: %v", err)
	}
	if string(stmt["Sid"]) != `"SharedTrust"` {
		t.Fatalf("Sid not preserved: %s", stmt["Sid"])
	}
	var principal map[string]json.RawMessage
	if err := json.Unmarshal(stmt["Principal"], &principal); err != nil {
		t.Fatalf("unmarshal principal: %v", err)
	}
	var awsPrincipals []string
	if err := json.Unmarshal(principal["AWS"], &awsPrincipals); err != nil {
		t.Fatalf("unmarshal AWS principals: %v\n%s", err, principal["AWS"])
	}
	want := []string{
		"arn:aws:iam::987654321098:root",
		"arn:aws:iam::111111111111:root",
	}
	if len(awsPrincipals) != len(want) {
		t.Fatalf("AWS principals = %#v, want %#v", awsPrincipals, want)
	}
	for i := range want {
		if awsPrincipals[i] != want[i] {
			t.Fatalf("AWS principals = %#v, want %#v", awsPrincipals, want)
		}
	}
	if strings.Contains(client.policy, "arn:aws:iam::123456789012:root") {
		t.Fatalf("source management root still present: %s", client.policy)
	}
}

func TestUpdateLinkedRoleTrustDedupesSourceAndDestinationInArray(t *testing.T) {
	client := &fakeIAM{
		existingPolicy: `{
			"Version":"2012-10-17",
			"Statement":[{
				"Effect":"Allow",
				"Principal":{"AWS":[
					"arn:aws:iam::123456789012:root",
					"arn:aws:iam::987654321098:root",
					"arn:aws:iam::222222222222:root"
				]},
				"Action":"sts:AssumeRole"
			}]
		}`,
	}
	if err := updateLinkedRoleTrustWithClient(context.Background(), client, "OrganizationAccountAccessRole", testSourceManagementAccountID, testDestinationManagementAccountID); err != nil {
		t.Fatalf("update: %v", err)
	}

	var policy trustPolicyDocument
	if err := json.Unmarshal([]byte(client.policy), &policy); err != nil {
		t.Fatalf("unmarshal policy: %v\n%s", err, client.policy)
	}
	var stmt map[string]json.RawMessage
	if err := json.Unmarshal(policy.Statement[0], &stmt); err != nil {
		t.Fatalf("unmarshal statement: %v", err)
	}
	var principal map[string]json.RawMessage
	if err := json.Unmarshal(stmt["Principal"], &principal); err != nil {
		t.Fatalf("unmarshal principal: %v", err)
	}
	var awsPrincipals []string
	if err := json.Unmarshal(principal["AWS"], &awsPrincipals); err != nil {
		t.Fatalf("unmarshal AWS principals: %v", err)
	}
	want := []string{
		"arn:aws:iam::987654321098:root",
		"arn:aws:iam::222222222222:root",
	}
	if len(awsPrincipals) != len(want) {
		t.Fatalf("AWS principals = %#v, want %#v", awsPrincipals, want)
	}
	for i := range want {
		if awsPrincipals[i] != want[i] {
			t.Fatalf("AWS principals = %#v, want %#v", awsPrincipals, want)
		}
	}
}

func TestUpdateLinkedRoleTrustRejectsInvalidInputs(t *testing.T) {
	cases := []struct {
		name     string
		role     string
		sourceID string
		destID   string
		substr   string
	}{
		{name: "empty role", role: "", sourceID: testSourceManagementAccountID, destID: testDestinationManagementAccountID, substr: "role name"},
		{name: "role ARN", role: "arn:aws:iam::111111111111:role/X", sourceID: testSourceManagementAccountID, destID: testDestinationManagementAccountID, substr: "invalid role name"},
		{name: "bad source id", role: "OrganizationAccountAccessRole", sourceID: "bad", destID: testDestinationManagementAccountID, substr: "source management account"},
		{name: "bad dest id", role: "OrganizationAccountAccessRole", sourceID: testSourceManagementAccountID, destID: "bad", substr: "destination management account"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &fakeIAM{}
			err := updateLinkedRoleTrustWithClient(context.Background(), c, tc.role, tc.sourceID, tc.destID)
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
	err := updateLinkedRoleTrustWithClient(context.Background(), client, "OrganizationAccountAccessRole", testSourceManagementAccountID, testDestinationManagementAccountID)
	if err == nil || !strings.Contains(err.Error(), "NoSuchEntity") {
		t.Fatalf("err = %v", err)
	}
	if client.updateCalled {
		t.Fatal("UpdateAssumeRolePolicy should not be called after GetRole failure")
	}
}

func TestUpdateLinkedRoleTrustPropagatesIAMError(t *testing.T) {
	client := &fakeIAM{updateErr: errors.New("AccessDenied")}
	err := updateLinkedRoleTrustWithClient(context.Background(), client, "OrganizationAccountAccessRole", testSourceManagementAccountID, testDestinationManagementAccountID)
	if err == nil || !strings.Contains(err.Error(), "AccessDenied") {
		t.Fatalf("err = %v", err)
	}
}
