// validate.go validates AWS sessions via STS GetCallerIdentity or shared-config profile loading.
package aws

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/smithy-go"
	"github.com/openshift-online/finops-tools/core/apilog"
)

// Identity is the result of a successful STS GetCallerIdentity call.
type Identity struct {
	AccountID string
	ARN       string
	UserID    string
}

// CredentialValidator verifies AWS session credentials.
type CredentialValidator interface {
	Validate(ctx context.Context, sess ProfileSession) (Identity, error)
}

// STSValidator validates credentials via STS GetCallerIdentity.
type STSValidator struct{}

// SharedConfigValidator validates credentials for a named profile via the AWS
// shared config/credentials chain (~/.aws/config, SSO cache, etc.).
type SharedConfigValidator struct {
	Profile string
}

func (v SharedConfigValidator) Validate(ctx context.Context, _ ProfileSession) (Identity, error) {
	if v.Profile == "" {
		return Identity{}, errors.New("profile name is required")
	}
	cfg, err := config.LoadDefaultConfig(ctx, config.WithSharedConfigProfile(v.Profile))
	if err != nil {
		return Identity{}, fmt.Errorf("load AWS profile %q: %w", v.Profile, err)
	}
	return callerIdentity(ctx, sts.NewFromConfig(cfg))
}

func (STSValidator) Validate(ctx context.Context, sess ProfileSession) (Identity, error) {
	if !sess.hasAccessKeys() {
		return Identity{}, errors.New("incomplete credentials")
	}
	region := sess.Region
	if region == "" {
		region = "us-east-1"
	}

	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			sess.AccessKeyID,
			sess.SecretAccessKey,
			sess.SessionToken,
		)),
	)
	if err != nil {
		return Identity{}, fmt.Errorf("load AWS config: %w", err)
	}

	return callerIdentity(ctx, sts.NewFromConfig(cfg))
}

func callerIdentity(ctx context.Context, client *sts.Client) (Identity, error) {
	apilog.Log(ctx, "STS.GetCallerIdentity")
	out, err := client.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) {
			switch apiErr.ErrorCode() {
			case "ExpiredToken", "ExpiredTokenException", "InvalidClientTokenId":
				return Identity{}, fmt.Errorf("%w: %s", ErrCredentialsInvalid, apiErr.ErrorMessage())
			}
		}
		return Identity{}, err
	}

	return Identity{
		AccountID: aws.ToString(out.Account),
		ARN:       aws.ToString(out.Arn),
		UserID:    aws.ToString(out.UserId),
	}, nil
}
