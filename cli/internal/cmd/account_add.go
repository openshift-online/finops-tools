// config_account_add.go implements "finops config account add" to log in and register payer or linked AWS accounts.
package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/openshift-online/finops-tools/cli/internal/account"
	awsconfig "github.com/openshift-online/finops-tools/cli/internal/aws"
	"github.com/openshift-online/finops-tools/cli/internal/awsauth"
	"github.com/openshift-online/finops-tools/cli/internal/configstore"
	coreaccount "github.com/openshift-online/finops-tools/core/account"
	"github.com/spf13/cobra"
)

var (
	accountAddForce            bool
	accountAddAlias            string
	accountAddPayer            string
	accountAddRole             string
	accountAddSnowflakeRole    string
	accountAddSnowflakeWH      string
	accountAddSnowflakeDB      string
	accountAddSnowflakeSchema  string
	accountAddSnowflakeSSO     string
	accountAddSnowflakeSecrets string
	accountAddSnowflakeTokens  string
	addAccountFn               = account.Add
	addSnowflakeAccountFn      = account.AddSnowflake
	loadAWSAccountProfileFn    = awsconfig.LoadSharedConfigProfile
	detectAWSAccountKindFn     = coreaccount.DetectAccountKind
)

var accountAddCmd = &cobra.Command{
	Use:   "add <provider> <account>",
	Short: "Log in and register a cloud account",
	Long: `Log into a cloud account and save it in the finops config file.

For AWS, <account> must be a 12-digit account ID.

For Snowflake, <account> is the Snowflake account identifier (e.g. orgname-accountname).
OAuth uses Red Hat SSO with a client registered for Dataverse Snowflake. Configure the
client ID and secret first:

  finops config oauth-client set --client-id <id> --client-secret "$SECRET"

See: https://dataverse.pages.redhat.com/platform/snowflake/red-hat-sso-access/

Payer account (Cost Explorer and org billing):
  finops config account add aws 123456789012 --alias rh-control

Linked account (assume a role from a registered payer):
  finops config account add aws 111111111111 --alias osd-tenant-1 --payer rh-control
  finops config account add aws 111111111111 --payer rh-control --role CustomRole

The role name defaults to OrganizationAccountAccessRole, or the value of
defaults.aws.linked_role in the finops config (finops config default set).

With --auth-method saml (default): runs built-in Red Hat SAML login for the payer when credentials are missing.
With --auth-method profile: uses an existing ~/.aws profile when valid.

account get-cost supports payer and linked account aliases. Linked accounts use payer credentials
and the configured auth method (defaults.aws.auth_method or --auth-method).`,
	Args: cobra.ExactArgs(2),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		provider, err := account.ParseProvider(args[0])
		if err != nil {
			return err
		}
		if provider == account.ProviderSnowflake {
			return nil
		}
		if provider != account.ProviderAWS {
			return nil
		}
		if err := account.ValidateAWSAccountID(args[1]); err != nil {
			return err
		}
		if strings.TrimSpace(accountAddPayer) != "" {
			if _, err := resolveLinkedRoleName(cmd, awsFlags.ConfigPath, accountAddRole); err != nil {
				return err
			}
		}
		method, err := resolveAuthMethod(cmd, awsFlags.ConfigPath, awsFlags.AuthMethod)
		if err != nil {
			return err
		}
		if accountAddForce && method == awsauth.MethodProfile {
			return fmt.Errorf("cannot use --force with --auth-method profile")
		}
		return nil
	},
	RunE: runAccountAdd,
}

