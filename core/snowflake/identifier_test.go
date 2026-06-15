package snowflake

import "testing"

func TestValidateQualifiedIdentifier_AcceptsValid(t *testing.T) {
	for _, name := range []string{
		"TABLE",
		"_private",
		"HCMFINOPS_DB.MARTS.OCM_CLOUDABILITY_MAPPING",
		"MY_DB.SCHEMA.TABLE",
	} {
		if err := ValidateQualifiedIdentifier(name, 3); err != nil {
			t.Errorf("ValidateQualifiedIdentifier(%q) = %v, want nil", name, err)
		}
	}
}

func TestValidateQualifiedIdentifier_RejectsInvalid(t *testing.T) {
	tests := []struct {
		name string
		id   string
	}{
		{"empty", ""},
		{"sql injection", "FOO; DROP TABLE bar"},
		{"comment", "FOO--comment"},
		{"space", "FOO BAR"},
		{"leading dot", ".SCHEMA.TABLE"},
		{"empty segment", "DB..TABLE"},
		{"too many parts", "A.B.C.D"},
		{"starts with digit", "1TABLE"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateQualifiedIdentifier(tc.id, 3); err == nil {
				t.Fatalf("ValidateQualifiedIdentifier(%q) = nil, want error", tc.id)
			}
		})
	}
}
