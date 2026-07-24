package output

import (
	"bytes"
	"strings"
	"testing"

	"github.com/openshift-online/finops-tools/core/snapshot"
)

func TestWriteSnapshotAccountWarnings(t *testing.T) {
	var buf bytes.Buffer
	r := snapshot.Result{
		Summary: snapshot.Summary{
			SkippedAccounts: []snapshot.AccountWarning{
				{
					AccountID: "222222222222",
					Message:   "AccessDenied: not authorized to perform sts:AssumeRole",
				},
			},
		},
	}
	if err := WriteSnapshotListResult(&buf, FormatPrettyPrint, r); err != nil {
		t.Fatal(err)
	}
	out := stripANSI(buf.String())
	if !strings.Contains(out, "Skipped accounts") {
		t.Fatalf("output = %q", out)
	}
	if !strings.Contains(out, "222222222222") {
		t.Fatalf("output = %q", out)
	}
	if !strings.Contains(out, "AccessDenied") {
		t.Fatalf("output = %q", out)
	}
}
