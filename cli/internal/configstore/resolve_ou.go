// resolve_ou.go parses --ou flags and builds cost targets for OU member accounts.
package configstore

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/openshift-online/finops-tools/cli/internal/account"
	"github.com/openshift-online/finops-tools/core/cost"
)

var ouIDPattern = regexp.MustCompile(`^ou-[0-9a-z]{4,32}-[0-9a-z]{4,32}$`)

// ParseOUIDs parses comma-separated AWS OU IDs from --ou.
func ParseOUIDs(s string) ([]string, error) {
	ids, err := account.ParseCommaSeparated(s)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, errors.New("at least one OU ID is required")
	}
	for _, id := range ids {
		if !ouIDPattern.MatchString(id) {
			return nil, fmt.Errorf("invalid OU ID %q (expected format ou-xxxx-yyyyy)", id)
		}
	}
	return ids, nil
}

// ResolveOUAccountTargets builds linked cost.AccountTarget values for OU member accounts.
func ResolveOUAccountTargets(cfg File, memberAccountIDs []string, payerAlias string) ([]cost.AccountTarget, error) {
	payerAlias = strings.TrimSpace(payerAlias)
	if payerAlias == "" {
		return nil, errors.New("payer alias is required for OU account targets")
	}
	payerAccountID, ok := cfg.PayerAccountIDForAlias(payerAlias)
	if !ok {
		return nil, fmt.Errorf("unknown payer alias %q (register payer with: finops config account add aws <12-digit-id> --alias %s)", payerAlias, payerAlias)
	}

	var out []cost.AccountTarget
	seen := make(map[string]struct{})
	for _, id := range memberAccountIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if err := account.ValidateAWSAccountID(id); err != nil {
			return nil, err
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		displayAlias := cfg.AliasForAccountID(id)
		if displayAlias == id {
			displayAlias = ""
		}
		out = append(out, cost.AccountTarget{
			AccountID:      id,
			PayerAccountID: payerAccountID,
			DisplayAlias:   displayAlias,
		})
	}
	return out, nil
}
