package account

import "testing"

func TestValidateParentID(t *testing.T) {
	if err := ValidateParentID("ou-abcd-12345678"); err != nil {
		t.Fatalf("valid OU: %v", err)
	}
	if err := ValidateParentID("r-abcd"); err != nil {
		t.Fatalf("valid root: %v", err)
	}
	if err := ValidateParentID("ou-abcd-1234"); err == nil {
		t.Fatal("expected error for short OU suffix")
	}
	if err := ValidateParentID(""); err == nil {
		t.Fatal("expected error for empty")
	}
	if err := ValidateParentID("not-an-id"); err == nil {
		t.Fatal("expected error for garbage")
	}
}
