package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestReportCreateHelpSeparatesAWSFlags(t *testing.T) {
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"report", "create", "--help"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("execute --help: %v", err)
	}

	out := buf.String()
	flagsIdx := strings.Index(out, "Flags:\n")
	globalIdx := strings.Index(out, "Global Flags:\n")
	if flagsIdx < 0 || globalIdx < 0 || globalIdx <= flagsIdx {
		t.Fatalf("expected Flags then Global Flags sections:\n%s", out)
	}
	flagsSection := out[flagsIdx:globalIdx]
	globalSection := out[globalIdx:]
	if strings.Contains(flagsSection, "--auth-method") || strings.Contains(flagsSection, "--credentials-file") {
		t.Errorf("AWS flags should not appear under Flags:\n%s", flagsSection)
	}
	for _, flag := range []string{"--auth-method", "--config", "--credentials-file", "--verbose"} {
		if !strings.Contains(globalSection, flag) {
			t.Errorf("Global Flags missing %s:\n%s", flag, globalSection)
		}
	}
	if !strings.Contains(globalSection, "-v,") && !strings.Contains(globalSection, "-v ") {
		t.Errorf("Global Flags missing -v shorthand:\n%s", globalSection)
	}
}
