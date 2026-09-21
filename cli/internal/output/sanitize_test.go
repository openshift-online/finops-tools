package output

import (
	"bytes"
	"strings"
	"testing"

	"github.com/openshift-online/finops-tools/core/accountreview"
)

func TestSanitizeTerminalStripsControlsAndOSC(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"plain", "plain"},
		{"name\x1b[31mRED\x1b[0m", "nameRED"},
		{"x\x1b]8;;https://evil.example\x07click", "xclick"},
		{"a\x1b]8;;https://evil.example\x1b\\b", "ab"},
		{"keep\tme", "keepme"},
		{"line\nbreak", "linebreak"},
		{"bell\x07gone", "bellgone"},
	}
	for _, tc := range cases {
		if got := SanitizeTerminal(tc.in); got != tc.want {
			t.Errorf("SanitizeTerminal(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestWriteAccountDetailsPrettySanitizesAWSMetadata(t *testing.T) {
	var buf bytes.Buffer
	d := accountreview.AccountDetails{
		AccountID:   "111111111111",
		AccountName: "acct\x1b[31m",
		EC2Instances: []accountreview.EC2Detail{{
			InstanceID: "i-abc",
			Name:       "web\x1b]8;;https://evil.example\x07",
			Type:       "t3.micro",
			State:      "running",
			Region:     "us-east-1",
		}},
	}
	if err := WriteAccountDetails(&buf, FormatPrettyPrint, []accountreview.AccountDetails{d}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, "\x1b[31m") || strings.Contains(out, "\x1b]8;") {
		t.Fatalf("pretty output still contains injected ESC:\n%q", out)
	}
	plain := stripANSI(out)
	if !strings.Contains(plain, "acct") || !strings.Contains(plain, "web") {
		t.Fatalf("sanitized names missing:\n%s", out)
	}
}
