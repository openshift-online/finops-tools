package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func addOutputFlag(cmd *cobra.Command, value *string) {
	cmd.Flags().StringVarP(value, "output", "o", "", "Write output to this file instead of stdout")
}

func resolveCommandOutput(cmd *cobra.Command, path string) (io.Writer, func(), error) {
	if path = strings.TrimSpace(path); path != "" {
		f, err := os.Create(path)
		if err != nil {
			return nil, nil, fmt.Errorf("create output file: %w", err)
		}
		return f, func() { _ = f.Close() }, nil
	}
	return cmd.OutOrStdout(), nil, nil
}
