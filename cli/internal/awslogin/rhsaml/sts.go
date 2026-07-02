package rhsaml

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	awsconfig "github.com/openshift-online/finops-tools/cli/internal/aws"
	"github.com/openshift-online/finops-tools/core/apilog"
)

const redHatSAMLProviderName = "RedHatInternal"

type assumeRoleWithSAMLAPI interface {
	AssumeRoleWithSAML(ctx context.Context, params *sts.AssumeRoleWithSAMLInput, optFns ...func(*sts.Options)) (*sts.AssumeRoleWithSAMLOutput, error)
}

func assumeRoleWithSAML(ctx context.Context, region string, account awsAccount, assertion string, sessionTimeoutSeconds int32) (awsconfig.ProfileSession, error) {
	client, err := newSTSClient(ctx, region)
	if err != nil {
		return awsconfig.ProfileSession{}, err
	}
	return assumeRoleWithSAMLClient(ctx, client, region, account, assertion, sessionTimeoutSeconds)
}

func assumeRoleWithSAMLClient(ctx context.Context, client assumeRoleWithSAMLAPI, region string, account awsAccount, assertion string, sessionTimeoutSeconds int32) (awsconfig.ProfileSession, error) {
	if strings.TrimSpace(account.UID) == "" || strings.TrimSpace(account.RoleARN) == "" {
		return awsconfig.ProfileSession{}, fmt.Errorf("selected account is missing role metadata")
	}
	input := &sts.AssumeRoleWithSAMLInput{
		RoleArn:         aws.String(account.RoleARN),
		PrincipalArn:    aws.String(fmt.Sprintf("arn:aws:iam::%s:saml-provider/%s", account.UID, redHatSAMLProviderName)),
		SAMLAssertion:   aws.String(assertion),
		DurationSeconds: aws.Int32(sessionTimeoutSeconds),
	}
	apilog.Log(ctx, fmt.Sprintf("STS.AssumeRoleWithSAML role=%s", account.RoleARN))
	out, err := client.AssumeRoleWithSAML(ctx, input)
	if err != nil {
		return awsconfig.ProfileSession{}, fmt.Errorf("assume role with SAML for %s: %w", account.UID, err)
	}
	if out.Credentials == nil {
		return awsconfig.ProfileSession{}, fmt.Errorf("assume role with SAML for %s: empty credentials", account.UID)
	}
	session := awsconfig.ProfileSession{
		AccessKeyID:     aws.ToString(out.Credentials.AccessKeyId),
		SecretAccessKey: aws.ToString(out.Credentials.SecretAccessKey),
		SessionToken:    aws.ToString(out.Credentials.SessionToken),
		Region:          region,
	}
	if session.Region == "" {
		session.Region = defaultRegion
	}
	return session, nil
}

func newSTSClient(ctx context.Context, region string) (assumeRoleWithSAMLAPI, error) {
	if strings.TrimSpace(region) == "" {
		region = defaultRegion
	}
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(region),
		config.WithCredentialsProvider(aws.AnonymousCredentials{}),
	)
	if err != nil {
		return nil, fmt.Errorf("load AWS config for SAML assume-role: %w", err)
	}
	return sts.NewFromConfig(cfg, func(o *sts.Options) {
		o.Credentials = aws.AnonymousCredentials{}
	}), nil
}
