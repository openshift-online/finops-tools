package parallel

import "testing"

func TestShouldReportProgress(t *testing.T) {
	cases := []struct {
		index, total int
		want         bool
	}{
		{1, 1, false},
		{1, 100, true},
		{24, 100, false},
		{25, 100, true},
		{100, 100, true},
		{5, 10, true},
	}
	for _, tc := range cases {
		if got := ShouldReportProgress(tc.index, tc.total); got != tc.want {
			t.Fatalf("ShouldReportProgress(%d, %d) = %v, want %v", tc.index, tc.total, got, tc.want)
		}
	}
}
