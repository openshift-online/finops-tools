package report

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/openshift-online/finops-tools/core/cost"
	corehcp "github.com/openshift-online/finops-tools/core/hcphierarchy"
	execsum "github.com/openshift-online/finops-tools/core/report/execsummary"
)

// execSummaryTestTarget builds a resolved cost target for the given alias,
// as resolveCostTargets would produce for --account-alias <alias>.
func execSummaryTestTarget(alias string) cost.AccountTarget {
	return cost.AccountTarget{AccountID: "111111111111", DisplayAlias: alias}
}

// execSummaryTestInput is a fully valid GenerateInput for the
// executive-summary skeleton; individual tests zero out one field at a time.
func execSummaryTestInput() GenerateInput {
	return GenerateInput{
		Format:          FormatHTML,
		Targets:         []cost.AccountTarget{execSummaryTestTarget("rh-control")},
		Range:           cost.DateRange{Start: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)},
		Progress:        noopStepper{},
		Now:             time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC),
		SnowflakeAlias:  "rhsandbox",
		MappingCSVPath:  "/tmp/mapping.csv",
		DataDumpOutPath: "/tmp/dump.json",
	}
}

func TestExecutiveSummaryGeneratorForRegistersTemplate(t *testing.T) {
	if _, err := GeneratorFor(TemplateExecutiveSummary); err != nil {
		t.Fatalf("GeneratorFor(executive-summary): %v", err)
	}
	if got := AccountTargetModeFor(TemplateExecutiveSummary); got != AccountTargetsRequiredAndSnowflake {
		t.Fatalf("executive-summary mode = %v, want required-and-snowflake", got)
	}
}

func TestExecutiveSummaryValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(in *GenerateInput)
		wantErr string
	}{
		{
			name: "valid",
		},
		{
			name:    "unsupported format",
			mutate:  func(in *GenerateInput) { in.Format = "pdf" },
			wantErr: `does not support format`,
		},
		{
			name:    "missing snowflake opener",
			wantErr: "snowflake mart opener not configured",
		},
		{
			name:    "missing snowflake alias",
			mutate:  func(in *GenerateInput) { in.SnowflakeAlias = "" },
			wantErr: "requires --snowflake-alias",
		},
		{
			name:    "missing mapping csv",
			mutate:  func(in *GenerateInput) { in.MappingCSVPath = "" },
			wantErr: "requires --mapping-csv",
		},
		{
			name:    "missing payer alias",
			mutate:  func(in *GenerateInput) { in.Targets = nil },
			wantErr: "missing: rh-control",
		},
		{
			name: "window too short",
			mutate: func(in *GenerateInput) {
				in.Range = cost.DateRange{Start: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}
			},
			wantErr: "at least 2 months",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := execSummaryTestInput()
			if tc.mutate != nil {
				tc.mutate(&in)
			}
			var gen Generator = newExecutiveSummaryGenerator(openSnowflakeQuerierStub)
			if tc.name == "missing snowflake opener" {
				gen = newExecutiveSummaryGenerator(nil)
			}
			err := gen.Validate(in)
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

func openSnowflakeQuerierStub(context.Context, string, string) (corehcp.SnowflakeQueryer, error) {
	return nil, nil
}

func TestExecutiveSummaryGenerateWritesMinimalDump(t *testing.T) {
	dumpPath := filepath.Join(t.TempDir(), "dump.json")
	in := execSummaryTestInput()
	in.DataDumpOutPath = dumpPath

	gen := newExecutiveSummaryGenerator(nil) // skeleton opens no connections
	if err := gen.Generate(context.Background(), in); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	data, err := os.ReadFile(dumpPath)
	if err != nil {
		t.Fatal(err)
	}
	var dump FullDataDump
	if err := json.Unmarshal(data, &dump); err != nil {
		t.Fatalf("unmarshal dump: %v", err)
	}
	if dump.WindowStart != "2026-05" {
		t.Errorf("WindowStart = %q, want 2026-05", dump.WindowStart)
	}
	if dump.WindowEnd != "2026-09" {
		t.Errorf("WindowEnd = %q, want 2026-09", dump.WindowEnd)
	}
	wantMonths := []string{"2026-05", "2026-06", "2026-07", "2026-08", "2026-09"}
	if len(dump.Months) != len(wantMonths) {
		t.Fatalf("Months = %v, want %v", dump.Months, wantMonths)
	}
	for i, m := range wantMonths {
		if dump.Months[i] != m {
			t.Fatalf("Months = %v, want %v", dump.Months, wantMonths)
		}
	}
	if dump.GeneratedAt == "" {
		t.Error("GeneratedAt is empty")
	}
}

func TestExecutiveSummaryGenerateRequiresDataDumpOut(t *testing.T) {
	in := execSummaryTestInput()
	in.DataDumpOutPath = ""
	gen := newExecutiveSummaryGenerator(nil)
	err := gen.Generate(context.Background(), in)
	if err == nil || !strings.Contains(err.Error(), "--data-dump-out") {
		t.Fatalf("err = %v, want --data-dump-out required error", err)
	}
}

func TestExecSummaryWindow(t *testing.T) {
	tests := []struct {
		name       string
		start, end time.Time
		wantMonths []string
	}{
		{
			name:       "five months",
			start:      time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			end:        time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
			wantMonths: []string{"2026-05", "2026-06", "2026-07", "2026-08", "2026-09"},
		},
		{
			name:       "end mid-month excludes that month",
			start:      time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			end:        time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC),
			wantMonths: []string{"2026-05", "2026-06", "2026-07", "2026-08", "2026-09"},
		},
		{
			name:       "empty range",
			start:      time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
			end:        time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
			wantMonths: nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := execSummaryWindow(cost.DateRange{Start: tc.start, End: tc.end})
			got := monthLabelsFor(w.Months)
			if len(got) != len(tc.wantMonths) {
				t.Fatalf("months = %v, want %v", got, tc.wantMonths)
			}
			for i, m := range tc.wantMonths {
				if got[i] != m {
					t.Fatalf("months = %v, want %v", got, tc.wantMonths)
				}
			}
			if len(w.Months) > 0 {
				if want := tc.wantMonths[len(tc.wantMonths)-1]; execsum.MonthLabel(w.End) != want {
					t.Errorf("End label = %q, want %q", execsum.MonthLabel(w.End), want)
				}
			}
		})
	}
}

func TestExecSummaryPayerTargets(t *testing.T) {
	if _, err := execSummaryPayerTargets([]cost.AccountTarget{execSummaryTestTarget("rh-control")}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := execSummaryPayerTargets([]cost.AccountTarget{execSummaryTestTarget("other-alias")}); err == nil {
		t.Fatal("expected error for missing rh-control alias")
	}
	if _, err := execSummaryPayerTargets(nil); err == nil {
		t.Fatal("expected error for no targets")
	}
}
