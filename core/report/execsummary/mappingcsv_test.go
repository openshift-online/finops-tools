package execsummary

import (
	"strings"
	"testing"
)

const validMappingCSV = `Account ID, Account name, Service Name, service-phase, cost-center, app-code
111111111111,Payer Control,HCP,prod,CC-100,APP-1
022222222222, Staging Shared ,Tooling, stage ,CC-200,APP-2
`

func TestLoadMappingCSV_ParsesRows(t *testing.T) {
	m, err := LoadMappingCSV(strings.NewReader(validMappingCSV))
	if err != nil {
		t.Fatalf("LoadMappingCSV() error = %v", err)
	}
	if len(m) != 2 {
		t.Fatalf("len(m) = %d, want 2", len(m))
	}

	row, ok := m["111111111111"]
	if !ok {
		t.Fatalf("missing row for 111111111111: %+v", m)
	}
	want := AccountTaxonomy{
		AccountID:    "111111111111",
		AccountName:  "Payer Control",
		ServiceName:  "HCP",
		ServicePhase: "prod",
		CostCenter:   "CC-100",
		AppCode:      "APP-1",
	}
	if row != want {
		t.Errorf("row = %+v, want %+v", row, want)
	}
}

func TestLoadMappingCSV_NormalizesLeadingZerosAndWhitespace(t *testing.T) {
	m, err := LoadMappingCSV(strings.NewReader(validMappingCSV))
	if err != nil {
		t.Fatalf("LoadMappingCSV() error = %v", err)
	}
	// "022222222222" normalizes to "22222222222" (leading zero stripped).
	row, ok := m["22222222222"]
	if !ok {
		t.Fatalf("expected normalized key 22222222222, got keys %v", keysOf(m))
	}
	if row.AccountName != "Staging Shared" {
		t.Errorf("AccountName = %q, want trimmed %q", row.AccountName, "Staging Shared")
	}
	if row.ServicePhase != "stage" {
		t.Errorf("ServicePhase = %q, want trimmed %q", row.ServicePhase, "stage")
	}
}

func keysOf(m AccountTaxonomyMap) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestLoadMappingCSV_SkipsBlankAccountID(t *testing.T) {
	csv := `Account ID, Account name, Service Name, service-phase, cost-center, app-code
   ,No ID Row,HCP,prod,CC-100,APP-1
111111111111,Payer Control,HCP,prod,CC-100,APP-1
`
	m, err := LoadMappingCSV(strings.NewReader(csv))
	if err != nil {
		t.Fatalf("LoadMappingCSV() error = %v", err)
	}
	if len(m) != 1 {
		t.Fatalf("len(m) = %d, want 1 (blank ID row skipped)", len(m))
	}
}

func TestLoadMappingCSV_RejectsWrongHeader(t *testing.T) {
	csv := `Account ID, Account name, Category, service-phase, cost-center, app-code
111111111111,Payer Control,HCP,prod,CC-100,APP-1
`
	if _, err := LoadMappingCSV(strings.NewReader(csv)); err == nil {
		t.Fatal("expected error for wrong header column name")
	}
}

func TestLoadMappingCSV_RejectsWrongColumnCount(t *testing.T) {
	csv := `Account ID, Account name, Service Name
111111111111,Payer Control,HCP
`
	if _, err := LoadMappingCSV(strings.NewReader(csv)); err == nil {
		t.Fatal("expected error for wrong column count")
	}
}

func TestLoadMappingCSV_RejectsRowWithWrongColumnCount(t *testing.T) {
	csv := `Account ID, Account name, Service Name, service-phase, cost-center, app-code
111111111111,Payer Control,HCP,prod,CC-100
`
	if _, err := LoadMappingCSV(strings.NewReader(csv)); err == nil {
		t.Fatal("expected error for data row with wrong column count")
	}
}

func TestLoadMappingCSV_EmptyHeaderOnly(t *testing.T) {
	csv := `Account ID, Account name, Service Name, service-phase, cost-center, app-code
`
	m, err := LoadMappingCSV(strings.NewReader(csv))
	if err != nil {
		t.Fatalf("LoadMappingCSV() error = %v", err)
	}
	if len(m) != 0 {
		t.Errorf("len(m) = %d, want 0", len(m))
	}
}

func TestLoadMappingCSV_RejectsUnreadableHeader(t *testing.T) {
	if _, err := LoadMappingCSV(strings.NewReader("")); err == nil {
		t.Fatal("expected error for empty input (no header)")
	}
}

func TestLoadMappingCSVFile_MissingFile(t *testing.T) {
	if _, err := LoadMappingCSVFile("/nonexistent/path/mapping.csv"); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestNormalizeAccountID(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"111111111111", "111111111111"},
		{" 111111111111 ", "111111111111"},
		{"0111111111", "111111111"},
		{"111-111-111", "111111111"},
		{"000000000000", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := NormalizeAccountID(c.in); got != c.want {
			t.Errorf("NormalizeAccountID(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestAccountTaxonomyMap_ServiceName_Fallback(t *testing.T) {
	m, err := LoadMappingCSV(strings.NewReader(validMappingCSV))
	if err != nil {
		t.Fatalf("LoadMappingCSV() error = %v", err)
	}
	if got := m.ServiceName("111111111111"); got != "HCP" {
		t.Errorf("ServiceName(mapped) = %q, want %q", got, "HCP")
	}
	if got := m.ServiceName("999999999999"); got != UnmappedServiceName {
		t.Errorf("ServiceName(unmapped) = %q, want %q", got, UnmappedServiceName)
	}
}

func TestAccountTaxonomyMap_AccountName_Fallback(t *testing.T) {
	m, err := LoadMappingCSV(strings.NewReader(validMappingCSV))
	if err != nil {
		t.Fatalf("LoadMappingCSV() error = %v", err)
	}
	if got := m.AccountName("111111111111"); got != "Payer Control" {
		t.Errorf("AccountName(mapped) = %q, want %q", got, "Payer Control")
	}
	if got := m.AccountName("999999999999"); got != "999999999999" {
		t.Errorf("AccountName(unmapped) = %q, want account ID itself", got)
	}
}

func TestAccountTaxonomyMap_ServiceName_NormalizesLookupInput(t *testing.T) {
	m, err := LoadMappingCSV(strings.NewReader(validMappingCSV))
	if err != nil {
		t.Fatalf("LoadMappingCSV() error = %v", err)
	}
	// CE's LINKED_ACCOUNT style value "022222222222" should still resolve
	// even though the map key is normalized to "22222222222".
	if got := m.ServiceName("022222222222"); got != "Tooling" {
		t.Errorf("ServiceName(zero-padded) = %q, want %q", got, "Tooling")
	}
}
