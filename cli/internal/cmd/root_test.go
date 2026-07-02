// root_test.go tests root command help output and command group registration.
package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestRootHelpGroups(t *testing.T) {
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"--help"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("execute --help: %v", err)
	}

	out := buf.String()
	for _, want := range []string{
		"Core Commands:",
		"Setup:",
		"account",
		"snapshot",
		"report",
		"tag",
		"aws",
		"snowflake",
		"config",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("help output missing %q\n%s", want, out)
		}
	}
	for _, absent := range []string{"cost", "demo"} {
		if commandLineListed(out, absent) {
			t.Errorf("help output should not list top-level %q\n%s", absent, out)
		}
	}
}

func commandLineListed(help, name string) bool {
	for _, line := range strings.Split(help, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == name || strings.HasPrefix(trimmed, name+" ") {
			return true
		}
	}
	return false
}
