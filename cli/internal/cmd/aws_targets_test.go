package cmd

import "testing"

func TestValidateWorkers(t *testing.T) {
	if err := validateWorkers(1); err != nil {
		t.Fatalf("validateWorkers(1) = %v", err)
	}
	if err := validateWorkers(10); err != nil {
		t.Fatalf("validateWorkers(10) = %v", err)
	}
	if err := validateWorkers(0); err == nil {
		t.Fatal("expected error for workers=0")
	}
	if err := validateWorkers(-1); err == nil {
		t.Fatal("expected error for workers=-1")
	}
}
