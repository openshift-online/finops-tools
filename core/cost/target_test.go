// target_test.go tests AccountTarget helpers (linked detection, credentials account ID).
package cost

import "testing"

func TestAccountTargetCredentialsAccountID(t *testing.T) {
	payer := AccountTarget{AccountID: "123456789012"}
	if got := payer.CredentialsAccountID(); got != "123456789012" {
		t.Fatalf("payer creds = %q", got)
	}
	if payer.IsLinked() {
		t.Fatal("payer should not be linked")
	}

	linked := AccountTarget{AccountID: "111111111111", PayerAccountID: "123456789012"}
	if got := linked.CredentialsAccountID(); got != "123456789012" {
		t.Fatalf("linked creds = %q", got)
	}
	if !linked.IsLinked() {
		t.Fatal("expected linked target")
	}
}

func TestAccountTargetScopeToAccount(t *testing.T) {
	payer := AccountTarget{AccountID: "123456789012"}
	if payer.ScopeToAccount() {
		t.Fatal("payer without ScopeAccountOnly should not scope to account")
	}

	payerScoped := AccountTarget{AccountID: "123456789012", ScopeAccountOnly: true}
	if !payerScoped.ScopeToAccount() {
		t.Fatal("payer with ScopeAccountOnly should scope to account")
	}

	selfPayer := AccountTarget{AccountID: "123456789012", PayerAccountID: "123456789012", ScopeAccountOnly: true}
	if !selfPayer.ScopeToAccount() {
		t.Fatal("self payer with ScopeAccountOnly should scope to account")
	}
	if selfPayer.IsLinked() {
		t.Fatal("self payer should not be linked")
	}
}

func TestFilterOverlappingTargets(t *testing.T) {
	independent := FilterOverlappingTargets([]AccountTarget{
		{AccountID: "123456789012"},
		{AccountID: "987654321098"},
	})
	if len(independent) != 2 {
		t.Fatalf("got %d targets, want 2", len(independent))
	}

	siblings := FilterOverlappingTargets([]AccountTarget{
		{AccountID: "111111111111", PayerAccountID: "123456789012"},
		{AccountID: "222222222222", PayerAccountID: "123456789012"},
	})
	if len(siblings) != 2 {
		t.Fatalf("got %d targets, want 2", len(siblings))
	}

	overlap := FilterOverlappingTargets([]AccountTarget{
		{AccountID: "123456789012"},
		{AccountID: "111111111111", PayerAccountID: "123456789012"},
	})
	if len(overlap) != 1 || overlap[0].AccountID != "123456789012" {
		t.Fatalf("got %+v, want payer only", overlap)
	}
}

func TestReportFetchProgress(t *testing.T) {
	rec := &recordingFetchProgress{}
	reportFetchProgress(rec, AccountTarget{AccountID: "111111111111", DisplayName: "Prod"}, 1, 100, GroupByNone)
	if len(rec.steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(rec.steps))
	}

	recMid := &recordingFetchProgress{}
	reportFetchProgress(recMid, AccountTarget{AccountID: "111111111111", DisplayName: "Prod"}, 24, 100, GroupByNone)
	if len(recMid.steps) != 0 {
		t.Fatalf("expected throttled step at 24, got %d", len(recMid.steps))
	}
	reportFetchProgress(recMid, AccountTarget{AccountID: "111111111111", DisplayName: "Prod"}, 25, 100, GroupByNone)
	if len(recMid.steps) != 1 {
		t.Fatalf("expected step at 25, got %d", len(recMid.steps))
	}

	recSingle := &recordingFetchProgress{}
	reportFetchProgress(recSingle, AccountTarget{AccountID: "111111111111"}, 1, 1, GroupByNone)
	if len(recSingle.steps) != 0 {
		t.Fatalf("expected no steps for single account, got %d", len(recSingle.steps))
	}
}

type recordingFetchProgress struct {
	steps []string
}

func (r *recordingFetchProgress) Step(message string) {
	r.steps = append(r.steps, message)
}
