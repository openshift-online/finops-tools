// config.go loads an aws.Config for a named profile from the shared AWS config chain.
package aws

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// LoadSharedConfigProfile loads an AWS API config for a named ~/.aws profile.
func LoadSharedConfigProfile(ctx context.Context, profile string) (aws.Config, error) {
	return config.LoadDefaultConfig(ctx, config.WithSharedConfigProfile(profile))
}

// LoadAccountConfig loads a configured profile and verifies the expected account once.
// Keeping the SDK provider supports shared-config/SSO profiles without rereading raw keys.
func LoadAccountConfig(ctx context.Context, opts ResolveOptions) (aws.Config, error) {
	return loadAccountConfigWith(ctx, opts, loadAccountProfile, func(ctx context.Context, cfg aws.Config) (Identity, error) {
		return callerIdentity(ctx, sts.NewFromConfig(cfg))
	})
}

func loadAccountProfile(ctx context.Context, profile, credentialsPath string) (aws.Config, error) {
	options := []func(*config.LoadOptions) error{config.WithSharedConfigProfile(profile)}
	if credentialsPath != "" {
		options = append(options, config.WithSharedCredentialsFiles([]string{credentialsPath}))
	}
	return config.LoadDefaultConfig(ctx, options...)
}

func loadAccountConfigWith(
	ctx context.Context, opts ResolveOptions,
	load func(context.Context, string, string) (aws.Config, error),
	validate func(context.Context, aws.Config) (Identity, error),
) (aws.Config, error) {
	if opts.AccountName == "" {
		return aws.Config{}, errors.New("account ID is required")
	}
	var failures, fatalFailures []error
	sawInvalid := false
	for _, profile := range profileNamesForAccount(opts.AccountName, opts.ProfileNames) {
		if err := ctx.Err(); err != nil {
			return aws.Config{}, err
		}
		cfg, err := load(ctx, profile, opts.CredentialsPath)
		if err != nil {
			var absent config.SharedConfigProfileNotExistError
			if errors.As(err, &absent) {
				failures = append(failures, fmt.Errorf("load AWS profile %q: %w", profile, err))
			} else {
				fatalFailures = append(fatalFailures, fmt.Errorf("load AWS profile %q: %w", profile, err))
			}
			continue
		}
		if cfg.Region == "" {
			cfg.Region = "us-east-1"
		}
		id, err := validate(ctx, cfg)
		if err != nil {
			failure := fmt.Errorf("validate AWS profile %q: %w", profile, err)
			if IsCredentialError(err) {
				sawInvalid = true
				failures = append(failures, failure)
			} else {
				fatalFailures = append(fatalFailures, failure)
			}
			continue
		}
		if id.AccountID != opts.AccountName {
			fatalFailures = append(fatalFailures, fmt.Errorf("AWS profile %q is account %s, expected %s", profile, id.AccountID, opts.AccountName))
			continue
		}
		return cfg, nil
	}
	if len(fatalFailures) > 0 {
		return aws.Config{}, errors.Join(fatalFailures...)
	}
	if sawInvalid {
		return aws.Config{}, fmt.Errorf("%w: %w", ErrCredentialsInvalid, errors.Join(failures...))
	}
	return aws.Config{}, fmt.Errorf("%w: %w", ErrCredentialsNotFound, errors.Join(failures...))
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