func init() {
	configAccountCmd.AddCommand(accountAddCmd)
	accountAddCmd.Flags().BoolVar(&accountAddForce, "force", false, "Re-run SAML login even if existing payer credentials are valid (AWS only)")
	accountAddCmd.Flags().StringVar(&accountAddAlias, "alias", "", "Friendly alias for the account (e.g. rh-control)")
	accountAddCmd.Flags().StringVar(&accountAddPayer, "payer", "", "Registered payer alias (linked account: assume role in <account> from this payer)")
	accountAddCmd.Flags().StringVar(&accountAddRole, "role", "", "IAM role name in the linked account (default: config aws.linked_role or OrganizationAccountAccessRole)")
	accountAddCmd.Flags().StringVar(&accountAddSnowflakeRole, "snowflake-role", "", "Default Snowflake role for the session")
	accountAddCmd.Flags().StringVar(&accountAddSnowflakeWH, "warehouse", "", "Default Snowflake warehouse")
	accountAddCmd.Flags().StringVar(&accountAddSnowflakeDB, "database", "", "Default Snowflake database")
	accountAddCmd.Flags().StringVar(&accountAddSnowflakeSchema, "schema", "", "Default Snowflake schema")
	accountAddCmd.Flags().StringVar(&accountAddSnowflakeSSO, "sso", "", "Red Hat SSO environment: prod or stage (default: config snowflake.sso_issuer or prod)")
	accountAddCmd.Flags().StringVar(&accountAddSnowflakeSecrets, "oauth-secrets-file", "", "Path to Snowflake OAuth client secrets file")
	accountAddCmd.Flags().StringVar(&accountAddSnowflakeTokens, "tokens-file", "", "Path to Snowflake OAuth tokens file")
}

func runAccountAdd(cmd *cobra.Command, args []string) error {
	provider, err := account.ParseProvider(args[0])
	if err != nil {
		return err
	}
	accountID := args[1]

	if provider == account.ProviderSnowflake {
		return runAccountAddSnowflake(cmd, accountID)
	}

	if provider == account.ProviderAWS && strings.TrimSpace(accountAddPayer) != "" {
		return runAccountAddLinked(cmd, accountID)
	}

	ensureOpts, err := buildAWSEnsureOptions(cmd)
	if err != nil {
		return err
	}

	awsCtx := awsCommandContext(cmd)
	res, err := addAccountFn(awsCtx, account.AddOptions{
		Provider:      provider,
		AccountID:     accountID,
		Alias:         accountAddAlias,
		AWSEnsureOpts: ensureOpts,
	})
	if err != nil {
		return err
	}
	if provider == account.ProviderAWS {
		awsCfg, err := loadAWSAccountProfileFn(awsCtx, res.Profile)
		if err != nil {
			return fmt.Errorf("load AWS profile %q: %w", res.Profile, err)
		}
		kind, kindErr := detectAWSAccountKindFn(awsCtx, awsCfg, res.AccountID)
		switch kind {
		case coreaccount.AccountKindLinked:
			return fmt.Errorf(
				"account %s appears to be a linked/member account (profile=%s); register it with --payer <payer-alias>",
				res.AccountID, res.Profile,
			)
		case coreaccount.AccountKindUnknown:
			if kindErr != nil {
				if _, err := fmt.Fprintf(cmd.ErrOrStderr(),
					"warning: unable to determine whether account %s is payer or linked (profile=%s): %v; continuing as payer because --payer was not set\n",
					res.AccountID, res.Profile, kindErr,
				); err != nil {
					return err
				}
			} else {
				if _, err := fmt.Fprintf(cmd.ErrOrStderr(),
					"warning: unable to determine whether account %s is payer or linked (profile=%s); continuing as payer because --payer was not set\n",
					res.AccountID, res.Profile,
				); err != nil {
					return err
				}
			}
		}
	}

	if err := registerAWSAccount(awsFlags.ConfigPath, res.AccountID, accountAddAlias); err != nil {
		return err
	}

	return printAccountAddResult(cmd, res, accountAddAlias, "payer")
}

func runAccountAddLinked(cmd *cobra.Command, linkedAccountID string) error {
	payerAlias := strings.TrimSpace(accountAddPayer)
	payerAccountID, err := payerAccountIDFromConfig(awsFlags.ConfigPath, payerAlias)
	if err != nil {
		return err
	}

	roleARN, err := resolveLinkedRoleARN(cmd, awsFlags.ConfigPath, linkedAccountID, accountAddRole)
	if err != nil {
		return err
	}
	roleName, err := resolveLinkedRoleName(cmd, awsFlags.ConfigPath, accountAddRole)
	if err != nil {
		return err
	}

	payerEnsure, err := buildAWSEnsureOptions(cmd)
	if err != nil {
		return err
	}

	awsCtx := awsCommandContext(cmd)
	res, err := account.AddAWSLinked(awsCtx, account.AddAWSLinkedOptions{
		LinkedAccountID: linkedAccountID,
		Alias:           accountAddAlias,
		PayerAccountID:  payerAccountID,
		PayerAlias:      payerAlias,
		RoleARN:         roleARN,
		PayerEnsureOpts: payerEnsure,
		EnsureLinkedOpts: awsconfig.EnsureLinkedOptions{
			CredentialsPath: awsFlags.CredentialsFile,
		},
	})
	if err != nil {
		return err
	}

	if err := registerAWSLinkedAccount(awsFlags.ConfigPath, res.AccountID, accountAddAlias, payerAlias, roleName); err != nil {
		return err
	}

	return printAccountAddResult(cmd, res, accountAddAlias, "linked")
}

