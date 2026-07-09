// linked_ensure_test.go tests EnsureLinkedCredentials end-to-end with mocks.
package aws

import (
	"context"
	"path/filepath"
	"testing"
)

type fakeValidatorFunc func(ctx context.Context, sess ProfileSession) (Identity, error)

func (f fakeValidatorFunc) Validate(ctx context.Context, sess ProfileSession) (Identity, error) {
	return f(ctx, sess)
}

func TestEnsureLinkedCredentials(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials")
	payerID := "123456789012"
	linkedID := "111111111111"
	payerProfile := SanitizeProfileName(payerID)
	if err := WriteProfile(path, payerProfile, ProfileSession{
		AccessKeyID: "PAYER", SecretAccessKey: "SK", SessionToken: "ST", Region: "us-east-1",
	}); err != nil {
		t.Fatal(err)
	}

	validator := fakeValidatorFunc(func(_ context.Context, sess ProfileSession) (Identity, error) {
		switch sess.AccessKeyID {
		case "PAYER":
			return Identity{AccountID: payerID, ARN: "arn:payer", UserID: "u"}, nil
		case "LINKED":
			return Identity{AccountID: linkedID, ARN: "arn:linked", UserID: "u"}, nil
		default:
			return Identity{}, ErrCredentialsInvalid
		}
	})

	res, err := EnsureLinkedCredentials(context.Background(), EnsureLinkedOptions{
		PayerAccountID:  payerID,
		LinkedAccountID: linkedID,
		RoleARN:         "arn:aws:iam::111111111111:role/FinOpsReadOnly",
		CredentialsPath: path,
		Validator:       validator,
		AssumeRoleFn: func(_ context.Context, _ ProfileSession, roleARN, _ string) (ProfileSession, error) {
			if roleARN == "" {
				t.Fatal("empty role ARN")
			}
			return ProfileSession{
				AccessKeyID: "LINKED", SecretAccessKey: "SK", SessionToken: "ST", Region: "us-east-1",
			}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Refreshed || res.AccountID != linkedID {
		t.Fatalf("result: %+v", res)
	}

	got, ok, err := ReadProfile(path, SanitizeProfileName(linkedID))
	if err != nil || !ok || got.AccessKeyID != "LINKED" {
		t.Fatalf("linked profile: ok=%v err=%v got=%+v", ok, err, got)
	}
}

func TestEnsureLinkedCredentialsRejectsWrongLinkedAccount(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials")
	payerID := "123456789012"
	if err := WriteProfile(path, SanitizeProfileName(payerID), ProfileSession{
		AccessKeyID: "PAYER", SecretAccessKey: "SK", SessionToken: "ST", Region: "us-east-1",
	}); err != nil {
		t.Fatal(err)
	}

	validator := fakeValidatorFunc(func(_ context.Context, sess ProfileSession) (Identity, error) {
		if sess.AccessKeyID == "PAYER" {
			return Identity{AccountID: payerID}, nil
		}
		return Identity{AccountID: "999999999999"}, nil
	})

	_, err := EnsureLinkedCredentials(context.Background(), EnsureLinkedOptions{
		PayerAccountID:  payerID,
		LinkedAccountID: "111111111111",
		RoleARN:         "arn:aws:iam::111111111111:role/X",
		CredentialsPath: path,
		Validator:       validator,
		AssumeRoleFn: func(context.Context, ProfileSession, string, string) (ProfileSession, error) {
			return ProfileSession{AccessKeyID: "LINKED", SecretAccessKey: "S", SessionToken: "T"}, nil
		},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAssumeLinkedCredentialsDoesNotWriteProfile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials")
	payerID := "123456789012"
	linkedID := "111111111111"
	payerProfile := SanitizeProfileName(payerID)
	if err := WriteProfile(path, payerProfile, ProfileSession{
		AccessKeyID: "PAYER", SecretAccessKey: "SK", SessionToken: "ST", Region: "us-east-1",
	}); err != nil {
		t.Fatal(err)
	}

	validator := fakeValidatorFunc(func(_ context.Context, sess ProfileSession) (Identity, error) {
		switch sess.AccessKeyID {
		case "PAYER":
			return Identity{AccountID: payerID}, nil
		case "LINKED":
			return Identity{AccountID: linkedID}, nil
		default:
			return Identity{}, ErrCredentialsInvalid
		}
	})

	_, id, err := AssumeLinkedCredentials(context.Background(), EnsureLinkedOptions{
		PayerAccountID:  payerID,
		LinkedAccountID: linkedID,
		RoleARN:         "arn:aws:iam::111111111111:role/FinOpsReadOnly",
		CredentialsPath: path,
		Validator:       validator,
		AssumeRoleFn: func(context.Context, ProfileSession, string, string) (ProfileSession, error) {
			return ProfileSession{
				AccessKeyID: "LINKED", SecretAccessKey: "SK", SessionToken: "ST", Region: "us-east-1",
			}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if id.AccountID != linkedID {
		t.Fatalf("identity account = %q", id.AccountID)
	}

	_, ok, err := ReadProfile(path, SanitizeProfileName(linkedID))
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("linked profile should not be written by AssumeLinkedCredentials")
	}
}

func TestEnsureLinkedCredentialsRequiresPayerCredentials(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials")

	_, err := EnsureLinkedCredentials(context.Background(), EnsureLinkedOptions{
		PayerAccountID:  "123456789012",
		LinkedAccountID: "111111111111",
		RoleARN:         "arn:aws:iam::111111111111:role/X",
		CredentialsPath: path,
	})
	if err == nil {
		t.Fatal("expected error")
	}
}
