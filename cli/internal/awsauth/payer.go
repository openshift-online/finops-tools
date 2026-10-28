package awsauth

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/openshift-online/finops-tools/cli/internal/aws"
)

type payerState struct {
	config  aws.Config
	assumer awsconfig.RoleAssumer
}

// PayerSession shares validated payer configuration and coordinates login renewal
// for one command run. Member sessions are still obtained when their scans start.
type PayerSession struct {
	options EnsureOptions
	mu      sync.Mutex
	current *payerState
	pending chan struct{}
	failure error

	load       func(context.Context) (aws.Config, error)
	login      func(context.Context) error
	newAssumer func(aws.Config) awsconfig.RoleAssumer
	validate   awsconfig.CredentialValidator
}

// NewPayerSession creates a lazy, per-run payer session. Profile-mode renewal
// uses its configured SDK provider; it never prompts for keys from scan workers.
func NewPayerSession(options EnsureOptions) *PayerSession {
	if options.Method == MethodProfile {
		options.Provider = nil
	}
	p := &PayerSession{options: options, newAssumer: awsconfig.NewRoleAssumer, validate: awsconfig.STSValidator{}}
	p.load = func(ctx context.Context) (aws.Config, error) {
		return awsconfig.LoadAccountConfig(ctx, awsconfig.ResolveOptions{
			AccountName: options.AccountName, ProfileNames: options.ProfileNames, CredentialsPath: options.CredentialsPath,
		})
	}
	p.login = func(ctx context.Context) error {
		refreshOptions := options
		refreshOptions.Force = true
		result, err := EnsureAccountCredentials(ctx, refreshOptions)
		if err != nil {
			return err
		}
		if result.AccountID != options.AccountName {
			return fmt.Errorf("refreshed session is account %s, expected %s", result.AccountID, options.AccountName)
		}
		return nil
	}
	return p
}

// Config returns the shared payer configuration, validating it once on first use.
func (p *PayerSession) Config(ctx context.Context) (aws.Config, error) {
	state, err := p.state(ctx, nil)
	if err != nil {
		return aws.Config{}, err
	}
	return state.config, nil
}

// LinkedConfig assumes and verifies a member role. An invalid payer credential
// response triggers one coordinated refresh and one retry of AssumeRole.
func (p *PayerSession) LinkedConfig(ctx context.Context, accountID, roleARN string) (aws.Config, error) {
	state, err := p.state(ctx, nil)
	if err != nil {
		return aws.Config{}, err
	}
	sessionName := "finops-" + awsconfig.SanitizeProfileName(accountID)
	session, err := state.assumer.AssumeRole(ctx, roleARN, sessionName)
	if awsconfig.IsCredentialError(err) {
		originalErr := err
		state, err = p.state(ctx, state)
		if err != nil {
			return aws.Config{}, fmt.Errorf("%w; %w (refreshing login for payer %s)", originalErr, err, p.options.AccountName)
		}
		session, err = state.assumer.AssumeRole(ctx, roleARN, sessionName)
		if err != nil {
			credentialFailure := awsconfig.IsCredentialError(err)
			err = fmt.Errorf("%w; retry failed: %w (after refreshing payer %s)", originalErr, err, p.options.AccountName)
			if credentialFailure {
				p.mu.Lock()
				if p.current == state && p.pending == nil && p.failure == nil {
					p.failure = err
				}
				p.mu.Unlock()
			}
		}
	}
	if err != nil {
		return aws.Config{}, err
	}
	// Member identity failures must not trigger a payer login.
	identity, err := p.validate.Validate(ctx, session)
	if err != nil {
		return aws.Config{}, fmt.Errorf("verify linked credentials for %s: %w", accountID, err)
	}
	if identity.AccountID != accountID {
		return aws.Config{}, fmt.Errorf("linked role session is account %s, expected %s", identity.AccountID, accountID)
	}
	return awsconfig.LoadConfigFromSession(ctx, session)
}

// state loads once, or replaces failed only if no other worker already did so.
// Network/login work runs outside the lock so waiting workers can be canceled.
func (p *PayerSession) state(ctx context.Context, failed *payerState) (*payerState, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		p.mu.Lock()
		if p.failure != nil {
			err := p.failure
			p.mu.Unlock()
			return nil, err
		}
		if p.pending != nil {
			pending := p.pending
			p.mu.Unlock()
			select {
			case <-pending:
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		if p.current != nil && (failed == nil || p.current != failed) {
			state := p.current
			p.mu.Unlock()
			return state, nil
		}
		p.pending = make(chan struct{})
		p.mu.Unlock()

		cfg, err := p.loadWithLogin(ctx)
		var state *payerState
		if err == nil {
			state = &payerState{config: cfg, assumer: p.newAssumer(cfg)}
		}
		p.mu.Lock()
		if err == nil {
			p.current = state
			p.failure = nil
		} else if ctx.Err() == nil {
			// Share failures too, so a failed login does not repeat per member.
			p.failure = err
		}
		close(p.pending)
		p.pending = nil
		p.mu.Unlock()
		return state, err
	}
}

func (p *PayerSession) loadWithLogin(ctx context.Context) (aws.Config, error) {
	// Reload first: an SDK provider or another process may already have renewed it.
	cfg, err := p.load(ctx)
	if err == nil || ctx.Err() != nil {
		return cfg, err
	}
	if !awsconfig.IsCredentialError(err) && !errors.Is(err, awsconfig.ErrCredentialsNotFound) {
		return aws.Config{}, err
	}
	if p.options.Method == MethodProfile {
		return aws.Config{}, fmt.Errorf("%w (payer %s profile renewal failed; renew the configured profile before rerunning)", err, p.options.AccountName)
	}
	if loginErr := p.login(ctx); loginErr != nil {
		return aws.Config{}, fmt.Errorf("%w; %w (login refresh failed for payer %s)", err, loginErr, p.options.AccountName)
	}
	cfg, err = p.load(ctx)
	if err != nil {
		return aws.Config{}, fmt.Errorf("%w (loading refreshed credentials for payer %s)", err, p.options.AccountName)
	}
	return cfg, nil
}
