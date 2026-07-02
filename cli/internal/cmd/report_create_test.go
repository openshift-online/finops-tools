package cmd

import (
	"testing"

	reportpkg "github.com/openshift-online/finops-tools/cli/internal/report"
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
