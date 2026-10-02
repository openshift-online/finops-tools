package notify

import (
	"strings"
	"testing"
	"time"

	"github.com/openshift-online/finops-tools/core/accountreview"
	"github.com/openshift-online/finops-tools/core/cost"
	"github.com/openshift-online/finops-tools/core/inventory"
)

func TestRenderAccountEmail(t *testing.T) {
	msg := RenderAccountEmail(accountreview.DetailsFrom(accountreview.AccountReport{
		AccountID:   "111111111111",
		AccountName: "test-account",
		OwnerEmail:  "jdoe@redhat.com",
		GeneratedAt: time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC),
		MonthlyCosts: cost.AccountMonthlyCosts{
			Currency: "USD",
			Months:   []cost.MonthlyCostPoint{{Month: "2026-01", Amount: 1234.5}},
			Total:    1234.5,
		},
		Inventory: inventory.AccountInventory{
			RDSClusters: []inventory.RDSCluster{{
				ClusterID: "cluster-1",
				Engine:    "aurora-postgresql",
				Status:    "available",
				Region:    "us-east-1",
			}},
		},
	}))
	if msg.To != "jdoe@redhat.com" {
		t.Fatalf("To = %q", msg.To)
	}
	if !strings.Contains(msg.Subject, "Action required") {
		t.Fatalf("Subject = %q, want action required prefix", msg.Subject)
	}
	if !strings.Contains(msg.Subject, "111111111111") {
		t.Fatalf("Subject = %q", msg.Subject)
	}
	if !strings.Contains(msg.TextBody, "2026-01") {
		t.Fatalf("text body missing month: %s", msg.TextBody)
	}
	if !strings.Contains(msg.HTMLBody, "1,234.50") {
		t.Fatalf("html body missing formatted amount")
	}
	if !strings.Contains(msg.TextBody, "Action required") {
		t.Fatalf("text body missing action required section")
	}
	if !strings.Contains(msg.TextBody, "AWS account test-account (111111111111)") {
		t.Fatalf("text intro should name the account: %s", msg.TextBody)
	}
	if !strings.Contains(msg.TextBody, "29 January 2026") {
		t.Fatalf("text body missing reply-by date: %s", msg.TextBody)
	}
	if strings.Contains(msg.TextBody, "2 weeks") {
		t.Fatalf("text body should use a calendar date, not '2 weeks': %s", msg.TextBody)
	}
	if !strings.Contains(msg.TextBody, "reply to this email") {
		t.Fatalf("text body missing reply instruction")
	}
	if !strings.Contains(msg.TextBody, "Last listed month: USD 1,234.50") {
		t.Fatalf("text body missing cost summary: %s", msg.TextBody)
	}
	if !strings.Contains(msg.TextBody, "Period total: USD 1,234.50") {
		t.Fatalf("text body missing period total: %s", msg.TextBody)
	}
	if !strings.Contains(msg.TextBody, "Generated 2026-01-15 12:00 UTC") {
		t.Fatalf("text body missing generated-at date: %s", msg.TextBody)
	}
	if !strings.Contains(msg.TextBody, "Red Hat Hybrid Platform FinOps") {
		t.Fatalf("text body missing footer: %s", msg.TextBody)
	}
	if strings.Contains(msg.TextBody, "finops-tools") || strings.Contains(msg.HTMLBody, "finops-tools") {
		t.Fatalf("footer should not mention finops-tools")
	}
	if strings.Contains(msg.TextBody, "Owner: jdoe@redhat.com") {
		t.Fatalf("resolved owner email should not be repeated in the body: %s", msg.TextBody)
	}
	actionIdx := strings.Index(msg.TextBody, "Action required")
	accountIdx := strings.Index(msg.TextBody, "Account:")
	if actionIdx < 0 || accountIdx < 0 || actionIdx > accountIdx {
		t.Fatalf("action required should appear before account details: action=%d account=%d", actionIdx, accountIdx)
	}
	if !strings.Contains(msg.HTMLBody, "Action required") {
		t.Fatalf("html body missing action required section")
	}
	if !strings.Contains(msg.HTMLBody, "<h3>Monthly costs (net amortized)</h3>") {
		t.Fatalf("html monthly costs should be h3: %s", msg.HTMLBody)
	}
	htmlActionIdx := strings.Index(msg.HTMLBody, "Action required")
	htmlAccountIdx := strings.Index(msg.HTMLBody, "test-account")
	if htmlActionIdx < 0 || htmlAccountIdx < 0 || htmlActionIdx > htmlAccountIdx {
		t.Fatalf("html action required should appear before account details")
	}
	if !strings.Contains(msg.HTMLBody, "2026-01-15") {
		t.Fatalf("html body missing generated-at date: %s", msg.HTMLBody)
	}
	if !strings.Contains(msg.HTMLBody, "<h3>Resources</h3>") {
		t.Fatalf("html resources should be h3: %s", msg.HTMLBody)
	}
	if !strings.Contains(msg.HTMLBody, "<h4") || !strings.Contains(msg.HTMLBody, "RDS clusters") {
		t.Fatalf("html resource types should be h4: %s", msg.HTMLBody)
	}
	if strings.Contains(msg.HTMLBody, "<h3>RDS clusters") {
		t.Fatalf("html resource types should not be h3: %s", msg.HTMLBody)
	}
	if !strings.Contains(msg.TextBody, "None found (scanned):") {
		t.Fatalf("text should list scanned-empty types: %s", msg.TextBody)
	}
	if !strings.Contains(msg.TextBody, "EC2 instances") {
		t.Fatalf("empty EC2 type should appear in none-found line: %s", msg.TextBody)
	}
	if strings.Contains(msg.HTMLBody, "<h3>EC2 instances") {
		t.Fatalf("empty EC2 should not be an HTML table heading: %s", msg.HTMLBody)
	}
}

