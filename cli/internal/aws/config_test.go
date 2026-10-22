package aws

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/smithy-go"
)

func TestLoadConfigFromSession(t *testing.T) {
	cfg, err := LoadConfigFromSession(context.Background(), ProfileSession{
		AccessKeyID:     "AK",
		SecretAccessKey: "SK",
		SessionToken:    "ST",
		Region:          "eu-west-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Region != "eu-west-1" {
		t.Fatalf("region = %q, want eu-west-1", cfg.Region)
	}
}

func TestLoadAccountConfigUsesProviderAndProfileFallback(t *testing.T) {
	provider := aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
		return aws.Credentials{AccessKeyID: "PROVIDER", SecretAccessKey: "SK"}, nil
	})
	loads, validations := 0, 0
	loaded, err := loadAccountConfigWith(context.Background(), ResolveOptions{AccountName: "123456789012", ProfileNames: []string{"payer", "123456789012"}, CredentialsPath: "custom-credentials"},
		func(_ context.Context, profile, path string) (aws.Config, error) {
			loads++
			if path != "custom-credentials" {
				t.Fatalf("credentials path=%q", path)
			}
			if profile == "payer" {
				return aws.Config{}, config.SharedConfigProfileNotExistError{Profile: profile}
			}
			return aws.Config{Credentials: provider}, nil
		},
		func(_ context.Context, cfg aws.Config) (Identity, error) {
			validations++
			if cfg.Region != "us-east-1" {
				t.Fatalf("region=%q", cfg.Region)
			}
			return Identity{AccountID: "123456789012"}, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	creds, err := loaded.Credentials.Retrieve(context.Background())
	if err != nil || creds.AccessKeyID != "PROVIDER" || loads != 2 || validations != 1 {
		t.Fatalf("err=%v loads=%d validations=%d", err, loads, validations)
	}
}

func TestLoadAccountConfigPreservesCredentialErrorClassification(t *testing.T) {
	for _, tc := range []struct {
		name              string
		loadErr           error
		validationErr     error
		accountID         string
		credentialFailure bool
		missing           bool
	}{
		{name: "missing profile", loadErr: config.SharedConfigProfileNotExistError{Profile: "payer"}, missing: true},
		{name: "expired", validationErr: ErrCredentialsInvalid, credentialFailure: true},
		{name: "permission", validationErr: &smithy.GenericAPIError{Code: "AccessDenied", Message: "not allowed"}},
		{name: "network", validationErr: errors.New("connection refused")},
		{name: "wrong account", accountID: "999999999999"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadAccountConfigWith(context.Background(), ResolveOptions{AccountName: "123456789012", ProfileNames: []string{"payer"}},
				func(context.Context, string, string) (aws.Config, error) { return aws.Config{}, tc.loadErr },
				func(context.Context, aws.Config) (Identity, error) {
					return Identity{AccountID: tc.accountID}, tc.validationErr
				})
			if err == nil || IsCredentialError(err) != tc.credentialFailure || errors.Is(err, ErrCredentialsNotFound) != tc.missing {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestLoadAccountConfigMissingAliasDoesNotHidePermissionFailure(t *testing.T) {
	_, err := loadAccountConfigWith(context.Background(), ResolveOptions{AccountName: "123456789012", ProfileNames: []string{"payer", "123456789012"}},
		func(_ context.Context, profile, _ string) (aws.Config, error) {
			if profile == "payer" {
				return aws.Config{}, config.SharedConfigProfileNotExistError{Profile: profile}
			}
			return aws.Config{}, nil
		},
		func(context.Context, aws.Config) (Identity, error) {
			return Identity{}, &smithy.GenericAPIError{Code: "AccessDenied", Message: "not allowed"}
		})
	if err == nil || errors.Is(err, ErrCredentialsNotFound) || IsCredentialError(err) || !strings.Contains(err.Error(), "AccessDenied") {
		t.Fatalf("err=%v", err)
	}
}

func TestLoadAccountProfileHonorsCredentialsFileOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "custom-credentials")
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(dir, "config"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(dir, "unused-credentials"))
	if err := WriteProfile(path, "payer", ProfileSession{AccessKeyID: "CUSTOM", SecretAccessKey: "SK", SessionToken: "ST"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadAccountProfile(context.Background(), "payer", path)
	if err != nil {
		t.Fatal(err)
	}
	creds, err := cfg.Credentials.Retrieve(context.Background())
	if err != nil || creds.AccessKeyID != "CUSTOM" {
		t.Fatalf("custom credentials were not used: err=%v", err)
	}
}

func TestLoadConfigFromSessionRejectsIncomplete(t *testing.T) {
	_, err := LoadConfigFromSession(context.Background(), ProfileSession{
		AccessKeyID: "AK",
	})
	if err == nil {
		t.Fatal("expected error for incomplete session")
	}
}
