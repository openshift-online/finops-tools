package cmd

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/openshift-online/finops-tools/cli/internal/configstore"
	"github.com/openshift-online/finops-tools/cli/internal/output"
	reportpkg "github.com/openshift-online/finops-tools/cli/internal/report"
	coreaccount "github.com/openshift-online/finops-tools/core/account"
	"github.com/openshift-online/finops-tools/core/cost"
	"github.com/spf13/cobra"
)

func TestValidateCostTargetSelector(t *testing.T) {
	tests := []struct {
		name    string
		sel     costTargetSelector
		wantErr string
	}{
		{
			name: "explicit account",
			sel:  costTargetSelector{Aliases: []string{"rh-control"}},
		},
		{
			name: "tag mode",
			sel:  costTargetSelector{PayerAlias: "rh-control", TagKey: "env"},
		},
		{
			name: "ou mode",
			sel:  costTargetSelector{OUIDs: []string{"ou-abcd-1234"}, PayerAlias: "rh-control"},
		},
		{
			name:    "neither",
			sel:     costTargetSelector{},
			wantErr: "provide --account/--account-alias, --ou, or --tag-key",
		},
		{
			name:    "both modes",
			sel:     costTargetSelector{Aliases: []string{"rh-control"}, TagKey: "env", PayerAlias: "rh-control"},
			wantErr: "not both",
		},
		{
			name:    "tag without payer",
			sel:     costTargetSelector{TagKey: "env"},
			wantErr: "--payer is required with --tag-key",
		},
		{
			name:    "payer alone",
			sel:     costTargetSelector{PayerAlias: "rh-control"},
			wantErr: "--payer requires --account or --ou",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := validateCostTargetSelector(tc.sel)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected error")
			}
			if tc.wantErr != "" && !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestValidateReportCostTargetSelector(t *testing.T) {
	tests := []struct {
		name     string
		template string
		sel      costTargetSelector
		wantErr  string
	}{
		{
			name:     "hcp-hierarchy no alias",
			template: reportpkg.TemplateHCPHierarchy,
			sel:      costTargetSelector{},
		},
		{
			name:     "hcp-hierarchy snowflake alias",
			template: reportpkg.TemplateHCPHierarchy,
			sel:      costTargetSelector{Aliases: []string{"rhsandbox"}},
		},
		{
			name:     "hcp-hierarchy rejects aws alias flags",
			template: reportpkg.TemplateHCPHierarchy,
			sel:      costTargetSelector{AccountIDs: []string{"111111111111"}},
			wantErr:  "does not use AWS account targets",
		},
		{
			name:     "hcp-hierarchy rejects multiple aliases",
			template: reportpkg.TemplateHCPHierarchy,
			sel:      costTargetSelector{Aliases: []string{"rhsandbox", "rhprod"}},
			wantErr:  "single Snowflake --account-alias",
		},
		{
			name:     "costs optional empty",
			template: reportpkg.TemplateCosts,
			sel:      costTargetSelector{},
		},
		{
			name:     "savings-plans requires targets",
			template: reportpkg.TemplateSavingsPlans,
			sel:      costTargetSelector{},
			wantErr:  "provide --account/--account-alias, --ou, or --tag-key",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateReportCostTargetSelector(tc.template, tc.sel)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestResolveCostTargetsByTag(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := configstore.RegisterAWSAccount(path, "123456789012", "rh-control"); err != nil {
		t.Fatal(err)
	}
	cfg, err := configstore.Load(path)
	if err != nil {
		t.Fatal(err)
	}

	origEnsure := ensureCostCredentials
	origLoad := loadAWSConfigForCredentialsAccount
	origFilter := filterOrganizationAccountsByTag
	t.Cleanup(func() {
		ensureCostCredentials = origEnsure
		loadAWSConfigForCredentialsAccount = origLoad
		filterOrganizationAccountsByTag = origFilter
	})

	ensureCostCredentials = func(context.Context, *cobra.Command, configstore.File, []cost.AccountTarget, string, string, string) error {
		return nil
	}
	loadAWSConfigForCredentialsAccount = func(context.Context, configstore.File, string, string) (aws.Config, error) {
		return aws.Config{}, nil
	}
	filterOrganizationAccountsByTag = func(context.Context, aws.Config, string, string, string, coreaccount.TagFilterProgress, string, bool, bool) ([]coreaccount.OrganizationAccount, error) {
		return []coreaccount.OrganizationAccount{
			{ID: "111111111111", Name: "Prod"},
			{ID: "222222222222", Name: "Stage"},
		}, nil
	}

	cmd := &cobra.Command{}
	targets, err := resolveCostTargets(context.Background(), cmd, cfg, costTargetSelector{
		PayerAlias: "rh-control",
		TagKey:     "env",
	}, path, "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 2 {
		t.Fatalf("got %d targets, want 2", len(targets))
	}
	if targets[0].PayerAccountID != "123456789012" || !targets[0].ScopeAccountOnly {
		t.Fatalf("unexpected target[0]: %+v", targets[0])
	}
	if targets[0].DisplayName != "Prod" {
		t.Fatalf("DisplayName = %q", targets[0].DisplayName)
	}
}

func TestResolveCostTargetsByTagNoMatches(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := configstore.RegisterAWSAccount(path, "123456789012", "rh-control"); err != nil {
		t.Fatal(err)
	}
	cfg, err := configstore.Load(path)
	if err != nil {
		t.Fatal(err)
	}

	origEnsure := ensureCostCredentials
	origLoad := loadAWSConfigForCredentialsAccount
	origFilter := filterOrganizationAccountsByTag
	t.Cleanup(func() {
		ensureCostCredentials = origEnsure
		loadAWSConfigForCredentialsAccount = origLoad
		filterOrganizationAccountsByTag = origFilter
	})

	ensureCostCredentials = func(context.Context, *cobra.Command, configstore.File, []cost.AccountTarget, string, string, string) error {
		return nil
	}
	loadAWSConfigForCredentialsAccount = func(context.Context, configstore.File, string, string) (aws.Config, error) {
		return aws.Config{}, nil
	}
	filterOrganizationAccountsByTag = func(context.Context, aws.Config, string, string, string, coreaccount.TagFilterProgress, string, bool, bool) ([]coreaccount.OrganizationAccount, error) {
		return nil, nil
	}

	cmd := &cobra.Command{}
	targets, err := resolveCostTargets(context.Background(), cmd, cfg, costTargetSelector{
		PayerAlias: "rh-control",
		TagKey:     "env",
		TagValue:   "prod",
	}, path, "", "", nil)
	if err != nil {
		t.Fatalf("resolveCostTargets: %v", err)
	}
	if len(targets) != 0 {
		t.Fatalf("got %d targets, want 0", len(targets))
	}
}

func TestValidateOrgCacheFlags(t *testing.T) {
	if err := validateOrgCacheFlags(false, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := validateOrgCacheFlags(true, true); err == nil {
		t.Fatal("expected error for skip and refresh together")
	}
}

func TestCostGetPreRunETagMode(t *testing.T) {
	costGetAccount = ""
	costGetAccountAliases = ""
	costGetPayer = "rh-control"
	costGetTagKey = "env"
	costGetTagValue = ""
	costGetFormat = string(output.FormatPrettyPrint)
	costGetProvider = string(cost.ProviderAWS)
	costGetSplitBy = ""
	t.Cleanup(func() {
		costGetAccount = ""
		costGetAccountAliases = ""
		costGetPayer = ""
		costGetTagKey = ""
		costGetTagValue = ""
		costGetFormat = ""
		costGetProvider = ""
		costGetSplitBy = ""
	})

	if err := costGetCmd.PreRunE(costGetCmd, nil); err != nil {
		t.Fatalf("PreRunE: %v", err)
	}
}
