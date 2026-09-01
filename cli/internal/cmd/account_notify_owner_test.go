package cmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/openshift-online/finops-tools/cli/internal/output"
	"github.com/openshift-online/finops-tools/core/accountreview"
	"github.com/spf13/cobra"
)

func TestParseNotifySummaryFormat(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in      string
		want    output.Format
		wantErr bool
	}{
		{in: "pretty-print", want: output.FormatPrettyPrint},
		{in: "json", want: output.FormatJSON},
		{in: "JSON", want: output.FormatJSON},
		{in: "", want: output.FormatPrettyPrint},
		{in: "csv", wantErr: true},
		{in: "CSV", wantErr: true},
		{in: "yaml", wantErr: true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			got, err := parseNotifySummaryFormat(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

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

func TestNotifyOwnerAfterSummary(t *testing.T) {
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
			err := notifyOwnerAfterSummary(tc.summaryErr, tc.results)
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
