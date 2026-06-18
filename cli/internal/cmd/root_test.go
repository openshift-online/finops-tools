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
		"Setup & Extra:",
		"cost",
		"report",
		"snapshot",
		"config",
		"account",
		"demo",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("help output missing %q\n%s", want, out)
		}
	}
}
