package awsauth

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/smithy-go"
	awsconfig "github.com/openshift-online/finops-tools/cli/internal/aws"
)

type payerAssumeFunc func(context.Context, string, string) (awsconfig.ProfileSession, error)

func (f payerAssumeFunc) AssumeRole(ctx context.Context, role, name string) (awsconfig.ProfileSession, error) {
	return f(ctx, role, name)
}

func testPayerConfig(key string) aws.Config {
	return aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider(key, "SK", "ST")}
}

func testMemberSession() awsconfig.ProfileSession {
	return awsconfig.ProfileSession{AccessKeyID: "MEMBER", SecretAccessKey: "SK", SessionToken: "ST", Region: "us-east-1"}
}

func testPayer() *PayerSession {
	p := NewPayerSession(EnsureOptions{AccountName: "123456789012", Method: MethodSAML})
	p.validate = fakeValidatorFunc(func(context.Context, awsconfig.ProfileSession) (awsconfig.Identity, error) {
		return awsconfig.Identity{AccountID: "111111111111"}, nil
	})
	return p
}

func runPayerMembers(t *testing.T, p *PayerSession, workers int) []error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = p.LinkedConfig(ctx, "111111111111", "arn:aws:iam::111111111111:role/MemberRole")
		}(i)
	}
	wg.Wait()
	return errs
}

func TestPayerSessionSharesConfigurationAndClient(t *testing.T) {
	p := testPayer()
	var loads, clients, assumptions, validations atomic.Int32
	p.load = func(context.Context) (aws.Config, error) { loads.Add(1); return testPayerConfig("PAYER"), nil }
	p.login = func(context.Context) error { t.Error("unexpected login"); return nil }
	p.newAssumer = func(aws.Config) awsconfig.RoleAssumer {
		clients.Add(1)
		return payerAssumeFunc(func(context.Context, string, string) (awsconfig.ProfileSession, error) {
			assumptions.Add(1)
			return testMemberSession(), nil
		})
	}
	p.validate = fakeValidatorFunc(func(context.Context, awsconfig.ProfileSession) (awsconfig.Identity, error) {
		validations.Add(1)
		return awsconfig.Identity{AccountID: "111111111111"}, nil
	})
	for _, err := range runPayerMembers(t, p, 40) {
		if err != nil {
			t.Fatal(err)
		}
	}
	if loads.Load() != 1 || clients.Load() != 1 || assumptions.Load() != 40 || validations.Load() != 40 {
		t.Fatalf("loads=%d clients=%d assumptions=%d validations=%d", loads.Load(), clients.Load(), assumptions.Load(), validations.Load())
	}
}

func TestPayerSessionCoordinatesConcurrentRefresh(t *testing.T) {
	for _, loginFails := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failed login"}[loginFails], func(t *testing.T) {
			p := testPayer()
			const workers = 20
			var loads, clients, logins, oldAttempts, retries atomic.Int32
			allOldAttempts := make(chan struct{})
			loginFailure := errors.New("Kerberos ticket expired")
			p.load = func(context.Context) (aws.Config, error) {
				switch loads.Add(1) {
				case 1:
					return testPayerConfig("OLD"), nil
				case 2:
					return aws.Config{}, awsconfig.ErrCredentialsInvalid
				default:
					return testPayerConfig("NEW"), nil
				}
			}
			p.login = func(context.Context) error {
				logins.Add(1)
				if loginFails {
					return loginFailure
				}
				return nil
			}
			p.newAssumer = func(cfg aws.Config) awsconfig.RoleAssumer {
				clients.Add(1)
				creds, _ := cfg.Credentials.Retrieve(context.Background())
				return payerAssumeFunc(func(ctx context.Context, _, _ string) (awsconfig.ProfileSession, error) {
					if creds.AccessKeyID == "OLD" {
						if oldAttempts.Add(1) == workers {
							close(allOldAttempts)
						}
						select {
						case <-allOldAttempts:
						case <-ctx.Done():
							return awsconfig.ProfileSession{}, ctx.Err()
						}
						return awsconfig.ProfileSession{}, &smithy.GenericAPIError{Code: "ExpiredToken", Message: "payer session expired"}
					}
					retries.Add(1)
					return testMemberSession(), nil
				})
			}
			for _, err := range runPayerMembers(t, p, workers) {
				if loginFails {
					if !errors.Is(err, loginFailure) || !strings.Contains(err.Error(), "payer 123456789012") {
						t.Fatalf("err=%v", err)
					}
				} else if err != nil {
					t.Fatal(err)
				}
			}
			if logins.Load() != 1 || oldAttempts.Load() != workers {
				t.Fatalf("logins=%d oldAttempts=%d", logins.Load(), oldAttempts.Load())
			}
			if loginFails {
				_, err := p.LinkedConfig(context.Background(), "111111111111", "role")
				if !errors.Is(err, loginFailure) || loads.Load() != 2 || retries.Load() != 0 || clients.Load() != 1 {
					t.Fatalf("failed renewal was retried: err=%v loads=%d retries=%d clients=%d", err, loads.Load(), retries.Load(), clients.Load())
				}
			} else if loads.Load() != 3 || clients.Load() != 2 || retries.Load() != workers {
				t.Fatalf("loads=%d clients=%d retries=%d", loads.Load(), clients.Load(), retries.Load())
			}
		})
	}
}

