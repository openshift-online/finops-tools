// Package account implements cloud payer and linked-account login flows used by the CLI.
package account

import (
	"context"
	"errors"
	"fmt"
	"strings"

	awsconfig "github.com/openshift-online/finops-tools/cli/internal/aws"
	"github.com/openshift-online/finops-tools/cli/internal/awsauth"
)

var (
	errProviderNotImplemented = errors.New("account provider not implemented")
	errUseSpecializedMethod   = errors.New("use specialized method for this provider")
)

// AWSEnsureFunc performs AWS credential ensure (injectable for tests).
type AWSEnsureFunc func(context.Context, awsauth.EnsureOptions) (awsconfig.Result, error)

// AddAWSOptions configures AddAWS.
type AddAWSOptions struct {
	AccountID     string
	Alias         string
	Ensure        AWSEnsureFunc
	EnsureOptions awsauth.EnsureOptions
}

// AddResult is returned after a successful AWS account login.
type AddResult struct {
	Provider  Provider
	AccountID string
	ARN       string
	Profile   string
	Refreshed bool
}

// AddAWS logs into an AWS payer account and verifies the session account ID.
func AddAWS(ctx context.Context, opts AddAWSOptions) (AddResult, error) {
	accountID := strings.TrimSpace(opts.AccountID)
	if err := ValidateAWSAccountID(accountID); err != nil {
		return AddResult{}, err
	}

	ensure := opts.Ensure
	if ensure == nil {
		ensure = awsauth.EnsureAccountCredentials
	}

	ensureOpts := opts.EnsureOptions
	ensureOpts.AccountName = accountID
	ensureOpts.Lookup = awsconfig.CredentialLookup{
		AccountID: accountID,
		Names:     awsProfileNames(accountID, opts.Alias, nil),
	}
	ensureOpts.ProfileNames = awsProfileNames(accountID, opts.Alias, ensureOpts.ProfileNames)

	res, err := ensure(ctx, ensureOpts)
	if err != nil {
		return AddResult{}, err
	}

	if res.AccountID != accountID {
		return AddResult{}, fmt.Errorf("logged in as account %s, expected %s", res.AccountID, accountID)
	}

	profile := res.Profile
	if profile == "" {
		profile = awsconfig.SanitizeProfileName(accountID)
	}

	return AddResult{
		Provider:  ProviderAWS,
		AccountID: accountID,
		ARN:       res.ARN,
		Profile:   profile,
		Refreshed: res.Refreshed,
	}, nil
}

// AddOptions configures Add for any provider.
type AddOptions struct {
	Provider      Provider
	AccountID     string
	Alias         string
	AWSEnsure     AWSEnsureFunc
	AWSEnsureOpts awsauth.EnsureOptions
}

// Add logs into a cloud payer account.
func Add(ctx context.Context, opts AddOptions) (AddResult, error) {
	switch opts.Provider {
	case ProviderAWS:
		return AddAWS(ctx, AddAWSOptions{
			AccountID:     opts.AccountID,
			Alias:         opts.Alias,
			Ensure:        opts.AWSEnsure,
			EnsureOptions: opts.AWSEnsureOpts,
		})
	case ProviderGCP:
		return AddResult{}, fmt.Errorf("%w: gcp", errProviderNotImplemented)
	case ProviderSnowflake:
		return AddResult{}, fmt.Errorf("%w: use AddSnowflake for snowflake provider", errUseSpecializedMethod)
	default:
		return AddResult{}, fmt.Errorf("unknown provider %q", opts.Provider)
	}
}
