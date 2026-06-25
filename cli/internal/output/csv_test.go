package output

import "testing"

func TestSanitizeCSVField(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"", ""},
		{"snap-abc", "snap-abc"},
		{"=1+1", "'=1+1"},
		{"+cmd", "'+cmd"},
		{"-sum(A1)", "'-sum(A1)"},
		{"@SUM(1,1)", "'@SUM(1,1)"},
		{"\t=1+1", "'\t=1+1"},
		{"\r=1+1", "'\r=1+1"},
		{"\n=1+1", "'\n=1+1"},
		{"\t@SUM(1,1)", "'\t@SUM(1,1)"},
	}
	for _, tc := range tests {
		if got := sanitizeCSVField(tc.in); got != tc.want {
			t.Errorf("sanitizeCSVField(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