func TestPayerSessionReloadsExternallyRenewedProfile(t *testing.T) {
	p := testPayer()
	p.options.Method = MethodProfile
	loads, logins := 0, 0
	p.load = func(context.Context) (aws.Config, error) {
		loads++
		if loads == 1 {
			return testPayerConfig("OLD"), nil
		}
		return testPayerConfig("NEW"), nil
	}
	p.login = func(context.Context) error { logins++; return nil }
	p.newAssumer = func(cfg aws.Config) awsconfig.RoleAssumer {
		creds, _ := cfg.Credentials.Retrieve(context.Background())
		return payerAssumeFunc(func(context.Context, string, string) (awsconfig.ProfileSession, error) {
			if creds.AccessKeyID == "OLD" {
				return awsconfig.ProfileSession{}, awsconfig.ErrCredentialsInvalid
			}
			return testMemberSession(), nil
		})
	}
	_, err := p.LinkedConfig(context.Background(), "111111111111", "role")
	if err != nil || loads != 2 || logins != 0 {
		t.Fatalf("err=%v loads=%d logins=%d", err, loads, logins)
	}
}

func TestPayerSessionBoundsRetryWhenRefreshedCredentialsStillFail(t *testing.T) {
	p := testPayer()
	loads, logins, assumptions := 0, 0, 0
	p.load = func(context.Context) (aws.Config, error) {
		loads++
		if loads == 2 {
			return aws.Config{}, awsconfig.ErrCredentialsInvalid
		}
		return testPayerConfig("PAYER"), nil
	}
	p.login = func(context.Context) error { logins++; return nil }
	p.newAssumer = func(aws.Config) awsconfig.RoleAssumer {
		return payerAssumeFunc(func(context.Context, string, string) (awsconfig.ProfileSession, error) {
			assumptions++
			return awsconfig.ProfileSession{}, awsconfig.ErrCredentialsInvalid
		})
	}
	for range 3 {
		if _, err := p.LinkedConfig(context.Background(), "111111111111", "role"); err == nil {
			t.Fatal("expected failure")
		}
	}
	if loads != 3 || logins != 1 || assumptions != 2 {
		t.Fatalf("loads=%d logins=%d assumptions=%d", loads, logins, assumptions)
	}
}

func TestPayerSessionPermissionAndMemberErrorsDoNotRefresh(t *testing.T) {
	for _, stage := range []string{"assume permission", "assume network", "member validation", "member account mismatch"} {
		t.Run(stage, func(t *testing.T) {
			p := testPayer()
			loads, logins := 0, 0
			p.load = func(context.Context) (aws.Config, error) { loads++; return testPayerConfig("PAYER"), nil }
			p.login = func(context.Context) error { logins++; return nil }
			p.newAssumer = func(aws.Config) awsconfig.RoleAssumer {
				return payerAssumeFunc(func(context.Context, string, string) (awsconfig.ProfileSession, error) {
					if stage == "assume permission" {
						return awsconfig.ProfileSession{}, &smithy.GenericAPIError{Code: "AccessDenied", Message: "role denied"}
					}
					if stage == "assume network" {
						return awsconfig.ProfileSession{}, errors.New("connection refused")
					}
					return testMemberSession(), nil
				})
			}
			if stage == "member validation" {
				p.validate = fakeValidatorFunc(func(context.Context, awsconfig.ProfileSession) (awsconfig.Identity, error) {
					return awsconfig.Identity{}, awsconfig.ErrCredentialsInvalid
				})
			}
			if stage == "member account mismatch" {
				p.validate = fakeValidatorFunc(func(context.Context, awsconfig.ProfileSession) (awsconfig.Identity, error) {
					return awsconfig.Identity{AccountID: "999999999999"}, nil
				})
			}
			_, err := p.LinkedConfig(context.Background(), "111111111111", "role")
			if err == nil || loads != 1 || logins != 0 {
				t.Fatalf("err=%v loads=%d logins=%d", err, loads, logins)
			}
		})
	}
}

