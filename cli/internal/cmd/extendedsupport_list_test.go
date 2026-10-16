package cmd

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/openshift-online/finops-tools/cli/internal/awsauth"
	"github.com/openshift-online/finops-tools/cli/internal/configstore"
	"github.com/openshift-online/finops-tools/cli/internal/output"
	"github.com/openshift-online/finops-tools/cli/internal/progress"
	"github.com/openshift-online/finops-tools/core/cost"
	"github.com/openshift-online/finops-tools/core/extendedsupport"
	"github.com/openshift-online/finops-tools/core/parallel"
	"github.com/spf13/cobra"
)

func TestExtendedSupportListPreRunRejectsUnknownService(t *testing.T) {
	extendedSupportListAccount = "123456789012"
	extendedSupportListAccountAliases = ""
	extendedSupportListFormat = string(output.FormatPrettyPrint)
	extendedSupportListServices = "eks"
	t.Cleanup(func() {
		extendedSupportListAccount = ""
		extendedSupportListServices = "rds"
	})

	cmd := extendedSupportListCmd
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	if err := cmd.PreRunE(cmd, nil); err == nil {
		t.Fatal("expected error for unknown service")
	}
}

type fakeExtendedSupportPayer struct {
	config func(context.Context) (aws.Config, error)
	linked func(context.Context, string, string) (aws.Config, error)
}

func (p fakeExtendedSupportPayer) Config(ctx context.Context) (aws.Config, error) {
	if p.config != nil {
		return p.config(ctx)
	}
	return aws.Config{}, nil
}