func TestRenderAccountEmailEscapesHTML(t *testing.T) {
	msg := RenderAccountEmail(accountreview.AccountDetails{
		AccountID:   "111111111111",
		AccountName: `<script>alert("x")</script>`,
		GeneratedAt: time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC),
	})
	if strings.Contains(msg.HTMLBody, "<script>") {
		t.Fatalf("html should escape account name: %s", msg.HTMLBody)
	}
	if !strings.Contains(msg.HTMLBody, "&lt;script&gt;") {
		t.Fatalf("expected escaped account name: %s", msg.HTMLBody)
	}
}

func TestRenderAccountEmailNoMonthlyData(t *testing.T) {
	msg := RenderAccountEmail(accountreview.AccountDetails{
		AccountID:   "111111111111",
		AccountName: "test-account",
		GeneratedAt: time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC),
	})
	if !strings.Contains(msg.TextBody, "(no data)") {
		t.Fatalf("text missing no-data: %s", msg.TextBody)
	}
	if !strings.Contains(msg.HTMLBody, "(no data)") {
		t.Fatalf("html missing no-data: %s", msg.HTMLBody)
	}
	if strings.Contains(msg.HTMLBody, ">Month</th>") {
		t.Fatalf("html should not render an empty month table: %s", msg.HTMLBody)
	}
}

func TestRenderAccountEmailKeepsMonthsWhenTopServicesFail(t *testing.T) {
	msg := RenderAccountEmail(accountreview.DetailsFrom(accountreview.AccountReport{
		AccountID:   "111111111111",
		AccountName: "test-account",
		OwnerEmail:  "jdoe@redhat.com",
		MonthlyCosts: cost.AccountMonthlyCosts{
			Currency:         "USD",
			Months:           []cost.MonthlyCostPoint{{Month: "2026-01", Amount: 1234.5}},
			Total:            1234.5,
			TopServicesError: "service breakdown denied",
		},
	}))
	if !strings.Contains(msg.TextBody, "2026-01") {
		t.Fatalf("text body missing month: %s", msg.TextBody)
	}
	if !strings.Contains(msg.TextBody, "unavailable: service breakdown denied") {
		t.Fatalf("text body missing top services error: %s", msg.TextBody)
	}
	if !strings.Contains(msg.HTMLBody, "1,234.50") {
		t.Fatalf("html body missing monthly amount")
	}
	if !strings.Contains(msg.HTMLBody, "service breakdown denied") {
		t.Fatalf("html body missing top services error")
	}
}

