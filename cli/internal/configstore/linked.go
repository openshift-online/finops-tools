// linked.go models and persists linked (member) AWS accounts accessed via payer role assumption.
package configstore

import (
	"fmt"
	"strings"

	"github.com/openshift-online/finops-tools/cli/internal/account"
	"github.com/openshift-online/finops-tools/cli/internal/awsrole"
)

// LinkedAccount holds metadata for a member account accessed via role assumption from a payer.
type LinkedAccount struct {
	AccountID  string `yaml:"account_id"`
	PayerAlias string `yaml:"payer_alias"`
	Role       string `yaml:"role,omitempty"`
	// RoleARN is deprecated; use Role. Loaded for backward compatibility only.
	RoleARN string `yaml:"role_arn,omitempty"`
}

// RoleName returns the IAM role name for this linked account.
func (l LinkedAccount) RoleName() string {
	if r := strings.TrimSpace(l.Role); r != "" {
		return awsrole.NameFromARN(r)
	}
	return awsrole.NameFromARN(l.RoleARN)
}

// SetLinkedAccount records a linked account under alias and returns the updated config.
func (f File) SetLinkedAccount(alias string, linked LinkedAccount) (File, error) {
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return File{}, fmt.Errorf("alias is required")
	}
	linked.AccountID = strings.TrimSpace(linked.AccountID)
	linked.PayerAlias = strings.TrimSpace(linked.PayerAlias)
	roleName := linked.RoleName()
	if roleName == "" {
		roleName = strings.TrimSpace(linked.Role)
	}
	if roleName == "" {
		return File{}, fmt.Errorf("role name is required")
	}
	if err := awsrole.ValidateName(roleName); err != nil {
		return File{}, err
	}
	linked.Role = roleName
	linked.RoleARN = ""
	if err := account.ValidateAWSAccountID(linked.AccountID); err != nil {
		return File{}, err
	}
	if linked.PayerAlias == "" {
		return File{}, fmt.Errorf("payer alias is required")
	}
	payerID, ok := f.AWSAccountIDForAlias(linked.PayerAlias)
	if !ok {
		return File{}, fmt.Errorf("unknown payer alias %q (register payer with: finops config account add aws <12-digit-id> --alias %s)",
			linked.PayerAlias, linked.PayerAlias)
	}
	if !f.isPayerAccountID(payerID) {
		return File{}, fmt.Errorf("%q is a linked account alias, not a payer (use a payer alias for --payer)", linked.PayerAlias)
	}
	if f.AWS.AccountAliases == nil {
		f.AWS.AccountAliases = make(map[string]AWSAccountAlias)
	}
	f.AWS.AccountAliases[alias] = awsAccountAliasFromLinked(linked)
	return f, nil
}

// LinkedAccountForAlias returns linked account metadata when configured.
func (f File) LinkedAccountForAlias(alias string) (LinkedAccount, bool) {
	entry, ok := f.AWS.AccountAliases[strings.TrimSpace(alias)]
	if !ok {
		return LinkedAccount{}, false
	}
	return entry.LinkedAccount()
}

// PayerAccountIDForAlias returns the payer account ID for a payer alias (not a linked alias).
func (f File) PayerAccountIDForAlias(alias string) (string, bool) {
	id, ok := f.AWSAccountIDForAlias(alias)
	if !ok {
		return "", false
	}
	if f.isLinkedAlias(alias) {
		return "", false
	}
	return id, true
}

func (f File) isLinkedAlias(alias string) bool {
	_, ok := f.LinkedAccountForAlias(alias)
	return ok
}

func (f File) isPayerAccountID(accountID string) bool {
	accountID = strings.TrimSpace(accountID)
	if f.isLinkedAccountID(accountID) {
		return false
	}
	return f.HasAWSAccount(accountID)
}

func (f File) isLinkedAccountID(accountID string) bool {
	accountID = strings.TrimSpace(accountID)
	for _, entry := range f.AWS.AccountAliases {
		if entry.IsLinked() && entry.AccountID() == accountID {
			return true
		}
	}
	return false
}

// migrateAccountAliases normalizes linked entries (role_arn into role) on load.
func (f *File) migrateAccountAliases() {
	for alias, entry := range f.AWS.AccountAliases {
		if !entry.IsLinked() {
			continue
		}
		if strings.TrimSpace(entry.Role) == "" && strings.TrimSpace(entry.RoleARN) != "" {
			entry.Role = awsrole.NameFromARN(entry.RoleARN)
			entry.RoleARN = ""
			f.AWS.AccountAliases[alias] = entry
		}
	}
}

// RegisterAWSLinkedAccount persists linked account metadata in the config file.
func RegisterAWSLinkedAccount(path, linkedAccountID, alias, payerAlias, roleName string) error {
	cfg, err := Ensure(path)
	if err != nil {
		return err
	}
	if alias == "" {
		alias = linkedAccountID
	}
	cfg, err = cfg.SetLinkedAccount(alias, LinkedAccount{
		AccountID:  linkedAccountID,
		PayerAlias: payerAlias,
		Role:       roleName,
	})
	if err != nil {
		return err
	}
	return Save(path, cfg)
}