func TestPrepareExtendedSupportTargetsKeepsPayersSeparate(t *testing.T) {
	cfg, path := testLinkedRoleConfig(t)
	cmd := testRoleFlagCmd()
	previous := newExtendedSupportPayerSession
	t.Cleanup(func() { newExtendedSupportPayerSession = previous })
	sessions := make(map[string]int)
	loaded := make(map[string]int)
	newExtendedSupportPayerSession = func(opts awsauth.EnsureOptions) extendedSupportPayerSession {
		sessions[opts.AccountName]++
		return fakeExtendedSupportPayer{
			config: func(context.Context) (aws.Config, error) { loaded[opts.AccountName]++; return aws.Config{}, nil },
			linked: func(context.Context, string, string) (aws.Config, error) {
				loaded[opts.AccountName]++
				return aws.Config{}, nil
			},
		}
	}
	targets, _, err := prepareExtendedSupportTargetsImpl(cmd, cfg, []cost.AccountTarget{
		{AccountID: "111111111111", PayerAccountID: "123456789012", DisplayAlias: "member"},
		{AccountID: "123456789012"},
		{AccountID: "222222222222", PayerAccountID: "987654321098"},
	}, "", path, "", 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range targets {
		if _, err := target.ConfigLoader(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if len(sessions) != 2 || sessions["123456789012"] != 1 || sessions["987654321098"] != 1 || loaded["123456789012"] != 2 || loaded["987654321098"] != 1 {
		t.Fatalf("sessions=%v loaded=%v", sessions, loaded)
	}
}
func (p fakeExtendedSupportPayer) LinkedConfig(ctx context.Context, accountID, role string) (aws.Config, error) {
	return p.linked(ctx, accountID, role)
}

func TestPrepareExtendedSupportTargetsSharesPayerAndDefersLinkedAuthentication(t *testing.T) {
	cfg, path := testLinkedRoleConfig(t)
	cmd := testRoleFlagCmd()
	previous := newExtendedSupportPayerSession
	t.Cleanup(func() { newExtendedSupportPayerSession = previous })
	payerSessions, assumptions := 0, 0
	newExtendedSupportPayerSession = func(opts awsauth.EnsureOptions) extendedSupportPayerSession {
		payerSessions++
		if opts.AccountName != "123456789012" {
			t.Fatalf("payer = %q", opts.AccountName)
		}
		return fakeExtendedSupportPayer{linked: func(_ context.Context, accountID, role string) (aws.Config, error) {
			assumptions++
			if accountID == "222222222222" {
				return aws.Config{}, errors.New("AccessDenied")
			}
			if role != "arn:aws:iam::111111111111:role/OldRole" {
				t.Fatalf("role = %q", role)
			}
			return aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("LINKED", "SK", "ST")}, nil
		}}
	}
	targets, skipped, err := prepareExtendedSupportTargetsImpl(cmd, cfg, []cost.AccountTarget{
		{AccountID: "111111111111", PayerAccountID: "123456789012", DisplayAlias: "member"},
		{AccountID: "222222222222", PayerAccountID: "123456789012"},
	}, "", path, "", 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 2 || len(skipped) != 0 || payerSessions != 1 || assumptions != 0 {
		t.Fatalf("targets=%v skipped=%v payerSessions=%d assumptions=%d", targets, skipped, payerSessions, assumptions)
	}
	awsCfg, err := targets[0].ConfigLoader(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	creds, err := awsCfg.Credentials.Retrieve(context.Background())
	if err != nil || creds.AccessKeyID != "LINKED" || assumptions != 1 {
		t.Fatalf("credentials retrieval failed: err=%v assumptions=%d", err, assumptions)
	}
	result, err := extendedsupport.Fetch(context.Background(), extendedsupport.Query{Targets: targets[1:], Services: []extendedsupport.Service{extendedsupport.ServiceRDS}, Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	if assumptions != 2 || len(result.Summary.SkippedAccounts) != 1 || result.Summary.SkippedAccounts[0].AccountID != "222222222222" {
		t.Fatalf("result=%+v assumptions=%d", result, assumptions)
	}
}

func TestExtendedSupportListPreRunRejectsInvalidWorkers(t *testing.T) {
	extendedSupportListAccount = "123456789012"
	extendedSupportListFormat = string(output.FormatPrettyPrint)
	extendedSupportListServices = "rds"
	extendedSupportListWorkers = 0
	t.Cleanup(func() {
		extendedSupportListAccount = ""
		extendedSupportListWorkers = parallel.DefaultWorkers
	})

	cmd := extendedSupportListCmd
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	if err := cmd.PreRunE(cmd, nil); err == nil {
		t.Fatal("expected error for workers < 1")
	}
}

func TestRunExtendedSupportListUsesFetchHook(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := configstore.RegisterAWSAccount(path, "123456789012", "rh-control"); err != nil {
		t.Fatal(err)
	}

	prevFetch := extendedSupportListFetch
	prevPrepare := prepareExtendedSupportTargets
	prevEnsure := ensureExtendedSupportCredentials
	t.Cleanup(func() {
		extendedSupportListFetch = prevFetch
		prepareExtendedSupportTargets = prevPrepare
		ensureExtendedSupportCredentials = prevEnsure
		awsFlags.ConfigPath = ""
	})

	extendedSupportListFetch = func(_ context.Context, q extendedsupport.Query) (extendedsupport.Result, error) {
		if len(q.Targets) != 1 || q.Targets[0].AccountID != "123456789012" {
			t.Fatalf("targets = %#v", q.Targets)
		}
		if len(q.Services) != 1 || q.Services[0] != extendedsupport.ServiceRDS {
			t.Fatalf("services = %#v", q.Services)
		}
		return extendedsupport.Result{
			Records: []extendedsupport.Record{{
				AccountID:    "123456789012",
				Region:       "us-east-1",
				Service:      extendedsupport.ServiceRDS,
				ResourceType: extendedsupport.ResourceTypeRDSInstance,
				ResourceID:   "db-old",
			}},
			Summary: extendedsupport.Summary{TotalCount: 1},
		}, nil
	}
	ensureExtendedSupportCredentials = func(_ *cobra.Command, _ configstore.File, _ []cost.AccountTarget, _, _, _ string) error {
		return nil
	}
	prepareExtendedSupportTargets = func(_ *cobra.Command, _ configstore.File, targets []cost.AccountTarget, _, _, _ string, _ int, _ *progress.Bar) ([]extendedsupport.AccountTarget, []extendedsupport.AccountWarning, error) {
		out := make([]extendedsupport.AccountTarget, 0, len(targets))
		for _, target := range targets {
			out = append(out, extendedsupport.AccountTarget{AccountID: target.AccountID})
		}
		return out, nil, nil
	}

	awsFlags.ConfigPath = path
	extendedSupportListAccount = ""
	extendedSupportListAccountAliases = "rh-control"
	extendedSupportListFormat = string(output.FormatJSON)
	extendedSupportListQuiet = true
	extendedSupportListServices = "rds"
	t.Cleanup(func() {
		extendedSupportListAccount = ""
		extendedSupportListAccountAliases = ""
		extendedSupportListFormat = string(output.FormatPrettyPrint)
		extendedSupportListQuiet = false
	})

	buf := new(bytes.Buffer)
	cmd := extendedSupportListCmd
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	if err := runExtendedSupportList(cmd, nil); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(buf.Bytes(), []byte(`"total_count": 1`)) {
		t.Fatalf("output = %s", buf.String())
	}
	if !bytes.Contains(buf.Bytes(), []byte(`"resource_id": "db-old"`)) {
		t.Fatalf("output = %s", buf.String())
	}
}
