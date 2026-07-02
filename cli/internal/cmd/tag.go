// tag.go registers the "finops tag" noun command for cloud account tag metadata.
package cmd

import (
	"github.com/spf13/cobra"
)

var tagCmd = &cobra.Command{
	Use:   "tag",
	Short: "Manage cloud account tags",
	Long:  "List, add, or update tags on cloud accounts (AWS Organizations today).",
}

func init() {
	tagCmd.GroupID = "core"
	bindAWSPersistentFlags(tagCmd)
	rootCmd.AddCommand(tagCmd)
}
