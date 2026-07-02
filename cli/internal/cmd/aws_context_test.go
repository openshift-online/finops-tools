package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/openshift-online/finops-tools/cli/internal/extrace"
	"github.com/openshift-online/finops-tools/core/apilog"
	"github.com/spf13/cobra"
)

func TestAwsCommandContextVerbose(t *testing.T) {
	root := &cobra.Command{Use: "finops"}
	bindAWSPersistentFlags(root)
	cmd := &cobra.Command{Use: "sub"}
	root.AddCommand(cmd)
	if err := root.PersistentFlags().Set("verbose", "true"); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	cmd.SetErr(&buf)

	ctx := awsCommandContext(cmd)
	apilog.Log(ctx, "STS.GetCallerIdentity")
	extrace.LogCommand(ctx, "klist", "-s")

	out := buf.String()
	if !strings.Contains(out, "+ AWS STS.GetCallerIdentity") {
		t.Fatalf("missing API log line: %q", out)
	}
	if !strings.Contains(out, `+ "klist" "-s"`) {
		t.Fatalf("missing external command log line: %q", out)
	}
}

func TestAwsCommandContextQuietByDefault(t *testing.T) {
	root := &cobra.Command{Use: "finops"}
	bindAWSPersistentFlags(root)
	cmd := &cobra.Command{Use: "sub"}
	root.AddCommand(cmd)

	var buf bytes.Buffer
	cmd.SetErr(&buf)

	ctx := awsCommandContext(cmd)
	apilog.Log(ctx, "STS.GetCallerIdentity")
	extrace.LogCommand(ctx, "klist", "-s")

	if buf.Len() != 0 {
		t.Fatalf("expected no verbose output without -v, got %q", buf.String())
	}
}

func TestAwsVerboseEnabledWalksParents(t *testing.T) {
	root := &cobra.Command{Use: "finops"}
	bindAWSPersistentFlags(root)
	child := &cobra.Command{Use: "child"}
	leaf := &cobra.Command{Use: "leaf"}
	root.AddCommand(child)
	child.AddCommand(leaf)
	if err := root.PersistentFlags().Set("verbose", "true"); err != nil {
		t.Fatal(err)
	}
	if !awsVerboseEnabled(leaf) {
		t.Fatal("expected verbose on descendant command")
	}
}
