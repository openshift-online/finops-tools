// extendedsupport.go registers the "finops extended-support" noun command.
package cmd

import (
	"github.com/spf13/cobra"
)

var extendedSupportCmd = &cobra.Command{
	Use:   "extended-support",
	Short: "AWS resources running in extended support",
	Long:  "Find AWS resources running engine or platform versions past their standard support end date.",
}

func init() {
	extendedSupportCmd.GroupID = "core"
	bindAWSPersistentFlags(extendedSupportCmd)
	rootCmd.AddCommand(extendedSupportCmd)
}
