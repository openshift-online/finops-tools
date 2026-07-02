// aws.go registers the "finops aws" noun command for AWS-specific operations.
package cmd

import (
	"github.com/spf13/cobra"
)

var awsCmd = &cobra.Command{
	Use:   "aws",
	Short: "AWS-specific operations",
	Long:  "Commands that apply only to AWS (for example organizational unit discovery).",
}

func init() {
	awsCmd.GroupID = "core"
	bindAWSPersistentFlags(awsCmd)
	rootCmd.AddCommand(awsCmd)
}
