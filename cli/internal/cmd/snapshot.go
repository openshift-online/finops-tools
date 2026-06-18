// snapshot.go registers the "finops snapshot" noun command for AWS snapshot discovery.
package cmd

import (
	"github.com/spf13/cobra"
)

var snapshotCmd = &cobra.Command{
	Use:   "snapshot",
	Short: "AWS snapshot discovery",
	Long:  "Find AWS EBS and RDS snapshots older than a cutoff.",
}

func init() {
	snapshotCmd.GroupID = "core"
	bindAWSPersistentFlags(snapshotCmd)
	rootCmd.AddCommand(snapshotCmd)
}
