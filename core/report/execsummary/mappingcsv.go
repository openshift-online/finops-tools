package execsummary

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strings"
)

// UnmappedServiceName is the fallback Service Name assigned to any AWS
// account present in Cost Explorer data but absent from the mapping CSV
// (golang-executive-summary-spec.md section 3.4). It must still appear in
// the Cost-by-Category tab under this label, never be silently dropped.
const UnmappedServiceName = "Unmapped"

// mappingCSVHeader is the required, exact 6-column header for the external
// mapping CSV (spec section 3.4). No spend/dollar columns and no
// owner/team column exist in this schema.
var mappingCSVHeader = []string{
	"Account ID", "Account name", "Service Name", "service-phase", "cost-center", "app-code",
}

// AccountTaxonomy is one row of the spec section 3.4 mapping CSV, after
// parsing and account-ID normalization.
type AccountTaxonomy struct {
	// AccountID is the normalized join key (see NormalizeAccountID).
	AccountID string
	// AccountName is the display name for the account.
	AccountName string
	// ServiceName is the category field -- the sole grouping dimension for
	// the Cost-by-Category tab and Top-5-Growing-Accounts labeling.
	ServiceName string
	// ServicePhase is an environment-like label (e.g. "prod"/"stage"/"dev").
	// Parsed but not rendered by any tab today.
	ServicePhase string
	// CostCenter is a finance tagging field. Parsed but not rendered.
	CostCenter string
	// AppCode is an org tagging field. Parsed but not rendered.
	AppCode string
}

// NormalizeAccountID makes an AWS account ID comparable across the mapping
// CSV and AWS Cost Explorer's LINKED_ACCOUNT by stripping whitespace, `-`
// characters, and leading zeros (spec section 3.4). Apply this to both sides
// of any join -- skipping it on either side causes silent join misses that
// fall back to UnmappedServiceName.
func NormalizeAccountID(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.ReplaceAll(s, "-", "")
	s = strings.TrimLeft(s, "0")
	return s
}

// AccountTaxonomyMap is a normalized-account-ID-keyed lookup produced by
// LoadMappingCSV / LoadMappingCSVFile.
type AccountTaxonomyMap map[string]AccountTaxonomy

// LoadMappingCSVFile opens path and parses it as the spec section 3.4
// mapping CSV. No network or auth is involved -- this is a plain local file
// read of a required report input, not a credential/config file.
func LoadMappingCSVFile(path string) (AccountTaxonomyMap, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open mapping csv: %w", err)
	}
	defer func() {
		_ = f.Close()
	}()
	return LoadMappingCSV(f)
}

// LoadMappingCSV parses the required 6-column external mapping CSV (spec
// section 3.4) into a normalized-account-ID-keyed lookup map. The header row
// is required and must match mappingCSVHeader exactly (case-sensitive,
// trimmed). Rows with a blank Account ID after normalization are skipped
// rather than failing the whole load.
func LoadMappingCSV(r io.Reader) (AccountTaxonomyMap, error) {
	cr := csv.NewReader(r)
	cr.TrimLeadingSpace = true

	header, err := cr.Read()
	if err != nil {
		return nil, fmt.Errorf("read mapping csv header: %w", err)
	}
	if err := validateMappingCSVHeader(header); err != nil {
		return nil, err
	}

	out := make(AccountTaxonomyMap)
	rowNum := 1
	for {
		record, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read mapping csv row %d: %w", rowNum+1, err)
		}
		rowNum++
		if len(record) != len(mappingCSVHeader) {
			return nil, fmt.Errorf("mapping csv row %d has %d columns, want %d", rowNum, len(record), len(mappingCSVHeader))
		}

		id := NormalizeAccountID(record[0])
		if id == "" {
			continue
		}
		out[id] = AccountTaxonomy{
			AccountID:    id,
			AccountName:  strings.TrimSpace(record[1]),
			ServiceName:  strings.TrimSpace(record[2]),
			ServicePhase: strings.TrimSpace(record[3]),
			CostCenter:   strings.TrimSpace(record[4]),
			AppCode:      strings.TrimSpace(record[5]),
		}
	}
	return out, nil
}

func validateMappingCSVHeader(header []string) error {
	if len(header) != len(mappingCSVHeader) {
		return fmt.Errorf("mapping csv header has %d columns, want %d (%v)", len(header), len(mappingCSVHeader), mappingCSVHeader)
	}
	for i, want := range mappingCSVHeader {
		if strings.TrimSpace(header[i]) != want {
			return fmt.Errorf("mapping csv header column %d = %q, want %q", i+1, header[i], want)
		}
	}
	return nil
}

// ServiceName returns the Service Name for accountID, falling back to
// UnmappedServiceName when no match exists (spec section 3.4's "Unmapped
// fallback" rule) or when the matched row has a blank Service Name.
// accountID is normalized internally, so callers may pass raw AWS account
// IDs (e.g. CostRecord.AccountID) directly.
func (m AccountTaxonomyMap) ServiceName(accountID string) string {
	if t, ok := m[NormalizeAccountID(accountID)]; ok && t.ServiceName != "" {
		return t.ServiceName
	}
	return UnmappedServiceName
}

// AccountName returns the Account name for accountID, falling back to
// accountID itself when no match exists.
func (m AccountTaxonomyMap) AccountName(accountID string) string {
	if t, ok := m[NormalizeAccountID(accountID)]; ok && t.AccountName != "" {
		return t.AccountName
	}
	return accountID
}
