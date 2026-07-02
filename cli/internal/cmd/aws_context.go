package cmd

import (
	"context"
	"fmt"

	"github.com/openshift-online/finops-tools/cli/internal/extrace"
	"github.com/openshift-online/finops-tools/core/apilog"
	"github.com/spf13/cobra"
)

func awsCommandContext(cmd *cobra.Command) context.Context {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	if awsVerboseEnabled(cmd) {
		w := cmd.ErrOrStderr()
		ctx = extrace.WithWriter(ctx, w)
		ctx = apilog.WithLog(ctx, func(line string) {
			_, _ = fmt.Fprintf(w, "+ AWS %s\n", line)
		})
	}
	return ctx
}

func awsVerboseEnabled(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		if c.PersistentFlags().Lookup("verbose") == nil {
			continue
		}
		verbose, err := c.PersistentFlags().GetBool("verbose")
		return err == nil && verbose
	}
	return false
}
