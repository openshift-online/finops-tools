// extendedsupport.go registers the "finops extended-support" noun command.
package cmd

import (
	"github.com/spf13/cobra"
)

var extendedSupportCmd = &cobra.Command{
	Use:   "extended-support",
	Short: "AWS extended-support resources and historical charges",
	Long:  "Find AWS resources past standard support and query historical Extended Support charges.",
}

func init() {
	extendedSupportCmd.GroupID = "core"
	bindAWSPersistentFlags(extendedSupportCmd)
	rootCmd.AddCommand(extendedSupportCmd)
}