func runAccountAddSnowflake(cmd *cobra.Command, accountID string) error {
	path, err := configstore.ResolvePath(awsFlags.ConfigPath)
	if err != nil {
		return err
	}
	cfg, err := configstore.Load(path)
	if err != nil {
		return err
	}

	clientID, clientSecret, err := configstore.ResolveSnowflakeOAuthClient(accountAddSnowflakeSecrets)
	if err != nil {
		return err
	}

	sso := strings.TrimSpace(accountAddSnowflakeSSO)
	if sso == "" {
		sso = cfg.SnowflakeSSOIssuer()
	}
	oauthCfg, err := snowflakeOAuthConfig(cfg, clientID, clientSecret, sso)
	if err != nil {
		return err
	}

	alias := strings.TrimSpace(accountAddAlias)
	if alias == "" {
		alias = accountID
	}

	session := cfg.ResolveSnowflakeSession(configstore.SnowflakeAccount{
		Account:   accountID,
		Role:      accountAddSnowflakeRole,
		Warehouse: accountAddSnowflakeWH,
		Database:  accountAddSnowflakeDB,
		Schema:    accountAddSnowflakeSchema,
		SSO:       sso,
	})
	if err := configstore.ValidateSnowflakeWarehouse(session, alias); err != nil {
		return err
	}

	existingTok, err := loadSnowflakeToken(alias, accountAddSnowflakeTokens)
	if err != nil && !errors.Is(err, errSnowflakeTokensNotFound) {
		return err
	}

	res, err := addSnowflakeAccountFn(cmd.Context(), account.AddSnowflakeOptions{
		Account: account.SnowflakeAccountSettings{
			Account:   session.Account,
			Role:      session.Role,
			Warehouse: session.Warehouse,
			Database:  session.Database,
			Schema:    session.Schema,
		},
		Alias:         alias,
		OAuth:         oauthCfg,
		TokensPath:    accountAddSnowflakeTokens,
		ExistingToken: existingTok,
		ForceLogin:    accountAddForce,
	})
	if err != nil {
		return err
	}

	if err := registerSnowflakeAccount(path, alias, session); err != nil {
		return err
	}

	return printAccountAddResult(cmd, res, alias, "snowflake")
}

func registerSnowflakeAccount(configPath, alias string, acct configstore.SnowflakeAccount) error {
	path, err := configstore.ResolvePath(configPath)
	if err != nil {
		return err
	}
	return configstore.RegisterSnowflakeAccount(path, alias, acct)
}

func buildAWSEnsureOptions(cmd *cobra.Command) (awsauth.EnsureOptions, error) {
	return newAWSEnsureOptions(cmd, awsEnsureConfig{
		configPath:      awsFlags.ConfigPath,
		authMethodFlag:  awsFlags.AuthMethod,
		force:           accountAddForce,
		credentialsFile: awsFlags.CredentialsFile,
	})
}

func printAccountAddResult(cmd *cobra.Command, res account.AddResult, alias, kind string) error {
	label := alias
	if label == "" {
		label = res.AccountID
	}
	if !res.Refreshed {
		_, err := fmt.Fprintf(cmd.OutOrStdout(),
			"%s account %s added (provider=%s profile=%s account=%s arn=%s)\n",
			kind, label, res.Provider, res.Profile, res.AccountID, res.ARN,
		)
		return err
	}
	_, err := fmt.Fprintf(cmd.OutOrStdout(),
		"%s account %s added and credentials stored (provider=%s profile=%s account=%s arn=%s)\n",
		kind, label, res.Provider, res.Profile, res.AccountID, res.ARN,
	)
	return err
}
