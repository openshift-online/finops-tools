package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/openshift-online/finops-tools/cli/internal/output"
	"github.com/openshift-online/finops-tools/core/accountreview"
	"github.com/spf13/cobra"
)

func TestValidateNotifyOwnerSendFlags(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		send     bool
		yes      bool
		prefix   string
		wantFail bool
	}{
		{name: "plan only", send: false, yes: false, prefix: ""},
		{name: "send owners", send: true, yes: true, prefix: ""},
		{name: "send redirect", send: true, yes: false, prefix: "finops"},
		{name: "send missing mode", send: true, yes: false, prefix: "", wantFail: true},
		{name: "yes without send", send: false, yes: true, prefix: "", wantFail: true},
		{name: "yes and redirect", send: true, yes: true, prefix: "finops", wantFail: true},
		{name: "crlf prefix", send: true, yes: false, prefix: "finops\r\nBcc:x", wantFail: true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateNotifyOwnerSendFlags(tc.send, tc.yes, tc.prefix)
			if tc.wantFail && err == nil {
				t.Fatal("expected error")
			}
			if !tc.wantFail && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestWriteNotifyDeliverySummaryCounts(t *testing.T) {
	var buf strings.Builder
	err := writeNotifyDeliverySummary(&buf, output.FormatPrettyPrint, []accountreview.DeliveryResult{
		{OwnerEmail: "a@redhat.com", Status: accountreview.StatusPlanned, Reason: "not sent"},
		{AccountID: "111111111111", Status: accountreview.StatusOwnerNotFound, Reason: "owner tag not found"},
		{AccountID: "222222222222", Status: accountreview.StatusSkipped, Reason: "role assumption failed"},
	})
	if err != nil {
		t.Fatalf("writeNotifyDeliverySummary() error = %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Planned") || !strings.Contains(out, "Failed") || !strings.Contains(out, "Skipped") {
		t.Fatalf("summary output = %q", out)
	}
}

func TestRunAccountNotifyOwnerRequiresSelection(t *testing.T) {
	t.Cleanup(func() {
		notifyOwnerSend = false
		notifyOwnerYes = false
		notifyOwnerRedirectPrefix = ""
		notifyOwnerAccount = ""
		notifyOwnerPayer = ""
	})

	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := runAccountNotifyOwner(cmd, nil)
	if err == nil {
		t.Fatal("expected error without account selection")
	}
}
