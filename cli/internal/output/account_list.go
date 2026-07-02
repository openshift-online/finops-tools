// account_list.go formats registered cloud accounts for terminal output.
package output

import (
	"fmt"
	"io"
)

// AccountListRow is one registered account for list display.
type AccountListRow struct {
	Alias      string
	AccountID  string
	Kind       string // "payer", "linked", or empty (e.g. GCP)
	PayerAlias string
	Role       string
}

// WriteAWSAccountList renders registered AWS accounts (payer and linked).
func WriteAWSAccountList(w io.Writer, entries []AccountListRow) error {
	return WriteAccountListResult(w, FormatPrettyPrint, "aws", entries)
}

// WriteAccountListResult renders registered accounts for a provider in the requested format.
func WriteAccountListResult(w io.Writer, format Format, provider string, entries []AccountListRow) error {
	switch format {
	case FormatPrettyPrint:
		switch provider {
		case "gcp":
			return writeGCPAccountListPretty(w, entries)
		case "snowflake":
			return writeSnowflakeAccountListPretty(w, entries)
		default:
			return writeAWSAccountListPretty(w, entries)
		}
	case FormatJSON:
		return writeAccountListJSON(w, provider, entries)
	case FormatCSV:
		return writeAccountListCSV(w, provider, entries)
	default:
		return fmt.Errorf("unsupported format %q", format)
	}
}

// WriteGCPAccountList renders registered GCP account aliases.
func WriteGCPAccountList(w io.Writer, entries []AccountListRow) error {
	return WriteAccountListResult(w, FormatPrettyPrint, "gcp", entries)
}

// WriteSnowflakeAccountList renders registered Snowflake account aliases.
func WriteSnowflakeAccountList(w io.Writer, entries []AccountListRow) error {
	return WriteAccountListResult(w, FormatPrettyPrint, "snowflake", entries)
}