func TestRenderAccountEmailListsEmptyInventory(t *testing.T) {
	msg := RenderAccountEmail(accountreview.AccountDetails{
		AccountID:   "111111111111",
		AccountName: "test-account",
		GeneratedAt: time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC),
		ResourceCounts: accountreview.ResourceCounts{
			VPCs: 3,
		},
	})
	if !strings.Contains(msg.TextBody, "VPCs: 3") {
		t.Fatalf("nonzero count should be present: %s", msg.TextBody)
	}
	if !strings.Contains(msg.HTMLBody, "VPCs: 3") {
		t.Fatalf("html missing VPC count: %s", msg.HTMLBody)
	}
	noneIdx := strings.Index(msg.TextBody, "None found (scanned):")
	vpcIdx := strings.Index(msg.TextBody, "VPCs: 3")
	if noneIdx < 0 || vpcIdx < 0 || vpcIdx > noneIdx {
		t.Fatalf("none-found line should follow found counts: vpc=%d none=%d\n%s", vpcIdx, noneIdx, msg.TextBody)
	}
	noneLine := msg.TextBody[noneIdx:]
	for _, want := range []string{"EC2 instances", "RDS instances", "Route53 hosted zones", "Unattached EBS volumes"} {
		if !strings.Contains(noneLine, want) {
			t.Fatalf("none-found line missing %q: %s", want, noneLine)
		}
	}
	if strings.Contains(msg.HTMLBody, "<h3>EC2 instances") {
		t.Fatalf("empty EC2 should not be an HTML table heading")
	}
	if strings.Contains(msg.TextBody, "EC2 instances: 0") {
		t.Fatalf("empty types should not use zero count rows: %s", msg.TextBody)
	}
}

func TestRenderAccountEmailEmptyResources(t *testing.T) {
	msg := RenderAccountEmail(accountreview.AccountDetails{
		AccountID:   "111111111111",
		AccountName: "test-account",
		GeneratedAt: time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC),
	})
	if !strings.Contains(msg.TextBody, "Resources:") {
		t.Fatalf("resources section should remain: %s", msg.TextBody)
	}
	if !strings.Contains(msg.TextBody, "None found (scanned):") {
		t.Fatalf("text missing none-found line: %s", msg.TextBody)
	}
	if !strings.Contains(msg.HTMLBody, "None found (scanned):") {
		t.Fatalf("html missing none-found line: %s", msg.HTMLBody)
	}
	for _, want := range []string{"EC2 instances", "VPCs"} {
		if !strings.Contains(msg.TextBody, want) {
			t.Fatalf("empty inventory should still name scanned type %q: %s", want, msg.TextBody)
		}
	}
}

func TestRenderAccountEmailShowsOwnerError(t *testing.T) {
	msg := RenderAccountEmail(accountreview.AccountDetails{
		AccountID:   "111111111111",
		AccountName: "test-account",
		OwnerError:  "owner tag is empty",
		GeneratedAt: time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC),
	})
	if !strings.Contains(msg.TextBody, "Owner: owner tag is empty") {
		t.Fatalf("text should show owner error: %s", msg.TextBody)
	}
	if !strings.Contains(msg.HTMLBody, "owner tag is empty") {
		t.Fatalf("html should show owner error: %s", msg.HTMLBody)
	}
}

