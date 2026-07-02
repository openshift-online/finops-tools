// aws_flags.go registers shared AWS and finops config flags on noun commands.
package cmd

import (
	"github.com/openshift-online/finops-tools/cli/internal/awsauth"
	"github.com/spf13/cobra"
)

// awsFlags are bound as persistent flags on account, report, snapshot, tag, aws, and config account commands.
// Help output lists them under "Global Flags", separate from each subcommand's flags.
var awsFlags struct {
	AuthMethod      string
	ConfigPath      string
	CredentialsFile string
	Verbose         bool
}

func bindAWSPersistentFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().StringVar(&awsFlags.AuthMethod, "auth-method", string(awsauth.MethodSAML),
		"AWS authentication method: saml or profile (overrides config default when set)")
	cmd.PersistentFlags().StringVar(&awsFlags.ConfigPath, "config", "",
		"Path to finops config file (default: OS-specific config dir)")
	cmd.PersistentFlags().StringVar(&awsFlags.CredentialsFile, "credentials-file", "",
		"Path to AWS credentials file (default: ~/.aws/credentials)")
	cmd.PersistentFlags().BoolVarP(&awsFlags.Verbose, "verbose", "v", false,
		"Log external commands and AWS API calls (STS, Organizations, EC2, RDS, Cost Explorer) to stderr when -v is set")
}
