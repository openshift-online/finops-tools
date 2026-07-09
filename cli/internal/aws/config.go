// config.go loads an aws.Config for a named profile from the shared AWS config chain.
package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
)

// LoadSharedConfigProfile loads an AWS API config for a named ~/.aws profile.
func LoadSharedConfigProfile(ctx context.Context, profile string) (aws.Config, error) {
	return config.LoadDefaultConfig(ctx, config.WithSharedConfigProfile(profile))
}

// LoadConfigFromSession builds an AWS API config from in-memory session credentials.
func LoadConfigFromSession(ctx context.Context, sess ProfileSession) (aws.Config, error) {
	if !sess.complete() && !sess.static() {
		return aws.Config{}, fmt.Errorf("incomplete session credentials")
	}
	region := sess.Region
	if region == "" {
		region = "us-east-1"
	}
	return config.LoadDefaultConfig(ctx,
		config.WithRegion(region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			sess.AccessKeyID,
			sess.SecretAccessKey,
			sess.SessionToken,
		)),
	)
}
