package account

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/openshift-online/finops-tools/core/apilog"
)

// IAMAPI is the subset of IAM used for linked-role trust updates.
type IAMAPI interface {
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

func (c iamClient) UpdateAssumeRolePolicy(
	ctx context.Context,
	params *iam.UpdateAssumeRolePolicyInput,
	optFns ...func(*iam.Options),
) (*iam.UpdateAssumeRolePolicyOutput, error) {
	apilog.Log(ctx, fmt.Sprintf("IAM.UpdateAssumeRolePolicy role=%s", aws.ToString(params.RoleName)))
	return c.client.UpdateAssumeRolePolicy(ctx, params, optFns...)
}

// managementAccountTrustPolicy is the standard Organizations-style trust document
// allowing the management account root to assume the linked role.
type managementAccountTrustPolicy struct {
	Version   string                            `json:"Version"`
	Statement []managementAccountTrustStatement `json:"Statement"`
}

type managementAccountTrustStatement struct {
	Effect    string            `json:"Effect"`
	Principal map[string]string `json:"Principal"`
	Action    string            `json:"Action"`
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

	doc, err := marshalManagementAccountTrustPolicy(managementAccountID)
	if err != nil {
		return err
	}
	_, err = client.UpdateAssumeRolePolicy(ctx, &iam.UpdateAssumeRolePolicyInput{
		RoleName:       aws.String(roleName),
		PolicyDocument: aws.String(doc),
	})
	if err != nil {
		return fmt.Errorf("update trust policy on role %s for management account %s: %w", roleName, managementAccountID, err)
	}
	return nil
}

func marshalManagementAccountTrustPolicy(managementAccountID string) (string, error) {
	policy := managementAccountTrustPolicy{
		Version: "2012-10-17",
		Statement: []managementAccountTrustStatement{{
			Effect: "Allow",
			Principal: map[string]string{
				"AWS": fmt.Sprintf("arn:aws:iam::%s:root", managementAccountID),
			},
			Action: "sts:AssumeRole",
		}},
	}
	raw, err := json.Marshal(policy)
	if err != nil {
		return "", fmt.Errorf("marshal trust policy: %w", err)
	}
	return string(raw), nil
}