func TestPayerSessionPermissionErrorAfterRefreshDoesNotPoisonPayer(t *testing.T) {
	p := testPayer()
	loads, logins := 0, 0
	p.load = func(context.Context) (aws.Config, error) {
		loads++
		if loads == 2 {
			return aws.Config{}, awsconfig.ErrCredentialsInvalid
		}
		return testPayerConfig("PAYER"), nil
	}
	p.login = func(context.Context) error { logins++; return nil }
	p.newAssumer = func(aws.Config) awsconfig.RoleAssumer {
		old := loads == 1
		return payerAssumeFunc(func(_ context.Context, role, _ string) (awsconfig.ProfileSession, error) {
			if old {
				return awsconfig.ProfileSession{}, awsconfig.ErrCredentialsInvalid
			}
			if role == "denied" {
				return awsconfig.ProfileSession{}, &smithy.GenericAPIError{Code: "AccessDenied", Message: "role denied"}
			}
			return testMemberSession(), nil
		})
	}
	if _, err := p.LinkedConfig(context.Background(), "111111111111", "denied"); err == nil {
		t.Fatal("expected permission failure")
	}
	if _, err := p.LinkedConfig(context.Background(), "111111111111", "allowed"); err != nil {
		t.Fatal(err)
	}
	if loads != 3 || logins != 1 {
		t.Fatalf("loads=%d logins=%d", loads, logins)
	}
}

func TestPayerSessionProfileModeNeverPromptsFromWorkers(t *testing.T) {
	provider := fakeProvider{err: errors.New("must not prompt")}
	p := NewPayerSession(EnsureOptions{AccountName: "123456789012", Method: MethodProfile, Provider: provider})
	if p.options.Provider != nil {
		t.Fatal("interactive provider retained")
	}
	logins := 0
	p.load = func(context.Context) (aws.Config, error) { return aws.Config{}, awsconfig.ErrCredentialsInvalid }
	p.login = func(context.Context) error { logins++; return nil }
	_, err := p.Config(context.Background())
	if !errors.Is(err, awsconfig.ErrCredentialsInvalid) || !strings.Contains(err.Error(), "renew the configured profile") || logins != 0 {
		t.Fatalf("err=%v logins=%d", err, logins)
	}
}

func TestPayerSessionCanceledWaiterDoesNotBlock(t *testing.T) {
	p := testPayer()
	started, release, finished := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	p.load = func(context.Context) (aws.Config, error) {
		close(started)
		<-release
		return testPayerConfig("PAYER"), nil
	}
	p.newAssumer = func(aws.Config) awsconfig.RoleAssumer {
		return payerAssumeFunc(func(context.Context, string, string) (awsconfig.ProfileSession, error) {
			return testMemberSession(), nil
		})
	}
	go func() { _, err := p.Config(context.Background()); finished <- err }()
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	waiter := make(chan error, 1)
	go func() { _, err := p.Config(ctx); waiter <- err }()
	cancel()
	select {
	case err := <-waiter:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err=%v", err)
		}
	case <-time.After(time.Second):
		t.Error("canceled waiter blocked on another worker")
	}
	close(release)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
}

func TestPayerSessionRunsExistingLoginAndReloadsPayerProfile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials")
	if err := awsconfig.WriteProfile(path, "payer", awsconfig.ProfileSession{AccessKeyID: "OLD", SecretAccessKey: "SK", SessionToken: "ST"}); err != nil {
		t.Fatal(err)
	}
	logins := 0
	p := NewPayerSession(EnsureOptions{AccountName: "123456789012", ProfileNames: []string{"payer"}, CredentialsPath: path, Method: MethodSAML,
		Provider: fakeProviderFunc(func(_ context.Context, account string) (awsconfig.ProfileSession, error) {
			logins++
			if account != "123456789012" {
				t.Errorf("login account=%q", account)
			}
			return awsconfig.ProfileSession{AccessKeyID: "NEW", SecretAccessKey: "SK", SessionToken: "ST"}, nil
		}),
		Validator: fakeValidatorFunc(func(_ context.Context, sess awsconfig.ProfileSession) (awsconfig.Identity, error) {
			if sess.AccessKeyID != "NEW" {
				return awsconfig.Identity{}, awsconfig.ErrCredentialsInvalid
			}
			return awsconfig.Identity{AccountID: "123456789012"}, nil
		}),
	})
	p.load = func(context.Context) (aws.Config, error) {
		sess, ok, err := awsconfig.ReadProfile(path, "payer")
		if err != nil {
			return aws.Config{}, err
		}
		if !ok || sess.AccessKeyID != "NEW" {
			return aws.Config{}, awsconfig.ErrCredentialsInvalid
		}
		return testPayerConfig(sess.AccessKeyID), nil
	}
	if _, err := p.Config(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Config(context.Background()); err != nil {
		t.Fatal(err)
	}
	if logins != 1 {
		t.Fatalf("logins=%d", logins)
	}
	sess, ok, err := awsconfig.ReadProfile(path, "payer")
	if err != nil || !ok || sess.AccessKeyID != "NEW" {
		t.Fatalf("payer profile was not refreshed: err=%v found=%v", err, ok)
	}
}
