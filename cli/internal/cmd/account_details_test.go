package cmd

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/openshift-online/finops-tools/cli/internal/configstore"
	"github.com/openshift-online/finops-tools/core/accountreview"
	"github.com/spf13/cobra"
)

func TestValidateAccountDetailsSnowflakeAlias(t *testing.T) {
	t.Parallel()
	cfg := configstore.File{
		Snowflake: configstore.SnowflakeConfig{
			AccountAliases: map[string]configstore.SnowflakeAccount{
				"rhprod": {Account: "ORG-ACCT", Warehouse: "WH"},
			},
		},
	}
	if err := validateAccountDetailsSnowflakeAlias(cfg, "", false); err != nil {
		t.Fatalf("omitted alias: %v", err)
	}
	err := validateAccountDetailsSnowflakeAlias(cfg, "", true)
	if err == nil {
		t.Fatal("expected empty explicit alias error")
	}
	if !strings.Contains(err.Error(), "was set but is empty") {
		t.Fatalf("empty explicit alias error = %v", err)
	}
	err = validateAccountDetailsSnowflakeAlias(cfg, "   ", true)
	if err == nil {
		t.Fatal("expected blank explicit alias error")
	}
	if err := validateAccountDetailsSnowflakeAlias(cfg, "rhprod", true); err != nil {
		t.Fatalf("known alias: %v", err)
	}
	err = validateAccountDetailsSnowflakeAlias(cfg, "missing", true)
	if err == nil {
		t.Fatal("expected unknown alias error")
	}
	if !strings.Contains(err.Error(), "unknown snowflake account alias") {
		t.Fatalf("error = %v", err)
	}
	noWH := configstore.File{
		Snowflake: configstore.SnowflakeConfig{
			AccountAliases: map[string]configstore.SnowflakeAccount{
				"nowh": {Account: "ORG-ACCT"},
			},
		},
	}
	err = validateAccountDetailsSnowflakeAlias(noWH, "nowh", true)
	if err == nil {
		t.Fatal("expected warehouse error")
	}
	noAcct := configstore.File{
		Snowflake: configstore.SnowflakeConfig{
			AccountAliases: map[string]configstore.SnowflakeAccount{
				"noacct": {Warehouse: "WH"},
			},
		},
	}
	err = validateAccountDetailsSnowflakeAlias(noAcct, "noacct", true)
	if err == nil {
		t.Fatal("expected account identifier error")
	}
	if !strings.Contains(err.Error(), "no account identifier") {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateAccountDetailsSendFlags(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		send       bool
		yes        bool
		prefix     string
		groupBySet bool
		wantFail   bool
	}{
		{name: "plan only", send: false, yes: false, prefix: ""},
		{name: "send owners", send: true, yes: true, prefix: ""},
		{name: "send redirect", send: true, yes: false, prefix: "finops"},
		{name: "send missing mode", send: true, yes: false, prefix: "", wantFail: true},
		{name: "yes without send", send: false, yes: true, prefix: "", wantFail: true},
		{name: "redirect without send", send: false, yes: false, prefix: "finops", wantFail: true},
		{name: "group-by without send", send: false, groupBySet: true, wantFail: true},
		{name: "group-by with send", send: true, yes: true, groupBySet: true},
		{name: "yes and redirect", send: true, yes: true, prefix: "finops", wantFail: true},
		{name: "crlf prefix", send: true, yes: false, prefix: "finops\r\nBcc:x", wantFail: true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateAccountDetailsSendFlags(tc.send, tc.yes, tc.prefix, tc.groupBySet)
			if tc.wantFail && err == nil {
				t.Fatal("expected error")
			}
			if !tc.wantFail && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestAccountDetailsAfterSummary(t *testing.T) {
	t.Parallel()

	writeErr := errors.New("write failed")
	sendFailed := []accountreview.DeliveryResult{{Status: accountreview.StatusSendFailed}}
	twoFailed := []accountreview.DeliveryResult{
		{Status: accountreview.StatusSendFailed},
		{Status: accountreview.StatusSent},
		{Status: accountreview.StatusSendFailed},
	}

	cases := []struct {
		name       string
		summaryErr error
		results    []accountreview.DeliveryResult
		wantErr    error
		wantMsg    string
	}{
		{name: "summary write error with send failures", summaryErr: writeErr, results: sendFailed, wantErr: writeErr},
		{name: "summary write error with no send failures", summaryErr: writeErr, results: []accountreview.DeliveryResult{{Status: accountreview.StatusSent}}, wantErr: writeErr},
		{name: "zero failures sent", results: []accountreview.DeliveryResult{{Status: accountreview.StatusSent}}},
		{name: "zero failures planned", results: []accountreview.DeliveryResult{{Status: accountreview.StatusPlanned}}},
		{name: "owner not found is not send failure", results: []accountreview.DeliveryResult{{Status: accountreview.StatusOwnerNotFound}}},
		{name: "invalid owner is not send failure", results: []accountreview.DeliveryResult{{Status: accountreview.StatusInvalidOwner}}},
		{name: "skipped is not send failure", results: []accountreview.DeliveryResult{{Status: accountreview.StatusSkipped}}},
		{name: "nil results"},
		{name: "one send failed", results: sendFailed, wantMsg: "1 notification failed to send"},
		{name: "two send failed", results: twoFailed, wantMsg: "2 notifications failed to send"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := accountDetailsAfterSummary(tc.summaryErr, tc.results)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("error = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if tc.wantMsg == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || err.Error() != tc.wantMsg {
				t.Fatalf("error = %v, want %q", err, tc.wantMsg)
			}
		})
	}
}

func TestWriteAccountDetailsDeliverySummaryCounts(t *testing.T) {
	var buf strings.Builder
	err := writeAccountDetailsDeliverySummary(&buf, []accountreview.DeliveryResult{
		{OwnerEmail: "a@redhat.com", Status: accountreview.StatusPlanned, Reason: "not sent"},
		{AccountID: "111111111111", Status: accountreview.StatusOwnerNotFound, Reason: "owner tag not found"},
		{AccountID: "222222222222", Status: accountreview.StatusSkipped, Reason: "role assumption failed"},
	})
	if err != nil {
		t.Fatalf("writeAccountDetailsDeliverySummary() error = %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Planned") || !strings.Contains(out, "Failed") || !strings.Contains(out, "Skipped") {
		t.Fatalf("summary output = %q", out)
	}
}

func TestRunAccountDetailsRequiresSelection(t *testing.T) {
	t.Cleanup(func() {
		detailsSend = false
		detailsYes = false
		detailsRedirectPrefix = ""
		detailsAccount = ""
		detailsPayer = ""
		detailsOutput = ""
	})

	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := runAccountDetails(cmd, nil)
	if err == nil {
		t.Fatal("expected error without account selection")
	}
}

func TestAccountDetailsHasNoNotifyOwnerAlias(t *testing.T) {
	if len(accountDetailsCmd.Aliases) != 0 {
		t.Fatalf("aliases = %v, want none", accountDetailsCmd.Aliases)
	}
	cmd, args, err := accountCmd.Find([]string{"notify-owner"})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if cmd == accountDetailsCmd || cmd.Name() == "notify-owner" {
		t.Fatalf("notify-owner should not resolve, got %q args=%v", cmd.Name(), args)
	}
}

func TestAccountDetailsFormatFlagAllowsCSV(t *testing.T) {
	flag := accountDetailsCmd.Flags().Lookup("format")
	if flag == nil {
		t.Fatal("missing --format")
	}
	if !strings.Contains(flag.Usage, "csv") {
		t.Fatalf("format help = %q", flag.Usage)
	}
	if accountDetailsCmd.Flags().Lookup("output") == nil {
		t.Fatal("missing --output")
	}
}

func TestAccountReviewBuildHookDefaultsToCore(t *testing.T) {
	if accountReviewBuild == nil {
		t.Fatal("accountReviewBuild is nil")
	}
}

func TestCachedConfigLoaderCallsOnce(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	load := cachedConfigLoader(func(context.Context) (aws.Config, error) {
		calls.Add(1)
		return aws.Config{Region: "us-east-1"}, nil
	})
	cfg1, err := load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cfg2, err := load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
	if cfg1.Region != "us-east-1" || cfg2.Region != "us-east-1" {
		t.Fatalf("cfg = %+v / %+v", cfg1, cfg2)
	}
}

func TestCachedConfigLoaderCachesError(t *testing.T) {
	t.Parallel()
	want := errors.New("assume-role denied")
	var calls atomic.Int32
	load := cachedConfigLoader(func(context.Context) (aws.Config, error) {
		calls.Add(1)
		return aws.Config{}, want
	})
	_, err1 := load(context.Background())
	_, err2 := load(context.Background())
	if !errors.Is(err1, want) || !errors.Is(err2, want) {
		t.Fatalf("err1=%v err2=%v", err1, err2)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
}
