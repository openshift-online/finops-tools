// resolve_ou.go parses --ou flags and builds cost targets for OU member accounts.
package configstore

import (
	"errors"
	"fmt"
	"strings"

	"github.com/openshift-online/finops-tools/cli/internal/account"
	coreaccount "github.com/openshift-online/finops-tools/core/account"
	"github.com/openshift-online/finops-tools/core/cost"
)

// OUSelector is one --ou value: an OU or org-root ID plus optional walk depth.
// MaxDepth nil means unbounded descendants; 0 = accounts directly in the parent only;
// 1 = parent + immediate child OUs.
type OUSelector struct {
	ID       string
	MaxDepth *int
}

// ParseOUSelectors parses comma-separated --ou values with optional scope suffixes:
//
//	ou-xxxx / r-xxxx      all accounts under parent (default)
//	ou-xxxx/ / r-xxxx/    accounts directly in parent only
//	ou-xxxx/* / r-xxxx/*  parent + immediate child OUs only
//	ou-xxxx/**            same as bare ID (explicit subtree)
func ParseOUSelectors(s string) ([]OUSelector, error) {
	tokens, err := account.ParseCommaSeparated(s)
	if err != nil {
		return nil, err
	}
	if len(tokens) == 0 {
		return nil, errors.New("at least one OU ID is required")
	}
	out := make([]OUSelector, 0, len(tokens))
	for _, token := range tokens {
		sel, err := parseOUSelectorToken(token)
		if err != nil {
			return nil, err
		}
		out = append(out, sel)
	}
	return out, nil
}

func parseOUSelectorToken(token string) (OUSelector, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return OUSelector{}, fmt.Errorf("empty OU ID")
	}

	id := token
	var maxDepth *int
	switch {
	case strings.HasSuffix(token, "/**"):
		id = strings.TrimSuffix(token, "/**")
		maxDepth = nil
	case strings.HasSuffix(token, "/*"):
		id = strings.TrimSuffix(token, "/*")
		d := 1
		maxDepth = &d
	case strings.HasSuffix(token, "/"):
		id = strings.TrimSuffix(token, "/")
		d := 0
		maxDepth = &d
	default:
		if strings.Contains(token, "/") {
			return OUSelector{}, fmt.Errorf("invalid --ou %q (supported suffixes: /, /*, /**)", token)
		}
	}

	id = strings.TrimSpace(id)
	if err := coreaccount.ValidateParentID(id); err != nil {
		return OUSelector{}, fmt.Errorf("invalid OU ID: %w", err)
	}
	return OUSelector{ID: id, MaxDepth: maxDepth}, nil
}

// ParseOUIDs parses comma-separated bare AWS OU or org-root IDs (no scope suffixes).
// Prefer ParseOUSelectors when --ou may include /, /*, or /**.
func ParseOUIDs(s string) ([]string, error) {
	sels, err := ParseOUSelectors(s)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(sels))
	for i, sel := range sels {
		if sel.MaxDepth != nil {
			return nil, fmt.Errorf("invalid OU ID %q (bare IDs only; use scope suffixes via ParseOUSelectors)", sel.ID)
		}
		ids[i] = sel.ID
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
