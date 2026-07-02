// aws_list_ous.go implements "finops aws list-ous".
package cmd

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/openshift-online/finops-tools/cli/internal/account"
	"github.com/openshift-online/finops-tools/cli/internal/awsauth"
	"github.com/openshift-online/finops-tools/cli/internal/configstore"
	"github.com/openshift-online/finops-tools/cli/internal/output"
	coreaccount "github.com/openshift-online/finops-tools/core/account"
	"github.com/spf13/cobra"
)

var (
	accountListOUsEnsureCredentials  = awsauth.EnsureAccountCredentials
	accountListOUsLoadConfigForCreds = loadAWSConfigForCredentialsAccount
	accountListOUsFetch              = coreaccount.ListOrganizationalUnits
	accountListOUsFormat             string
	accountListOUsOutput             string
	accountListOUsParent             string
	accountListOUsPayer              string
)

var parentIDPattern = regexp.MustCompile(`^(ou-[0-9a-z]{4,32}-[0-9a-z]{4,32}|r-[0-9a-z]{4,32}-[0-9a-z]{4,32})$`)

var awsListOUsCmd = &cobra.Command{
	Use:   "list-ous",
	Short: "List AWS Organizational Units under a payer",
	Long: `List child AWS Organizational Units for discovery before using --ou on account/report commands.

Examples:
  finops aws list-ous --payer rh-control
  finops aws list-ous --payer rh-control --parent ou-abcd-1234
  finops aws list-ous --payer rh-control --format json`,
	Args: cobra.NoArgs,
	PreRunE: func(_ *cobra.Command, _ []string) error {
		if strings.TrimSpace(accountListOUsPayer) == "" {
			return fmt.Errorf("--payer is required")
		}
		if parent := strings.TrimSpace(accountListOUsParent); parent != "" && !parentIDPattern.MatchString(parent) {
			return fmt.Errorf("invalid parent ID %q (expected ou-xxxx-yyyyy or r-xxxx-yyyyy)", parent)
		}
		_, err := output.ParseFormat(accountListOUsFormat)
		return err
	},
	RunE: runAWSListOUs,
}

func init() {
	awsCmd.AddCommand(awsListOUsCmd)
	awsListOUsCmd.Flags().StringVar(&accountListOUsFormat, "format", string(output.FormatPrettyPrint),
		"Output format: pretty-print, json, csv")
	addOutputFlag(awsListOUsCmd, &accountListOUsOutput)
	awsListOUsCmd.Flags().StringVar(&accountListOUsPayer, "payer", "", "Registered payer alias (required)")
	awsListOUsCmd.Flags().StringVar(&accountListOUsParent, "parent", "", "Parent OU or root ID (default: organization root)")
}

func runAWSListOUs(cmd *cobra.Command, _ []string) error {
	format, err := output.ParseFormat(accountListOUsFormat)
	if err != nil {
		return err
	}

	configPath, err := configstore.ResolvePath(awsFlags.ConfigPath)
	if err != nil {
		return err
	}
	cfg, err := configstore.Load(configPath)
	if err != nil {
		return err
	}

	payerAlias := strings.TrimSpace(accountListOUsPayer)
	payerID, ok := cfg.PayerAccountIDForAlias(payerAlias)
	if !ok {
		return errUnknownPayerAlias(payerAlias)
	}

	profiles := account.AWSProfileNames(payerID, payerAlias, nil)
	ensureOpts, err := newAWSEnsureOptions(cmd, awsEnsureConfig{
		configPath:      awsFlags.ConfigPath,
		authMethodFlag:  awsFlags.AuthMethod,
		credentialsFile: awsFlags.CredentialsFile,
	})
	if err != nil {
		return err
	}
	ensureOpts.AccountName = payerID
	ensureOpts.ProfileNames = profiles
	awsCtx := awsCommandContext(cmd)
	if _, err := accountListOUsEnsureCredentials(awsCtx, ensureOpts); err != nil {
		return fmt.Errorf("%s: %w", payerID, mapCredentialError(payerID, err))
	}

	awsCfg, err := accountListOUsLoadConfigForCreds(awsCtx, cfg, payerID, awsFlags.CredentialsFile)
	if err != nil {
		return err
	}

	parentID := strings.TrimSpace(accountListOUsParent)
	ous, err := accountListOUsFetch(awsCtx, awsCfg, parentID)
	if err != nil {
		return fmt.Errorf("list organizational units: %w", err)
	}

	rows := make([]output.OrganizationalUnitRow, len(ous))
	for i, ou := range ous {
		rows[i] = output.OrganizationalUnitRow{
			ID:   ou.ID,
			Name: ou.Name,
		}
	}
	out, closeOut, err := resolveCommandOutput(cmd, accountListOUsOutput)
	if err != nil {
		return err
	}
	if closeOut != nil {
		defer closeOut()
	}
	return output.WriteOrganizationalUnitListResult(out, format, output.OrganizationalUnitListView{
		PayerAlias: payerAlias,
		ParentID:   parentID,
		Units:      rows,
	})
}
