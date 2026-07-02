// ensure.go resolves valid stored credentials or obtains and stores fresh ones for a payer account.
package awsauth

import (
	"context"
	"errors"
	"fmt"

	awsconfig "github.com/openshift-online/finops-tools/cli/internal/aws"
)

// EnsureOptions configures EnsureAccountCredentials.
type EnsureOptions struct {
	AccountName     string
	ProfileNames    []string
	Lookup          awsconfig.CredentialLookup
	Force           bool
	Method          Method
	CredentialsPath string
	Validator       awsconfig.CredentialValidator
	Provider        awsconfig.CredentialProvider
}

// EnsureAccountCredentials resolves stored credentials or obtains and stores fresh ones.
func EnsureAccountCredentials(ctx context.Context, opts EnsureOptions) (awsconfig.Result, error) {
	if opts.AccountName == "" {
		return awsconfig.Result{}, errors.New("account name is required")
	}

	resolve := awsconfig.ResolveOptions{
		AccountName:     opts.AccountName,
		ProfileNames:    opts.ProfileNames,
		CredentialsPath: opts.CredentialsPath,
		Validator:       opts.Validator,
	}

	if !opts.Force {
		res, status, err := awsconfig.ResolveCredentials(ctx, resolve)
		if err != nil {
			return awsconfig.Result{}, err
		}
		if status == awsconfig.CredentialsValid {
			return res, nil
		}
	}

	provider := opts.Provider
	if provider == nil {
		return awsconfig.Result{}, missingCredentialsError(opts.Method, opts.AccountName, opts.ProfileNames, opts.CredentialsPath)
	}

	sess, err := obtainWithProvider(ctx, provider, opts)
	if err != nil {
		return awsconfig.Result{}, err
	}

	return awsconfig.StoreCredentials(ctx, awsconfig.StoreOptions{
		AccountName:     opts.AccountName,
		ProfileNames:    opts.ProfileNames,
		CredentialsPath: opts.CredentialsPath,
		Validator:       opts.Validator,
	}, sess)
}

func obtainWithProvider(ctx context.Context, provider awsconfig.CredentialProvider, opts EnsureOptions) (awsconfig.ProfileSession, error) {
	lookup := opts.Lookup
	if lookup.AccountID == "" {
		lookup.AccountID = opts.AccountName
	}
	if len(lookup.Names) == 0 {
		lookup.Names = []string{opts.AccountName}
	}
	if advanced, ok := provider.(awsconfig.LookupCredentialProvider); ok {
		return advanced.ObtainWithLookup(ctx, lookup)
	}
	return provider.Obtain(ctx, opts.AccountName)
}

func missingCredentialsError(method Method, accountName string, profileNames []string, credentialsPath string) error {
	profiles := profileNames
	if len(profiles) == 0 {
		profiles = []string{awsconfig.SanitizeProfileName(accountName)}
	}
	path := credentialsPath
	if path == "" {
		var err error
		path, err = awsconfig.DefaultCredentialsPath()
		if err != nil {
			return err
		}
	}

	switch method {
	case MethodProfile:
		return fmt.Errorf("%w: profiles %v (configure ~/.aws, or run: finops config account add aws %s --auth-method profile)",
			awsconfig.ErrCredentialsNotFound, profiles, accountName)
	default:
		return fmt.Errorf("%w: profiles %v (run: finops config account add aws %s)",
			awsconfig.ErrCredentialsNotFound, profiles, accountName)
	}
}
