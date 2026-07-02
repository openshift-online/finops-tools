package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestResolveCommandOutputStdout(t *testing.T) {
	buf := new(bytes.Buffer)
	cmd := &cobra.Command{}
	cmd.SetOut(buf)

	out, closeOut, err := resolveCommandOutput(cmd, "")
	if err != nil {
		t.Fatal(err)
	}
	if closeOut != nil {
		t.Fatal("expected no close func for stdout")
	}
	if _, err := out.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "hello" {
		t.Fatalf("stdout = %q", got)
	}
}

func TestResolveCommandOutputFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.json")
	cmd := &cobra.Command{}

	out, closeOut, err := resolveCommandOutput(cmd, path)
	if err != nil {
		t.Fatal(err)
	}
	if closeOut == nil {
		t.Fatal("expected close func for file output")
	}
	defer closeOut()

	if _, err := out.Write([]byte(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}
	closeOut()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(data)); got != `{"ok":true}` {
		t.Fatalf("file contents = %q", got)
	}
}
