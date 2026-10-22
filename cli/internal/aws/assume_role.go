// assume_role.go obtains temporary credentials by assuming an IAM role with payer session credentials.
package aws

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/openshift-online/finops-tools/core/apilog"
)

// AssumeRole obtains temporary credentials by assuming roleARN using payer credentials.
func AssumeRole(ctx context.Context, payerSession ProfileSession, roleARN, sessionName string) (ProfileSession, error) {
	roleARN = strings.TrimSpace(roleARN)
	if roleARN == "" {
		return ProfileSession{}, fmt.Errorf("role ARN is required")
	}
	if !payerSession.hasAccessKeys() {
		return ProfileSession{}, fmt.Errorf("payer credentials are incomplete")
	}

	region := payerSession.Region
	if region == "" {
		region = "us-east-1"
	}
	if sessionName == "" {
		sessionName = "finops-linked"
	}

	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			payerSession.AccessKeyID,
			payerSession.SecretAccessKey,
			payerSession.SessionToken,
		)),
	)
	if err != nil {
		return ProfileSession{}, fmt.Errorf("load AWS config for assume role: %w", err)
	}
	return NewRoleAssumer(cfg).AssumeRole(ctx, roleARN, sessionName)
}

// RoleAssumer obtains member credentials using a reusable, authenticated STS client.
type RoleAssumer interface {
	AssumeRole(context.Context, string, string) (ProfileSession, error)
}

type roleAssumer struct {
	client *sts.Client
	region string
}

// NewRoleAssumer reuses cfg's credentials provider and STS client across member accounts.
func NewRoleAssumer(cfg aws.Config) RoleAssumer {
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}
	return &roleAssumer{client: sts.NewFromConfig(cfg), region: cfg.Region}
}

func (r *roleAssumer) AssumeRole(ctx context.Context, roleARN, sessionName string) (ProfileSession, error) {
	roleARN = strings.TrimSpace(roleARN)
	if roleARN == "" {
		return ProfileSession{}, fmt.Errorf("role ARN is required")
	}
	if sessionName == "" {
		sessionName = "finops-linked"
	}
	apilog.Log(ctx, fmt.Sprintf("STS.AssumeRole role=%s", roleARN))
	out, err := r.client.AssumeRole(ctx, &sts.AssumeRoleInput{
		RoleArn:         aws.String(roleARN),
		RoleSessionName: aws.String(sessionName),
	})
	if err != nil {
		return ProfileSession{}, fmt.Errorf("assume role %q: %w", roleARN, err)
	}
	if out.Credentials == nil {
		return ProfileSession{}, fmt.Errorf("assume role %q: empty credentials", roleARN)
	}

	sess := ProfileSession{
		AccessKeyID:     aws.ToString(out.Credentials.AccessKeyId),
		SecretAccessKey: aws.ToString(out.Credentials.SecretAccessKey),
		SessionToken:    aws.ToString(out.Credentials.SessionToken),
		Region:          r.region,
	}
	if !sess.complete() {
		return ProfileSession{}, fmt.Errorf("assume role %q: incomplete credentials", roleARN)
	}
	return sess, nil
}
