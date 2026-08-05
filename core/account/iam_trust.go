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

// UpdateLinkedRoleTrust rewrites roleName's management-account trust from sourceManagementAccountID
// to destinationManagementAccountID. Other trust statements (service principals, other account roots)
// are preserved. Call with member-account credentials (typically still valid from the source-payer
// assume session).
func UpdateLinkedRoleTrust(ctx context.Context, cfg aws.Config, roleName, sourceManagementAccountID, destinationManagementAccountID string) error {
	return updateLinkedRoleTrustWithClient(ctx, newIAMClient(cfg), roleName, sourceManagementAccountID, destinationManagementAccountID)
}

func updateLinkedRoleTrustWithClient(ctx context.Context, client IAMAPI, roleName, sourceManagementAccountID, destinationManagementAccountID string) error {
	roleName = strings.TrimSpace(roleName)
	sourceManagementAccountID = strings.TrimSpace(sourceManagementAccountID)
	destinationManagementAccountID = strings.TrimSpace(destinationManagementAccountID)
	if roleName == "" {
		return fmt.Errorf("role name is required")
	}
	if strings.Contains(roleName, "/") || strings.Contains(roleName, ":") || strings.HasPrefix(roleName, "arn:") {
		return fmt.Errorf("invalid role name %q (pass the role name only)", roleName)
	}
	if err := validateAccountID(sourceManagementAccountID); err != nil {
		return fmt.Errorf("source management account: %w", err)
	}
	if err := validateAccountID(destinationManagementAccountID); err != nil {
		return fmt.Errorf("destination management account: %w", err)
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

	mgmtStmt, err := marshalManagementAccountTrustPolicy(destinationManagementAccountID)
	if err != nil {
		return err
	}
	merged, err := replaceManagementAccountTrustStatement(policy.Statement, sourceManagementAccountID, destinationManagementAccountID, mgmtStmt)
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
		return fmt.Errorf("update trust policy on role %s for management account %s: %w", roleName, destinationManagementAccountID, err)
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

func managementAccountRootARN(accountID string) string {
	return fmt.Sprintf("arn:aws:iam::%s:root", accountID)
}

func replaceManagementAccountTrustStatement(
	stmts []json.RawMessage,
	sourceManagementAccountID, destinationManagementAccountID string,
	replacement json.RawMessage,
) ([]json.RawMessage, error) {
	sourceRoot := managementAccountRootARN(sourceManagementAccountID)
	destinationRoot := managementAccountRootARN(destinationManagementAccountID)

	out := make([]json.RawMessage, 0, len(stmts)+1)
	touched := false
	for _, raw := range stmts {
		rewritten, found, err := rewriteManagementPrincipalInStatement(raw, sourceRoot, destinationRoot)
		if err != nil {
			return nil, err
		}
		if found {
			touched = true
		}
		out = append(out, rewritten)
	}
	if !touched {
		out = append(out, replacement)
	}
	return out, nil
}

// parseAWSPrincipals returns Principal.AWS as a list, plus whether the original value was an array.
func parseAWSPrincipals(awsPrincipalRaw json.RawMessage) (principals []string, isArray bool, ok bool) {
	var single string
	if err := json.Unmarshal(awsPrincipalRaw, &single); err == nil {
		return []string{single}, false, true
	}
	var multiple []string
	if err := json.Unmarshal(awsPrincipalRaw, &multiple); err == nil {
		return multiple, true, true
	}
	return nil, false, false
}

// rewriteManagementPrincipalInStatement swaps sourceRoot for destinationRoot inside Principal.AWS
// (string or array form). Other principals and statement fields are preserved. found is true when
// either management root was present (so callers know not to append a duplicate trust statement).
func rewriteManagementPrincipalInStatement(raw json.RawMessage, sourceRoot, destinationRoot string) (json.RawMessage, bool, error) {
	var stmt map[string]json.RawMessage
	if err := json.Unmarshal(raw, &stmt); err != nil {
		return nil, false, err
	}
	principalRaw, ok := stmt["Principal"]
	if !ok || len(principalRaw) == 0 {
		return raw, false, nil
	}
	var principal map[string]json.RawMessage
	if err := json.Unmarshal(principalRaw, &principal); err != nil {
		return raw, false, nil
	}
	awsPrincipalRaw, ok := principal["AWS"]
	if !ok || len(awsPrincipalRaw) == 0 {
		return raw, false, nil
	}

	principals, isArray, ok := parseAWSPrincipals(awsPrincipalRaw)
	if !ok {
		return raw, false, nil
	}

	found := false
	rewrote := false
	next := make([]string, 0, len(principals))
	seen := make(map[string]struct{}, len(principals))
	for _, p := range principals {
		p = strings.TrimSpace(p)
		switch p {
		case "":
			continue
		case sourceRoot:
			found = true
			rewrote = true
			p = destinationRoot
		case destinationRoot:
			found = true
		}
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		next = append(next, p)
	}
	if !found || !rewrote {
		return raw, found, nil
	}
	if len(next) == 0 {
		return nil, false, fmt.Errorf("trust statement principal became empty after rewriting management account")
	}

	var newAWS json.RawMessage
	var err error
	if isArray {
		newAWS, err = json.Marshal(next)
	} else {
		newAWS, err = json.Marshal(next[0])
	}
	if err != nil {
		return nil, false, err
	}
	principal["AWS"] = newAWS
	principalBytes, err := json.Marshal(principal)
	if err != nil {
		return nil, false, err
	}
	stmt["Principal"] = principalBytes
	out, err := json.Marshal(stmt)
	if err != nil {
		return nil, false, err
	}
	return out, true, nil
}
