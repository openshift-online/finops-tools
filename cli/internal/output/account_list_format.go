package output

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
)

type accountListJSONRow struct {
	Alias      string `json:"alias"`
	AccountID  string `json:"account_id"`
	Kind       string `json:"kind,omitempty"`
	PayerAlias string `json:"payer_alias,omitempty"`
	Role       string `json:"role,omitempty"`
}

func writeAccountListJSON(w io.Writer, provider string, entries []AccountListRow) error {
	rows := make([]accountListJSONRow, len(entries))
	for i, e := range entries {
		rows[i] = accountListJSONRow(e)
	}
	payload := struct {
		Provider string               `json:"provider"`
		Accounts []accountListJSONRow `json:"accounts"`
	}{
		Provider: provider,
		Accounts: rows,
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func writeAccountListCSV(w io.Writer, provider string, entries []AccountListRow) error {
	cw := csv.NewWriter(w)
	header := []string{"provider", "alias", "account_id", "kind", "payer_alias", "role"}
	if err := cw.Write(header); err != nil {
		return err
	}
	for _, e := range entries {
		if err := cw.Write([]string{
			provider,
			e.Alias,
			e.AccountID,
			e.Kind,
			e.PayerAlias,
			e.Role,
		}); err != nil {
			return err
		}
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		return fmt.Errorf("write csv: %w", err)
	}
	return nil
}
