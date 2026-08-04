package account

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/openshift-online/finops-tools/core/apilog"
)

// IAMAPI is the subset of IAM used for linked-role trust updates.
type IAMAPI interface {
	GetRole(
		ctx context.Context,
		params *iam.GetRoleInput,
		optFns ...func(*iam.Options),
	) (*iam.GetRoleOutput, error)
	UpdateAssumeRolePolicy(
		ctx context.Context,
		params *iam.UpdateAssumeRolePolicyInput,
		optFns ...func(*iam.Options),
	) (*iam.UpdateAssumeRolePolicyOutput, error)
}

func newIAMClient(cfg aws.Config) IAMAPI {
	return iamClient{client: iam.NewFromConfig(cfg)}
}

type iamClient struct {
	client *iam.Client
}

func (c iamClient) GetRole(
	ctx context.Context,
	params *iam.GetRoleInput,
	optFns ...func(*iam.Options),
) (*iam.GetRoleOutput, error) {
	apilog.Log(ctx, fmt.Sprintf("IAM.GetRole role=%s", aws.ToString(params.RoleName)))
	return c.client.GetRole(ctx, params, optFns...)
}

func (c iamClient) UpdateAssumeRolePolicy(
	ctx context.Context,
	params *iam.UpdateAssumeRolePolicyInput,
	optFns ...func(*iam.Options),
) (*iam.UpdateAssumeRolePolicyOutput, error) {
	apilog.Log(ctx, fmt.Sprintf("IAM.UpdateAssumeRolePolicy role=%s", aws.ToString(params.RoleName)))
	return c.client.UpdateAssumeRolePolicy(ctx, params, optFns...)
}

// trustPolicyDocument is a flexible assume-role policy used for merge updates.
// Statement values are kept as raw JSON so unrelated statements are preserved verbatim.
// AWS IAM accepts Statement as either a single object or an array; UnmarshalJSON normalizes both to a slice.
type trustPolicyDocument struct {
	Version   string            `json:"Version"`
	Statement []json.RawMessage `json:"Statement"`
}

