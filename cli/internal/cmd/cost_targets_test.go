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
			sel:  costTargetSelector{OUs: []configstore.OUSelector{{ID: "ou-abcd-12345678"}}, PayerAlias: "rh-control"},
		},
		{
			name: "org mode payer alone",
			sel:  costTargetSelector{PayerAlias: "rh-control"},
		},
		{
			name:    "neither",
			sel:     costTargetSelector{},
			wantErr: "provide --account-id/--account-alias, --ou, --tag, or --payer alone",
		},
		{
			name:    "account and ou footgun",
			sel:     costTargetSelector{AccountIDs: []string{"111111111111"}, OUs: []configstore.OUSelector{{ID: "ou-abcd-12345678"}}, PayerAlias: "osd-staging-2"},
			wantErr: errMixedAccountSelection,
		},
		{
			name:    "both explicit and tag",
			sel:     costTargetSelector{Aliases: []string{"rh-control"}, TagKey: "env", PayerAlias: "rh-control"},
			wantErr: errMixedAccountSelection,
		},
		{
			name:    "tag without payer",
			sel:     costTargetSelector{TagKey: "env"},
			wantErr: "--tag requires --payer",
		},
		{
			name:    "ou without payer",
			sel:     costTargetSelector{OUs: []configstore.OUSelector{{ID: "ou-abcd-12345678"}}},
			wantErr: "--ou requires --payer",
		},
		{
			name:    "ou and tag",
			sel:     costTargetSelector{OUs: []configstore.OUSelector{{ID: "ou-abcd-12345678"}}, TagKey: "env", PayerAlias: "rh-control"},
			wantErr: errMixedAccountSelection,
		},
		{
			name: "explicit with payer for member IDs",
			sel:  costTargetSelector{AccountIDs: []string{"333333333333"}, PayerAlias: "rh-control"},
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
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestValidateReportCostTargetSelector(t *testing.T) {
	tests := []struct {
		name           string
		template       string
		sel            costTargetSelector
		snowflakeAlias string
		wantErr        string
	}{
		{
			name:     "hcp-hierarchy no alias",
			template: reportpkg.TemplateHCPHierarchy,
			sel:      costTargetSelector{},
		},
		{
			name:           "hcp-hierarchy snowflake alias",
			template:       reportpkg.TemplateHCPHierarchy,
			sel:            costTargetSelector{},
			snowflakeAlias: "rhsandbox",
		},
		{
			name:     "hcp-hierarchy rejects aws alias flags",
			template: reportpkg.TemplateHCPHierarchy,
			sel:      costTargetSelector{AccountIDs: []string{"111111111111"}},
			wantErr:  "does not use AWS account targets",
		},
		{
			name:     "hcp-hierarchy rejects aws account aliases",
			template: reportpkg.TemplateHCPHierarchy,
			sel:      costTargetSelector{Aliases: []string{"rh-control"}},
			wantErr:  "does not use AWS account targets",
		},
		{
			name:     "costs requires targets",
			template: reportpkg.TemplateCosts,
			sel:      costTargetSelector{},
			wantErr:  "provide --account-id/--account-alias, --ou, --tag, or --payer alone",
		},
		{
			name:     "savings-plans requires targets",
			template: reportpkg.TemplateSavingsPlans,
			sel:      costTargetSelector{},
			wantErr:  "provide --account-id/--account-alias, --ou, --tag, or --payer alone",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateReportCostTargetSelector(tc.template, tc.sel, tc.snowflakeAlias)
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

func TestResolveCostTargetsOrg(t *testing.T) {
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
	origList := listOrganizationMemberAccounts
	origRoot := organizationRootID
	t.Cleanup(func() {
		ensureCostCredentials = origEnsure
		loadAWSConfigForCredentialsAccount = origLoad
		listOrganizationMemberAccounts = origList
		organizationRootID = origRoot
	})

	ensureCostCredentials = func(context.Context, *cobra.Command, configstore.File, []cost.AccountTarget, string, string, string) error {
		return nil
	}
	loadAWSConfigForCredentialsAccount = func(context.Context, configstore.File, string, string) (aws.Config, error) {
		return aws.Config{}, nil
	}
	listOrganizationMemberAccounts = func(context.Context, aws.Config, string) ([]coreaccount.OrganizationAccount, error) {
		return []coreaccount.OrganizationAccount{
			{ID: "123456789012", Name: "Payer"},
			{ID: "111111111111", Name: "Prod"},
			{ID: "222222222222", Name: "Stage"},
		}, nil
	}
	organizationRootID = func(context.Context, aws.Config) (string, error) {
		return "r-root", nil
	}

	cmd := &cobra.Command{}
	sel := costTargetSelector{PayerAlias: "rh-control"}
	targets, err := resolveCostTargets(cmd, cfg, &sel, path, "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 2 {
		t.Fatalf("got %d targets, want 2", len(targets))
	}
	if sel.SelectionRootID != "r-root" {
		t.Fatalf("SelectionRootID = %q", sel.SelectionRootID)
	}
	for _, target := range targets {
		if target.PayerAccountID != "123456789012" {
			t.Fatalf("unexpected target: %+v", target)
		}
		if target.AccountID == "123456789012" {
			t.Fatalf("payer account should be excluded: %+v", target)
		}
	}
	if targets[0].DisplayName != "Prod" {
		t.Fatalf("DisplayName = %q", targets[0].DisplayName)
	}
}

func TestResolveCostTargetsOUWithScope(t *testing.T) {
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
	origList := listAccountsUnderParent
	t.Cleanup(func() {
		ensureCostCredentials = origEnsure
		loadAWSConfigForCredentialsAccount = origLoad
		listAccountsUnderParent = origList
	})

	ensureCostCredentials = func(context.Context, *cobra.Command, configstore.File, []cost.AccountTarget, string, string, string) error {
		return nil
	}
	loadAWSConfigForCredentialsAccount = func(context.Context, configstore.File, string, string) (aws.Config, error) {
		return aws.Config{}, nil
	}

	var gotParent string
	var gotDepth *int
	listAccountsUnderParent = func(_ context.Context, _ aws.Config, parentID string, opts coreaccount.ListAccountsInOUOptions) ([]coreaccount.OrganizationAccount, error) {
		gotParent = parentID
		gotDepth = opts.MaxDepth
		return []coreaccount.OrganizationAccount{
			{ID: "111111111111", Name: "Direct"},
		}, nil
	}

	depth0 := 0
	cmd := &cobra.Command{}
	sel := costTargetSelector{
		PayerAlias: "rh-control",
		OUs:        []configstore.OUSelector{{ID: "ou-abcd-12345678", MaxDepth: &depth0}},
	}
	targets, err := resolveCostTargets(cmd, cfg, &sel, path, "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if gotParent != "ou-abcd-12345678" || gotDepth == nil || *gotDepth != 0 {
		t.Fatalf("parent=%q depth=%v", gotParent, gotDepth)
	}
	if len(targets) != 1 || targets[0].AccountID != "111111111111" {
		t.Fatalf("targets: %+v", targets)
	}
	if sel.SelectionRootID != "ou-abcd-12345678" {
		t.Fatalf("SelectionRootID = %q", sel.SelectionRootID)
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
	origRoot := organizationRootID
	t.Cleanup(func() {
		ensureCostCredentials = origEnsure
		loadAWSConfigForCredentialsAccount = origLoad
		filterOrganizationAccountsByTag = origFilter
		organizationRootID = origRoot
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
	organizationRootID = func(context.Context, aws.Config) (string, error) {
		return "r-root", nil
	}

	cmd := &cobra.Command{}
	sel := costTargetSelector{PayerAlias: "rh-control", TagKey: "env"}
	targets, err := resolveCostTargets(cmd, cfg, &sel, path, "", "", nil)
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
	sel := costTargetSelector{PayerAlias: "rh-control", TagKey: "env", TagValue: "prod"}
	targets, err := resolveCostTargets(cmd, cfg, &sel, path, "", "", nil)
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

func TestParseTagFlag(t *testing.T) {
	key, value, err := parseTagFlag("organization")
	if err != nil || key != "organization" || value != "" {
		t.Fatalf("got %q=%q err %v", key, value, err)
	}
	key, value, err = parseTagFlag(`organization=Hybrid Platform`)
	if err != nil || key != "organization" || value != "Hybrid Platform" {
		t.Fatalf("got %q=%q err %v", key, value, err)
	}
}

func TestParseCostTargetSelectorOUScope(t *testing.T) {
	sel, err := parseCostTargetSelector("", "", "ou-abcd-12345678/,ou-efgh-56789012/*", "rh-control", "", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(sel.OUs) != 2 {
		t.Fatalf("OUs = %+v", sel.OUs)
	}
	if sel.OUs[0].MaxDepth == nil || *sel.OUs[0].MaxDepth != 0 {
		t.Fatalf("direct scope: %+v", sel.OUs[0])
	}
	if sel.OUs[1].MaxDepth == nil || *sel.OUs[1].MaxDepth != 1 {
		t.Fatalf("children scope: %+v", sel.OUs[1])
	}
}

func TestCostGetPreRunETagMode(t *testing.T) {
	costGetAccount = ""
	costGetAccountAliases = ""
	costGetPayer = "rh-control"
	costGetTag = "env"
	costGetFormat = string(output.FormatPrettyPrint)
	costGetProvider = string(cost.ProviderAWS)
	costGetSplitBy = ""
	t.Cleanup(func() {
		costGetAccount = ""
		costGetAccountAliases = ""
		costGetPayer = ""
		costGetTag = ""
		costGetFormat = ""
		costGetProvider = ""
		costGetSplitBy = ""
	})

	if err := accountGetCostCmd.PreRunE(accountGetCostCmd, nil); err != nil {
		t.Fatalf("PreRunE: %v", err)
	}
}

func TestCostGetPreRunERejectsAccountAndOU(t *testing.T) {
	costGetAccount = "111111111111"
	costGetAccountAliases = ""
	costGetPayer = "osd-staging-2"
	costGetOU = "ou-abcd-12345678"
	costGetTag = ""
	costGetFormat = string(output.FormatPrettyPrint)
	costGetProvider = string(cost.ProviderAWS)
	costGetSplitBy = ""
	t.Cleanup(func() {
		costGetAccount = ""
		costGetPayer = ""
		costGetOU = ""
	})

	err := accountGetCostCmd.PreRunE(accountGetCostCmd, nil)
	if err == nil {
		t.Fatal("expected error for --account-id with --ou")
	}
	if !strings.Contains(err.Error(), errMixedAccountSelection) {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestResolveAccountOUBucketsMultiPayer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := configstore.RegisterAWSAccount(path, "123456789012", "rh-control"); err != nil {
		t.Fatal(err)
	}
	if err := configstore.RegisterAWSAccount(path, "987654321098", "osd-staging-1"); err != nil {
		t.Fatal(err)
	}
	cfg, err := configstore.Load(path)
	if err != nil {
		t.Fatal(err)
	}

	origLoad := loadAWSConfigForCredentialsAccount
	origRoot := organizationRootID
	origBuild := buildOUAccountMapping
	t.Cleanup(func() {
		loadAWSConfigForCredentialsAccount = origLoad
		organizationRootID = origRoot
		buildOUAccountMapping = origBuild
	})

	loadCalls := make([]string, 0)
	loadAWSConfigForCredentialsAccount = func(_ context.Context, _ configstore.File, payerID, _ string) (aws.Config, error) {
		loadCalls = append(loadCalls, payerID)
		return aws.Config{}, nil
	}
	organizationRootID = func(_ context.Context, _ aws.Config) (string, error) {
		return "r-ignored", nil
	}
	buildOUAccountMapping = func(_ context.Context, _ aws.Config, rootID string, accountIDs []string) (map[string]coreaccount.AccountOUBucket, []coreaccount.OUHierarchyNode, error) {
		out := make(map[string]coreaccount.AccountOUBucket, len(accountIDs))
		for _, id := range accountIDs {
			switch id {
			case "111111111111":
				out[id] = coreaccount.AccountOUBucket{ID: "ou-a-prod", Name: "Prod A"}
			case "222222222222":
				out[id] = coreaccount.AccountOUBucket{ID: "ou-b-sand", Name: "Sand B"}
			}
		}
		return out, []coreaccount.OUHierarchyNode{
			{ID: rootID, Name: "Root", Depth: 0},
		}, nil
	}

	// Force distinct roots per payer via organizationRootID keyed by load order.
	rootByPayer := map[string]string{
		"123456789012": "r-aaaa",
		"987654321098": "r-bbbb",
	}
	organizationRootID = func(_ context.Context, _ aws.Config) (string, error) {
		payer := loadCalls[len(loadCalls)-1]
		return rootByPayer[payer], nil
	}

	targets := []cost.AccountTarget{
		{AccountID: "111111111111", PayerAccountID: "123456789012"},
		{AccountID: "222222222222", PayerAccountID: "987654321098"},
	}
	buckets, hierarchy, _, err := resolveAccountOUBuckets(context.Background(), cfg, costTargetSelector{}, targets, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(loadCalls) != 2 {
		t.Fatalf("loadCalls = %v, want both payers", loadCalls)
	}
	if buckets["111111111111"].ID != "ou-a-prod" || buckets["222222222222"].ID != "ou-b-sand" {
		t.Fatalf("buckets = %+v", buckets)
	}
	if len(hierarchy) != 2 {
		t.Fatalf("hierarchy = %+v", hierarchy)
	}
	roots := map[string]bool{}
	for _, n := range hierarchy {
		roots[n.ID] = true
	}
	if !roots["r-aaaa"] || !roots["r-bbbb"] {
		t.Fatalf("expected both org roots in hierarchy: %+v", hierarchy)
	}
}
