// linked.go logs into a linked (member) AWS account via payer credentials and role assumption.
package account

import (
	"context"
	"fmt"
	"strings"

	awsconfig "github.com/openshift-online/finops-tools/cli/internal/aws"
	"github.com/openshift-online/finops-tools/cli/internal/awsauth"
)

// AWSEnsureLinkedFunc performs linked-account credential ensure (injectable for tests).
type AWSEnsureLinkedFunc func(context.Context, awsconfig.EnsureLinkedOptions) (awsconfig.Result, error)

// AddAWSLinkedOptions configures AddAWSLinked.
type AddAWSLinkedOptions struct {
	LinkedAccountID  string
	Alias            string
	PayerAccountID   string
	PayerAlias       string
	RoleARN          string
	EnsurePayer      AWSEnsureFunc
	PayerEnsureOpts  awsauth.EnsureOptions
	EnsureLinked     AWSEnsureLinkedFunc
	EnsureLinkedOpts awsconfig.EnsureLinkedOptions
}

// AddAWSLinked ensures payer credentials, assumes a role into the linked account, and verifies the session.
func AddAWSLinked(ctx context.Context, opts AddAWSLinkedOptions) (AddResult, error) {
	linkedID := strings.TrimSpace(opts.LinkedAccountID)
	payerID := strings.TrimSpace(opts.PayerAccountID)

	if err := ValidateAWSAccountID(linkedID); err != nil {
		return AddResult{}, err
	}
	if err := ValidateAWSAccountID(payerID); err != nil {
		return AddResult{}, fmt.Errorf("payer account: %w", err)
	}

	ensurePayer := opts.EnsurePayer
	if ensurePayer == nil {
		ensurePayer = awsauth.EnsureAccountCredentials
	}
	payerEnsure := opts.PayerEnsureOpts
	payerEnsure.AccountName = payerID
	payerEnsure.Lookup = awsconfig.CredentialLookup{
		AccountID: payerID,
		Names:     awsProfileNames(payerID, opts.PayerAlias, nil),
	}
	payerEnsure.ProfileNames = awsProfileNames(payerID, opts.PayerAlias, payerEnsure.ProfileNames)
	if _, err := ensurePayer(ctx, payerEnsure); err != nil {
		return AddResult{}, fmt.Errorf("ensure payer credentials: %w", err)
	}

	ensure := opts.EnsureLinked
	if ensure == nil {
		ensure = awsconfig.EnsureLinkedCredentials
	}

	ensureOpts := opts.EnsureLinkedOpts
	ensureOpts.PayerAccountID = payerID
	ensureOpts.LinkedAccountID = linkedID
	ensureOpts.RoleARN = opts.RoleARN
	ensureOpts.PayerProfileNames = awsProfileNames(payerID, opts.PayerAlias, ensureOpts.PayerProfileNames)
	ensureOpts.LinkedProfileNames = awsProfileNames(linkedID, opts.Alias, ensureOpts.LinkedProfileNames)

	res, err := ensure(ctx, ensureOpts)
	if err != nil {
		return AddResult{}, err
	}

	if res.AccountID != linkedID {
		return AddResult{}, fmt.Errorf("logged in as account %s, expected %s", res.AccountID, linkedID)
	}

	profile := res.Profile
	if profile == "" {
		profile = awsconfig.SanitizeProfileName(linkedID)
	}

	return AddResult{
		Provider:  ProviderAWS,
		AccountID: linkedID,
		ARN:       res.ARN,
		Profile:   profile,
		Refreshed: res.Refreshed,
	}, nil
}
