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

	apilog.Log(ctx, fmt.Sprintf("STS.AssumeRole role=%s", roleARN))
	out, err := sts.NewFromConfig(cfg).AssumeRole(ctx, &sts.AssumeRoleInput{
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
		Region:          region,
	}
	if !sess.complete() {
		return ProfileSession{}, fmt.Errorf("assume role %q: incomplete credentials", roleARN)
	}
	return sess, nil
}