func (p *trustPolicyDocument) UnmarshalJSON(data []byte) error {
	var raw struct {
		Version   string          `json:"Version"`
		Statement json.RawMessage `json:"Statement"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	p.Version = raw.Version
	stmt := bytes.TrimSpace(raw.Statement)
	if len(stmt) == 0 || bytes.Equal(stmt, []byte("null")) {
		p.Statement = nil
		return nil
	}
	switch stmt[0] {
	case '[':
		return json.Unmarshal(stmt, &p.Statement)
	case '{':
		p.Statement = []json.RawMessage{append(json.RawMessage(nil), stmt...)}
		return nil
	default:
		return fmt.Errorf("statement must be a JSON object or array")
	}
}

// managementAccountTrustStatement is the Organizations-style trust statement for a management account.
type managementAccountTrustStatement struct {
	Effect    string            `json:"Effect"`
	Principal map[string]string `json:"Principal"`
	Action    string            `json:"Action"`
}

// principalProbe is used only to inspect an existing statement's AWS principal.
type principalProbe struct {
	Principal map[string]json.RawMessage `json:"Principal"`
}

// UpdateLinkedRoleTrust sets roleName's assume-role trust to the destination management account.
// Call with member-account credentials (typically still valid from the source-payer assume session).
func UpdateLinkedRoleTrust(ctx context.Context, cfg aws.Config, roleName, managementAccountID string) error {
	return updateLinkedRoleTrustWithClient(ctx, newIAMClient(cfg), roleName, managementAccountID)
}

func updateLinkedRoleTrustWithClient(ctx context.Context, client IAMAPI, roleName, managementAccountID string) error {
	roleName = strings.TrimSpace(roleName)
	managementAccountID = strings.TrimSpace(managementAccountID)
	if roleName == "" {
		return fmt.Errorf("role name is required")
	}
	if strings.Contains(roleName, "/") || strings.Contains(roleName, ":") || strings.HasPrefix(roleName, "arn:") {
		return fmt.Errorf("invalid role name %q (pass the role name only)", roleName)
	}
	if err := validateAccountID(managementAccountID); err != nil {
		return fmt.Errorf("management account: %w", err)
	}

	roleOut, err := client.GetRole(ctx, &iam.GetRoleInput{RoleName: aws.String(roleName)})
	if err != nil {
		return fmt.Errorf("get role %s: %w", roleName, err)
	}
	if roleOut.Role == nil || roleOut.Role.AssumeRolePolicyDocument == nil {
		return fmt.Errorf("get role %s: missing assume-role policy document", roleName)
	}

	rawDoc, err := url.QueryUnescape(aws.ToString(roleOut.Role.AssumeRolePolicyDocument))
	if err != nil {
		return fmt.Errorf("decode trust policy on role %s: %w", roleName, err)
	}

	var policy trustPolicyDocument
	if err := json.Unmarshal([]byte(rawDoc), &policy); err != nil {
		return fmt.Errorf("parse trust policy on role %s: %w", roleName, err)
	}
	if policy.Version == "" {
		policy.Version = "2012-10-17"
	}

	mgmtStmt, err := marshalManagementAccountTrustPolicy(managementAccountID)
	if err != nil {
		return err
	}
	merged, err := replaceManagementAccountTrustStatement(policy.Statement, managementAccountID, mgmtStmt)
	if err != nil {
		return fmt.Errorf("merge trust policy on role %s: %w", roleName, err)
	}
	policy.Statement = merged

	doc, err := json.Marshal(policy)
	if err != nil {
		return fmt.Errorf("marshal trust policy on role %s: %w", roleName, err)
	}

	_, err = client.UpdateAssumeRolePolicy(ctx, &iam.UpdateAssumeRolePolicyInput{
		RoleName:       aws.String(roleName),
		PolicyDocument: aws.String(string(doc)),
	})
	if err != nil {
		return fmt.Errorf("update trust policy on role %s for management account %s: %w", roleName, managementAccountID, err)
	}
	return nil
}

// marshalManagementAccountTrustPolicy constructs the Organizations-style trust statement
// for the given management account.
func marshalManagementAccountTrustPolicy(managementAccountID string) (json.RawMessage, error) {
	stmt := managementAccountTrustStatement{
		Effect: "Allow",
		Principal: map[string]string{
			"AWS": fmt.Sprintf("arn:aws:iam::%s:root", managementAccountID),
		},
		Action: "sts:AssumeRole",
	}
	raw, err := json.Marshal(stmt)
	if err != nil {
		return nil, fmt.Errorf("marshal trust policy: %w", err)
	}
	return raw, nil
}

func replaceManagementAccountTrustStatement(
	stmts []json.RawMessage,
	managementAccountID string,
	replacement json.RawMessage,
) ([]json.RawMessage, error) {
	out := make([]json.RawMessage, 0, len(stmts)+1)
	replaced := false
	for _, raw := range stmts {
		matches, err := statementPrincipalReferencesManagementAccount(raw, managementAccountID)
		if err != nil {
			return nil, err
		}
		if !matches {
			out = append(out, raw)
			continue
		}
		if !replaced {
			out = append(out, replacement)
			replaced = true
		}
	}
	if !replaced {
		out = append(out, replacement)
	}
	return out, nil
}

func statementPrincipalReferencesManagementAccount(raw json.RawMessage, managementAccountID string) (bool, error) {
	var probe principalProbe
	if err := json.Unmarshal(raw, &probe); err != nil {
		return false, err
	}
	awsPrincipalRaw, ok := probe.Principal["AWS"]
	if !ok || len(awsPrincipalRaw) == 0 {
		return false, nil
	}

	var awsPrincipal string
	if err := json.Unmarshal(awsPrincipalRaw, &awsPrincipal); err != nil {
		// Principal.AWS may be an array; treat as non-management single-root statement.
		return false, nil
	}
	awsPrincipal = strings.TrimSpace(awsPrincipal)
	if awsPrincipal == "" {
		return false, nil
	}

	want := fmt.Sprintf("arn:aws:iam::%s:root", managementAccountID)
	if awsPrincipal == want {
		return true, nil
	}

	// Migrate path: existing trust still points at the source management account root.
	const (
		prefix     = "arn:aws:iam::"
		rootSuffix = ":root"
	)
	if !strings.HasPrefix(awsPrincipal, prefix) || !strings.HasSuffix(awsPrincipal, rootSuffix) {
		return false, nil
	}
	accountID := strings.TrimSuffix(strings.TrimPrefix(awsPrincipal, prefix), rootSuffix)
	return validateAccountID(accountID) == nil, nil
}