func TestRenderAccountEmailOmitsInventoryWarnings(t *testing.T) {
	msg := RenderAccountEmail(accountreview.AccountDetails{
		AccountID:      "111111111111",
		AccountName:    "test-account",
		GeneratedAt:    time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC),
		InventoryError: "me-south-1: scan timed out after 45s",
		ResourceCounts: accountreview.ResourceCounts{VPCs: 3},
	})
	if strings.Contains(msg.TextBody, "Inventory warnings") || strings.Contains(msg.HTMLBody, "Inventory warnings") {
		t.Fatalf("email should omit inventory warnings:\ntext=%s\nhtml=%s", msg.TextBody, msg.HTMLBody)
	}
	if strings.Contains(msg.TextBody, "timed out") || strings.Contains(msg.HTMLBody, "timed out") {
		t.Fatalf("email should omit scan timeout details:\ntext=%s", msg.TextBody)
	}
	if strings.Contains(msg.TextBody, "None found (scanned):") || strings.Contains(msg.HTMLBody, "None found (scanned):") {
		t.Fatalf("incomplete scan must not claim types were empty:\ntext=%s", msg.TextBody)
	}
	if !strings.Contains(msg.TextBody, "VPCs: 3") {
		t.Fatalf("found counts should still appear: %s", msg.TextBody)
	}
	if !strings.Contains(msg.TextBody, incompleteInventoryNote) {
		t.Fatalf("text missing incomplete-inventory note: %s", msg.TextBody)
	}
	if !strings.Contains(msg.HTMLBody, incompleteInventoryNote) {
		t.Fatalf("html missing incomplete-inventory note: %s", msg.HTMLBody)
	}
}

func TestRenderOwnerGroupEmail(t *testing.T) {
	generated := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	details := []accountreview.AccountDetails{
		{
			AccountID:   "111111111111",
			AccountName: "a",
			OwnerEmail:  "jdoe@redhat.com",
			GeneratedAt: generated,
			MonthlyCosts: accountreview.MonthlyCostsDetails{
				Currency: "USD",
				Months:   []cost.MonthlyCostPoint{{Month: "2026-01", Amount: 10}},
				Total:    10,
			},
		},
		{
			AccountID:   "222222222222",
			AccountName: "b",
			OwnerEmail:  "jdoe@redhat.com",
			GeneratedAt: generated,
			MonthlyCosts: accountreview.MonthlyCostsDetails{
				Currency: "USD",
				Months:   []cost.MonthlyCostPoint{{Month: "2026-01", Amount: 20}},
				Total:    20,
			},
		},
	}
	msg := RenderOwnerGroupEmail("jdoe@redhat.com", details)
	if msg.To != "jdoe@redhat.com" {
		t.Fatalf("To = %q", msg.To)
	}
	if !strings.Contains(msg.Subject, "2 AWS accounts") {
		t.Fatalf("Subject = %q", msg.Subject)
	}
	if strings.Contains(msg.Subject, "account(s)") {
		t.Fatalf("Subject should not use account(s): %q", msg.Subject)
	}
	if !strings.Contains(msg.TextBody, "111111111111") || !strings.Contains(msg.TextBody, "222222222222") {
		t.Fatalf("text missing accounts: %s", msg.TextBody)
	}
	if !strings.Contains(msg.TextBody, "these AWS accounts") {
		t.Fatalf("deadline should be plural: %s", msg.TextBody)
	}
	if !strings.Contains(msg.TextBody, "the 2 AWS accounts below") {
		t.Fatalf("intro should mention account count: %s", msg.TextBody)
	}
	if !strings.Contains(msg.TextBody, "Accounts in this review") {
		t.Fatalf("text missing overview: %s", msg.TextBody)
	}
	accountIdx := strings.Index(msg.TextBody, "Account:")
	id1 := strings.Index(msg.TextBody, "111111111111")
	id2 := strings.Index(msg.TextBody, "222222222222")
	if accountIdx < 0 || id1 < 0 || id2 < 0 || id1 > accountIdx || id2 > accountIdx {
		t.Fatalf("overview should mention both account IDs before the first Account: heading: account=%d id1=%d id2=%d", accountIdx, id1, id2)
	}
	htmlHeading := strings.Index(msg.HTMLBody, "<h2>a (111111111111)</h2>")
	htmlID1 := strings.Index(msg.HTMLBody, "111111111111")
	htmlID2 := strings.Index(msg.HTMLBody, "222222222222")
	if htmlHeading < 0 || htmlID1 < 0 || htmlID2 < 0 || htmlID1 > htmlHeading || htmlID2 > htmlHeading {
		t.Fatalf("html overview should mention both account IDs before the first account h2: heading=%d id1=%d id2=%d", htmlHeading, htmlID1, htmlID2)
	}
	if !strings.Contains(msg.HTMLBody, "Accounts in this review") {
		t.Fatalf("html missing overview heading")
	}
}
