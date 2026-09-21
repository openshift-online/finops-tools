package output

import (
	"bytes"
	"strings"
	"testing"

	"github.com/openshift-online/finops-tools/core/accountreview"
)

func TestWriteNotifySummaryPrettyColoredHeadersKeepSGR(t *testing.T) {
	t.Setenv("FORCE_COLOR", "1")
	t.Setenv("NO_COLOR", "")

	var buf bytes.Buffer
	err := WriteNotifySummary(&buf, FormatPrettyPrint, NotifySummary{
		Sent: 1,
		Results: []accountreview.DeliveryResult{{
			AccountID:  "111111111111",
			OwnerEmail: "jdoe@redhat.com",
			Status:     accountreview.StatusSent,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw := buf.String()
	if strings.Contains(raw, "\033[1M") || strings.Contains(raw, "\033[0M") {
		t.Fatalf("AutoFormatHeaders mangled SGR into Delete Line CSI:\n%q", raw)
	}
	for _, want := range []string{
		"\033[1mACCOUNT\033[0m",
		"\033[1mOWNER\033[0m",
		"\033[1mSTATUS\033[0m",
		"\033[1mREASON\033[0m",
	} {
		if !strings.Contains(raw, want) {
			t.Fatalf("expected bold header %q, got:\n%q", want, raw)
		}
	}
	out := stripANSI(raw)
	if !strings.Contains(out, "111111111111") || !strings.Contains(out, "jdoe@redhat.com") {
		t.Fatalf("rows missing:\n%s", out)
	}
}
