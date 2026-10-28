package cmd

import (
	"testing"

	reportpkg "github.com/openshift-online/finops-tools/cli/internal/report"
	"github.com/spf13/cobra"
)

func TestParseReportTemplate(t *testing.T) {
	name, err := reportpkg.ParseTemplate("costs")
	if err != nil || name != reportpkg.TemplateCosts {
		t.Fatalf("got %q %v", name, err)
	}
	_, err = reportpkg.ParseTemplate("unknown")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseReportFormat(t *testing.T) {
	format, err := reportpkg.ParseFormat("html")
	if err != nil || format != reportpkg.FormatHTML {
		t.Fatalf("got %q %v", format, err)
	}
	format, err = reportpkg.ParseFormat("")
	if err != nil || format != reportpkg.FormatHTML {
		t.Fatalf("empty format: got %q %v", format, err)
	}
	_, err = reportpkg.ParseFormat("pdf")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestApplyExecutiveSummaryMonthsDefault(t *testing.T) {
	tests := []struct {
		name        string
		template    string
		setMonths   bool
		monthsValue int
		wantMonths  int
	}{
		{
			name:       "executive-summary defaults to 5 months",
			template:   reportpkg.TemplateExecutiveSummary,
			wantMonths: 5,
		},
		{
			name:        "explicit --months wins",
			template:    reportpkg.TemplateExecutiveSummary,
			setMonths:   true,
			monthsValue: 12,
			wantMonths:  12,
		},
		{
			name:       "other templates untouched",
			template:   reportpkg.TemplateCosts,
			wantMonths: 0,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			orig := periodFlags
			t.Cleanup(func() { periodFlags = orig })

			cmd := &cobra.Command{Use: "test"}
			addPeriodFlags(cmd)
			periodFlags.months = 0
			if tc.setMonths {
				if err := cmd.Flags().Set("months", "12"); err != nil {
					t.Fatal(err)
				}
			}

			applyExecutiveSummaryMonthsDefault(cmd, tc.template)
			if periodFlags.months != tc.wantMonths {
				t.Errorf("periodFlags.months = %d, want %d", periodFlags.months, tc.wantMonths)
			}
		})
	}
}
