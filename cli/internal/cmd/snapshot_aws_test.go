package cmd

import (
	"context"
	"fmt"
	"testing"

	awsconfig "github.com/openshift-online/finops-tools/cli/internal/aws"
	"github.com/openshift-online/finops-tools/cli/internal/configstore"
	"github.com/openshift-online/finops-tools/core/cost"
	"github.com/spf13/cobra"
)

func TestPrepareSnapshotTargetsSkipsLinkedAssumeRoleFailure(t *testing.T) {
	const (
		payerID  = "123456789012"
		linkedOK = "111111111111"
		linkedNo = "222222222222"
	)

	origAssume := assumeSnapshotLinked
	origPayer := resolveSnapshotPayerSession
	t.Cleanup(func() {
		assumeSnapshotLinked = origAssume
		resolveSnapshotPayerSession = origPayer
	})

	payerSession := awsconfig.ProfileSession{
		AccessKeyID:     "PAYER",
		SecretAccessKey: "SK",
		SessionToken:    "ST",
		Region:          "us-east-1",
	}
	resolveSnapshotPayerSession = func(context.Context, awsconfig.EnsureLinkedOptions) (awsconfig.ProfileSession, error) {
		return payerSession, nil
	}
	assumeSnapshotLinked = func(_ context.Context, opts awsconfig.EnsureLinkedOptions) (awsconfig.ProfileSession, awsconfig.Identity, error) {
		if opts.LinkedAccountID == linkedNo {
			return awsconfig.ProfileSession{}, awsconfig.Identity{}, fmt.Errorf(
				`%s: assume role "arn:aws:iam::%s:role/OrganizationAccountAccessRole": operation error STS: AssumeRole, api error AccessDenied: not authorized`,
				linkedNo, linkedNo,
			)
		}
		return awsconfig.ProfileSession{
			AccessKeyID:     "LINKED",
			SecretAccessKey: "SK",
			SessionToken:    "ST",
			Region:          "us-east-1",
		}, awsconfig.Identity{AccountID: opts.LinkedAccountID}, nil
	}

	cmd := &cobra.Command{}
	targets := []cost.AccountTarget{
		{AccountID: linkedOK, PayerAccountID: payerID},
		{AccountID: linkedNo, PayerAccountID: payerID},
	}

	got, skipped, err := prepareSnapshotTargets(cmd, configstore.Default(), targets, "", "", "", 1, nil)
	if err != nil {
		t.Fatalf("prepareSnapshotTargets: %v", err)
	}
	if len(got) != 1 || got[0].AccountID != linkedOK {
		t.Fatalf("targets = %#v, want one scannable account", got)
	}
	if got[0].ConfigLoader == nil {
		t.Fatal("expected ConfigLoader on linked target")
	}
	if len(skipped) != 1 || skipped[0].AccountID != linkedNo {
		t.Fatalf("skipped = %#v, want one skipped account", skipped)
	}
	if skipped[0].Message == "" {
		t.Fatal("expected skipped message")
	}
}

func TestSnapshotAccountErrorMessage(t *testing.T) {
	err := fmt.Errorf(`222222222222: assume role "arn:aws:iam::222222222222:role/OrganizationAccountAccessRole": AccessDenied`)
	got := snapshotAccountErrorMessage(err)
	if got != `assume role "arn:aws:iam::222222222222:role/OrganizationAccountAccessRole": AccessDenied` {
		t.Fatalf("message = %q", got)
	}
}
