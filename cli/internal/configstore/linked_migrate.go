// linked_migrate.go updates a linked account's payer alias after an AWS organization transfer.
package configstore

import (
	"fmt"
	"strings"
)

// UpdateLinkedAccountPayer rewrites payer_alias (and optional role) for a registered linked account.
// accountOrAlias may be a linked alias or a 12-digit account ID.
func UpdateLinkedAccountPayer(path, accountOrAlias, newPayerAlias, roleName string) error {
	accountOrAlias = strings.TrimSpace(accountOrAlias)
	newPayerAlias = strings.TrimSpace(newPayerAlias)
	if accountOrAlias == "" {
		return fmt.Errorf("account or alias is required")
	}
	if newPayerAlias == "" {
		return fmt.Errorf("payer alias is required")
	}

	cfg, err := Ensure(path)
	if err != nil {
		return err
	}

	alias, linked, ok := cfg.findLinkedAccount(accountOrAlias)
	if !ok {
		return fmt.Errorf("no linked account registered for %q (skipping local config update)", accountOrAlias)
	}
	if roleName = strings.TrimSpace(roleName); roleName == "" {
		roleName = linked.RoleName()
	}
	if roleName == "" {
		roleName = cfg.AWSLinkedRoleName()
	}

	cfg, err = cfg.SetLinkedAccount(alias, LinkedAccount{
		AccountID:  linked.AccountID,
		PayerAlias: newPayerAlias,
		Role:       roleName,
	})
	if err != nil {
		return err
	}
	return Save(path, cfg)
}

func (f File) findLinkedAccount(accountOrAlias string) (alias string, linked LinkedAccount, ok bool) {
	accountOrAlias = strings.TrimSpace(accountOrAlias)
	if linked, ok = f.LinkedAccountForAlias(accountOrAlias); ok {
		return accountOrAlias, linked, true
	}
	for a, entry := range f.AWS.AccountAliases {
		if !entry.IsLinked() {
			continue
		}
		if entry.AccountID() == accountOrAlias {
			linked, _ = entry.LinkedAccount()
			return a, linked, true
		}
	}
	return "", LinkedAccount{}, false
}
