// linked_ensure.go ensures payer credentials then assumes a role into a linked (member) account.
package aws

import (
	"context"
	"fmt"
)

// AssumeRoleFunc obtains linked-account credentials from a payer session (injectable for tests).
type AssumeRoleFunc func(ctx context.Context, payerSession ProfileSession, roleARN, sessionName string) (ProfileSession, error)

// EnsureLinkedOptions configures EnsureLinkedCredentials.
type EnsureLinkedOptions struct {
	PayerAccountID     string
	PayerProfileNames  []string
	LinkedAccountID    string
	LinkedProfileNames []string
	RoleARN            string
	CredentialsPath    string
	// PayerSession skips payer credential resolution when already loaded.
	PayerSession ProfileSession
	Validator    CredentialValidator
	AssumeRoleFn AssumeRoleFunc
}

// AssumeLinkedCredentials assumes a role into the linked account using payer credentials
// already stored under the payer profile and validates the linked account ID.
// It does not write ~/.aws/credentials.
func AssumeLinkedCredentials(ctx context.Context, opts EnsureLinkedOptions) (ProfileSession, Identity, error) {
	if opts.PayerAccountID == "" {
		return ProfileSession{}, Identity{}, fmt.Errorf("payer account ID is required")
	}
	if opts.LinkedAccountID == "" {
		return ProfileSession{}, Identity{}, fmt.Errorf("linked account ID is required")
	}
	if opts.RoleARN == "" {
		return ProfileSession{}, Identity{}, fmt.Errorf("role ARN is required")
	}

	validator := opts.Validator
	if validator == nil {
		validator = STSValidator{}
	}

	payerSess, err := resolvePayerProfileSession(ctx, opts)
	if err != nil {
		return ProfileSession{}, Identity{}, err
	}

	assume := opts.AssumeRoleFn
	if assume == nil {
		assume = AssumeRole
	}
	linkedSess, err := assume(ctx, payerSess, opts.RoleARN, "finops-"+SanitizeProfileName(opts.LinkedAccountID))
	if err != nil {
		return ProfileSession{}, Identity{}, err
	}

	id, err := validator.Validate(ctx, linkedSess)
	if err != nil {
		return ProfileSession{}, Identity{}, fmt.Errorf("verify linked credentials: %w", err)
	}
	if id.AccountID != opts.LinkedAccountID {
		return ProfileSession{}, Identity{}, fmt.Errorf("linked role session is account %s, expected %s", id.AccountID, opts.LinkedAccountID)
	}

	return linkedSess, id, nil
}

// ResolvePayerProfileSession loads validated payer credentials from disk or returns PayerSession when set.
func ResolvePayerProfileSession(ctx context.Context, opts EnsureLinkedOptions) (ProfileSession, error) {
	return resolvePayerProfileSession(ctx, opts)
}

func resolvePayerProfileSession(ctx context.Context, opts EnsureLinkedOptions) (ProfileSession, error) {
	if opts.PayerSession.complete() {
		return opts.PayerSession, nil
	}
	if opts.PayerAccountID == "" {
		return ProfileSession{}, fmt.Errorf("payer account ID is required")
	}

	path := opts.CredentialsPath
	if path == "" {
		var err error
		path, err = DefaultCredentialsPath()
		if err != nil {
			return ProfileSession{}, err
		}
	}

	payerProfiles := profileNamesForAccount(opts.PayerAccountID, opts.PayerProfileNames)
	validator := opts.Validator
	if validator == nil {
		validator = STSValidator{}
	}

	payerRes, status, err := resolveFirstValidProfile(ctx, payerProfiles, path, validator)
	if err != nil {
		return ProfileSession{}, err
	}
	if status != CredentialsValid {
		return ProfileSession{}, fmt.Errorf("payer credentials: %w", errPayerCredentialsUnavailable(status, payerProfiles))
	}
	if payerRes.AccountID != opts.PayerAccountID {
		return ProfileSession{}, fmt.Errorf("payer session is account %s, expected %s", payerRes.AccountID, opts.PayerAccountID)
	}

	payerProfile := payerRes.Profile
	if payerProfile == "" {
		payerProfile = SanitizeProfileName(opts.PayerAccountID)
	}
	payerSess, ok, err := ReadProfile(path, payerProfile)
	if err != nil {
		return ProfileSession{}, err
	}
	if !ok {
		return ProfileSession{}, fmt.Errorf("%w: payer profile %q", ErrCredentialsNotFound, payerProfile)
	}
	return payerSess, nil
}

// EnsureLinkedCredentials assumes a role into the linked account and persists linked credentials
// under the first linked profile name. The caller must ensure payer credentials are valid before calling.
func EnsureLinkedCredentials(ctx context.Context, opts EnsureLinkedOptions) (Result, error) {
	linkedSess, id, err := AssumeLinkedCredentials(ctx, opts)
	if err != nil {
		return Result{}, err
	}

	path := opts.CredentialsPath
	if path == "" {
		path, err = DefaultCredentialsPath()
		if err != nil {
			return Result{}, err
		}
	}

	linkedProfiles := opts.LinkedProfileNames
	if len(linkedProfiles) == 0 {
		linkedProfiles = []string{SanitizeProfileName(opts.LinkedAccountID)}
	}
	writeProfile := linkedProfiles[0]

	if err := WriteProfile(path, writeProfile, linkedSess); err != nil {
		return Result{}, fmt.Errorf("write linked credentials profile: %w", err)
	}

	return Result{
		AccountID: id.AccountID,
		ARN:       id.ARN,
		UserID:    id.UserID,
		Profile:   writeProfile,
		Refreshed: true,
	}, nil
}

func errPayerCredentialsUnavailable(status ResolveStatus, profiles []string) error {
	switch status {
	case CredentialsInvalid:
		return fmt.Errorf("%w: profiles %v", ErrCredentialsInvalid, profiles)
	default:
		return fmt.Errorf("%w: profiles %v", ErrCredentialsNotFound, profiles)
	}
}
