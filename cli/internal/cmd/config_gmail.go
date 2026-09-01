package cmd

import (
	"fmt"

	"github.com/openshift-online/finops-tools/cli/internal/gmailauth"
	"github.com/spf13/cobra"
)

// config_gmail.go implements "finops config gmail login".
// It only verifies gcloud Application Default Credentials; finops never writes ADC.

var configGmailLoginQuotaProject string

var configGmailCmd = &cobra.Command{
	Use:   "gmail",
	Short: "Verify Gmail access for owner notification email",
}

var configGmailLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Verify Gmail send access via gcloud Application Default Credentials",
	Long: fmt.Sprintf(`Verify that existing gcloud Application Default Credentials can send Gmail.

finops-tools does not modify ~/.config/gcloud/application_default_credentials.json.
If you need to sign in or add Gmail scopes, run gcloud yourself (with --disable-quota-project
so gcloud does not write your config project into ADC; finops bills API calls to %s):

  %s

Gmail API quota is billed to %s by default (override with --quota-project or
FINOPS_GMAIL_QUOTA_PROJECT) without changing your ADC file.

Example:
  finops config gmail login`, gmailauth.DefaultGmailQuotaProject, gmailauth.ADCLoginHint(), gmailauth.DefaultGmailQuotaProject),
	RunE: runConfigGmailLogin,
}

func init() {
	configCmd.AddCommand(configGmailCmd)
	configGmailCmd.AddCommand(configGmailLoginCmd)
	configGmailLoginCmd.Flags().StringVar(&configGmailLoginQuotaProject, "quota-project", "",
		fmt.Sprintf("GCP project for Gmail API quota/billing (default: %s or $FINOPS_GMAIL_QUOTA_PROJECT)", gmailauth.DefaultGmailQuotaProject))
}

func runConfigGmailLogin(cmd *cobra.Command, _ []string) error {
	quotaProject := resolveGmailQuotaProject(configGmailLoginQuotaProject)
	email, err := gmailauth.TryADC(cmd.Context(), quotaProject)
	if err != nil {
		return fmt.Errorf(`gmail is not authorized: %v

finops does not modify your Application Default Credentials file.
Sign in with gcloud manually:
  %s

Gmail API quota project used by finops: %s`, err, gmailauth.ADCLoginHint(), quotaProject)
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "Gmail authorized as %s (quota project: %s).\n", email, quotaProject)
	return err
}

func resolveGmailQuotaProject(flagProject string) string {
	return gmailauth.QuotaProject(flagProject)
}
