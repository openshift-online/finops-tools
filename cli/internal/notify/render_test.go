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
	}), RenderOptions{})
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
	if strings.Contains(msg.TextBody, "29 January 2026") {
		t.Fatalf("default text body should not render a reply-by date: %s", msg.TextBody)
	}
	if strings.Contains(msg.TextBody, "If we do not hear from you by") {
		t.Fatalf("default text body should not state a deadline: %s", msg.TextBody)
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
	}, RenderOptions{})
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
	}, RenderOptions{})
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
	}), RenderOptions{})
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
	}, RenderOptions{})
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
	}, RenderOptions{})
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
	}, RenderOptions{})
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
	}, RenderOptions{})
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
	for _, body := range []string{msg.TextBody, msg.HTMLBody} {
		warnIdx := strings.Index(body, "Warnings")
		vpcIdx := strings.Index(body, "VPCs")
		if warnIdx < 0 || vpcIdx < 0 || warnIdx < vpcIdx {
			t.Fatalf("expected Warnings after resource counts:\n%s", body)
		}
	}
}

func TestRenderAccountEmailIncludesOpenShiftClusters(t *testing.T) {
	msg := RenderAccountEmail(accountreview.AccountDetails{
		AccountID:      "111111111111",
		AccountName:    "test-account",
		GeneratedAt:    time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC),
		ResourceCounts: accountreview.ResourceCounts{VPCs: 3},
		OpenShiftClusters: []accountreview.OpenShiftClusterDetail{{
			Environment: "Stage",
			Name:        "stage-cluster",
			ClusterID:   "ocm-stage-1",
			ProductType: "OSD",
			State:       "ready",
			Region:      "us-west-2",
		}},
	}, RenderOptions{})
	for _, body := range []string{msg.TextBody, msg.HTMLBody} {
		if !strings.Contains(body, "OpenShift clusters") || !strings.Contains(body, "stage-cluster") {
			t.Fatalf("email missing OpenShift clusters:\n%s", body)
		}
		resIdx := strings.Index(body, "Resources")
		otherIdx := strings.Index(body, "Other resources")
		ocpIdx := strings.Index(body, "OpenShift clusters")
		vpcIdx := strings.Index(body, "VPCs")
		if resIdx < 0 || otherIdx < 0 || ocpIdx < 0 || vpcIdx < 0 ||
			resIdx >= otherIdx || otherIdx >= vpcIdx || vpcIdx >= ocpIdx {
			t.Fatalf("expected Resources → Other resources → VPCs → OpenShift:\n%s", body)
		}
	}
}

func TestRenderAccountEmailOmitsOpenShiftLookupDetails(t *testing.T) {
	msg := RenderAccountEmail(accountreview.AccountDetails{
		AccountID:              "111111111111",
		AccountName:            "test-account",
		GeneratedAt:            time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC),
		OpenShiftClustersError: "token expired for alias rhprod",
	}, RenderOptions{})
	if strings.Contains(msg.TextBody, "token expired") || strings.Contains(msg.HTMLBody, "token expired") {
		t.Fatalf("email should omit raw OpenShift lookup errors:\n%s", msg.TextBody)
	}
	if !strings.Contains(msg.TextBody, incompleteOpenShiftNote) {
		t.Fatalf("text missing incomplete-openshift note: %s", msg.TextBody)
	}
	if !strings.Contains(msg.HTMLBody, incompleteOpenShiftNote) {
		t.Fatalf("html missing incomplete-openshift note: %s", msg.HTMLBody)
	}
	if strings.Count(msg.TextBody, incompleteOpenShiftNote) != 1 {
		t.Fatalf("text should print incomplete-openshift note once under Warnings:\n%s", msg.TextBody)
	}
	for _, body := range []string{msg.TextBody, msg.HTMLBody} {
		if strings.Contains(body, "OpenShift clusters (0)") {
			t.Fatalf("failed lookup must not imply zero clusters:\n%s", body)
		}
		if !strings.Contains(body, "OpenShift clusters (unavailable)") {
			t.Fatalf("failed lookup missing unavailable title:\n%s", body)
		}
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
	msg := RenderOwnerGroupEmail("jdoe@redhat.com", details, RenderOptions{})
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
	if strings.Contains(msg.TextBody, "these AWS accounts") {
		t.Fatalf("default group email should not state a deadline: %s", msg.TextBody)
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

func TestRenderAccountEmailNoDeadlineByDefault(t *testing.T) {
	msg := RenderAccountEmail(accountreview.AccountDetails{
		AccountID:   "111111111111",
		AccountName: "test-account",
		GeneratedAt: time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC),
	}, RenderOptions{})
	for _, body := range []string{msg.TextBody, msg.HTMLBody} {
		if strings.Contains(body, "If we do not hear from you by") {
			t.Fatalf("default email should not state a deadline:\n%s", body)
		}
		if strings.Contains(body, "29 January 2026") {
			t.Fatalf("default email should not render a reply-by date:\n%s", body)
		}
	}
	if !strings.Contains(msg.TextBody, "- Keep the account\n- Delete the account\n- Request modifications (describe what you need)") {
		t.Fatalf("default reply options should remain: %s", msg.TextBody)
	}
}

func TestRenderAccountEmailDeadlineOptIn(t *testing.T) {
	msg := RenderAccountEmail(accountreview.AccountDetails{
		AccountID:   "111111111111",
		AccountName: "test-account",
		GeneratedAt: time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC),
	}, RenderOptions{
		DeadlineText: "If we do not hear from you by {date}, we will proceed with deletion.",
	})
	want := "If we do not hear from you by 29 January 2026, we will proceed with deletion."
	if !strings.Contains(msg.TextBody, want) {
		t.Fatalf("text missing deadline sentence: %s", msg.TextBody)
	}
	if !strings.Contains(msg.HTMLBody, `class="deadline"`) || !strings.Contains(msg.HTMLBody, `font-weight:600`) || !strings.Contains(msg.HTMLBody, want) {
		t.Fatalf("html missing deadline sentence: %s", msg.HTMLBody)
	}
}

func TestRenderAccountEmailDeadlineReplyByDays(t *testing.T) {
	msg := RenderAccountEmail(accountreview.AccountDetails{
		AccountID:   "111111111111",
		AccountName: "test-account",
		GeneratedAt: time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC),
	}, RenderOptions{
		DeadlineText: "If we do not hear from you by {date}, we will proceed with deletion.",
		ReplyByDays:  21,
	})
	if !strings.Contains(msg.TextBody, "5 February 2026") {
		t.Fatalf("deadline should use the configured reply window: %s", msg.TextBody)
	}
	if strings.Contains(msg.TextBody, "29 January 2026") {
		t.Fatalf("deadline should not use the default reply window: %s", msg.TextBody)
	}
}

func TestRenderOwnerGroupEmailDeadline(t *testing.T) {
	generated := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	msg := RenderOwnerGroupEmail("jdoe@redhat.com", []accountreview.AccountDetails{
		{AccountID: "111111111111", AccountName: "a", GeneratedAt: generated},
		{AccountID: "222222222222", AccountName: "b", GeneratedAt: generated},
	}, RenderOptions{
		DeadlineText: "If we do not hear from you by {date}, we will proceed with deletion.",
	})
	if !strings.Contains(msg.TextBody, "If we do not hear from you by 29 January 2026, we will proceed with deletion.") {
		t.Fatalf("group email missing deadline sentence: %s", msg.TextBody)
	}
}

func TestRenderAccountEmailCustomSubjectPrefixAndTitle(t *testing.T) {
	msg := RenderAccountEmail(accountreview.AccountDetails{
		AccountID:   "111111111111",
		AccountName: "test-account",
		GeneratedAt: time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC),
	}, RenderOptions{SubjectPrefix: "🔔 Review: ", ActionTitle: "Your response is needed"})
	if msg.Subject != "🔔 Review: AWS account review: test-account (111111111111)" {
		t.Fatalf("Subject = %q", msg.Subject)
	}
	if !strings.Contains(msg.TextBody, "Your response is needed\n") {
		t.Fatalf("text missing custom section title: %s", msg.TextBody)
	}
	if !strings.Contains(msg.HTMLBody, "<h2>Your response is needed</h2>") {
		t.Fatalf("html missing custom section title: %s", msg.HTMLBody)
	}
}

func TestRenderOwnerGroupEmailCustomSubjectPrefix(t *testing.T) {
	msg := RenderOwnerGroupEmail("jdoe@redhat.com", []accountreview.AccountDetails{
		{AccountID: "111111111111", AccountName: "a"},
		{AccountID: "222222222222", AccountName: "b"},
	}, RenderOptions{SubjectPrefix: "🔔 Review: "})
	if msg.Subject != "🔔 Review: AWS account review: 2 AWS accounts" {
		t.Fatalf("Subject = %q", msg.Subject)
	}
}

func TestRenderAccountEmailCustomIntro(t *testing.T) {
	msg := RenderAccountEmail(accountreview.AccountDetails{
		AccountID:   "111111111111",
		AccountName: "test-account",
		GeneratedAt: time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC),
	}, RenderOptions{Intro: "Please review {account} (ID {account_id}); {count} account in this email."})
	want := "Please review AWS account test-account (111111111111) (ID 111111111111); 1 account in this email."
	if !strings.Contains(msg.TextBody, want) {
		t.Fatalf("text missing expanded intro:\nwant: %s\n got: %s", want, msg.TextBody)
	}
	if !strings.Contains(msg.HTMLBody, want) {
		t.Fatalf("html missing expanded intro: %s", msg.HTMLBody)
	}
	if strings.Contains(msg.TextBody, "We are reviewing AWS account") {
		t.Fatalf("custom intro should replace the built-in intro: %s", msg.TextBody)
	}
}

func TestRenderOwnerGroupEmailCustomIntroPlaceholders(t *testing.T) {
	msg := RenderOwnerGroupEmail("jdoe@redhat.com", []accountreview.AccountDetails{
		{AccountID: "111111111111", AccountName: "a"},
		{AccountID: "222222222222", AccountName: "b"},
	}, RenderOptions{Intro: "We are reviewing {account} ({count} total)."})
	if !strings.Contains(msg.TextBody, "We are reviewing the 2 AWS accounts below (2 total).") {
		t.Fatalf("group intro placeholders expanded wrong: %s", msg.TextBody)
	}
}

func TestRenderAccountEmailCustomOptions(t *testing.T) {
	msg := RenderAccountEmail(accountreview.AccountDetails{
		AccountID:   "111111111111",
		AccountName: "test-account",
		GeneratedAt: time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC),
	}, RenderOptions{Options: []string{"Keep it", "Archive it"}})
	if !strings.Contains(msg.TextBody, "- Keep it\n- Archive it") {
		t.Fatalf("text missing custom options: %s", msg.TextBody)
	}
	if !strings.Contains(msg.HTMLBody, "<li>Keep it</li><li>Archive it</li>") {
		t.Fatalf("html missing custom options: %s", msg.HTMLBody)
	}
	if strings.Contains(msg.TextBody, "Keep the account") {
		t.Fatalf("custom options should replace the defaults: %s", msg.TextBody)
	}
}

func TestRenderAccountEmailEscapesTemplateTextHTML(t *testing.T) {
	msg := RenderAccountEmail(accountreview.AccountDetails{
		AccountID:   "111111111111",
		AccountName: "test-account",
		GeneratedAt: time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC),
	}, RenderOptions{
		ActionTitle:  `<script>title</script>`,
		Intro:        "intro & more",
		Options:      []string{"<b>opt</b>"},
		DeadlineText: "deadline <by> {date}",
	})
	if strings.Contains(msg.HTMLBody, "<script>") || strings.Contains(msg.HTMLBody, "<b>opt</b>") || strings.Contains(msg.HTMLBody, "deadline <by>") {
		t.Fatalf("html should escape template text: %s", msg.HTMLBody)
	}
	if !strings.Contains(msg.HTMLBody, "&lt;b&gt;opt&lt;/b&gt;") {
		t.Fatalf("html should contain the escaped option: %s", msg.HTMLBody)
	}
	if !strings.Contains(msg.HTMLBody, "intro &amp; more") {
		t.Fatalf("html should escape the intro ampersand: %s", msg.HTMLBody)
	}
}

func TestValidateIntroTemplate(t *testing.T) {
	if err := ValidateIntroTemplate(""); err != nil {
		t.Fatalf("empty intro: %v", err)
	}
	if err := ValidateIntroTemplate("Review {account} ({account_id}), {count} total."); err != nil {
		t.Fatalf("valid intro: %v", err)
	}
	err := ValidateIntroTemplate("Review {acount}.")
	if err == nil || !strings.Contains(err.Error(), "{acount}") {
		t.Fatalf("unknown placeholder should fail: %v", err)
	}
}

func TestValidateDeadlineTemplate(t *testing.T) {
	if err := ValidateDeadlineTemplate(""); err != nil {
		t.Fatalf("empty deadline: %v", err)
	}
	if err := ValidateDeadlineTemplate("Reply by {date}."); err != nil {
		t.Fatalf("valid deadline: %v", err)
	}
	err := ValidateDeadlineTemplate("Reply by {data}.")
	if err == nil || !strings.Contains(err.Error(), "{data}") {
		t.Fatalf("unknown placeholder should fail: %v", err)
	}
	err = ValidateDeadlineTemplate("Reply by {accounts}.")
	if err == nil || !strings.Contains(err.Error(), "{accounts}") {
		t.Fatalf("{accounts} should be rejected: %v", err)
	}
}

func TestRenderAccountEmailDefaultActionHTML(t *testing.T) {
	msg := RenderAccountEmail(accountreview.AccountDetails{
		AccountID:   "111111111111",
		AccountName: "test-account",
		GeneratedAt: time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC),
	}, RenderOptions{})
	want := `<h2>Action required</h2><p>We are reviewing AWS account test-account (111111111111), which may no longer be needed. Based on the information below, please reply to this email and tell us whether you want to:</p><ul><li>Keep the account</li><li>Delete the account</li><li>Request modifications (describe what you need)</li></ul></section>`
	if !strings.Contains(msg.HTMLBody, want) {
		t.Fatalf("default action HTML changed:\n%s", msg.HTMLBody)
	}
	if strings.Contains(msg.HTMLBody, `class="deadline"`) {
		t.Fatalf("default email should not include a deadline: %s", msg.HTMLBody)
	}
}

func TestRenderAccountEmailSubjectPrefixIsPlain(t *testing.T) {
	msg := RenderAccountEmail(accountreview.AccountDetails{
		AccountID:   "111111111111",
		AccountName: "test-account",
		GeneratedAt: time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC),
	}, RenderOptions{SubjectPrefix: "**Review**: "})
	if msg.Subject != "**Review**: AWS account review: test-account (111111111111)" {
		t.Fatalf("Subject = %q", msg.Subject)
	}
}

func TestRenderAccountEmailMarkdownTemplate(t *testing.T) {
	msg := RenderAccountEmail(accountreview.AccountDetails{
		AccountID:   "111111111111",
		AccountName: "test-account",
		GeneratedAt: time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC),
	}, RenderOptions{
		ActionTitle:  "*Please respond*",
		Intro:        "Please review **{account}**. See the [runbook](https://example.com/docs).",
		Options:      []string{"**Keep** the account"},
		DeadlineText: "Reply by **{date}**.",
	})
	if !strings.Contains(msg.HTMLBody, "<h2><em>Please respond</em></h2>") {
		t.Fatalf("title html: %s", msg.HTMLBody)
	}
	if !strings.Contains(msg.TextBody, "Please respond\n") || strings.Contains(msg.TextBody, "*Please respond*") {
		t.Fatalf("title text: %s", msg.TextBody)
	}
	account := "AWS account test-account (111111111111)"
	if !strings.Contains(msg.HTMLBody, "<strong>"+account+"</strong>") {
		t.Fatalf("intro html: %s", msg.HTMLBody)
	}
	if !strings.Contains(msg.HTMLBody, `<a style="color:#0969da" href="https://example.com/docs">runbook</a>`) {
		t.Fatalf("link html: %s", msg.HTMLBody)
	}
	if !strings.Contains(msg.TextBody, "Please review "+account+". See the runbook (https://example.com/docs).") {
		t.Fatalf("intro text: %s", msg.TextBody)
	}
	if !strings.Contains(msg.HTMLBody, "<li><strong>Keep</strong> the account</li>") {
		t.Fatalf("option html: %s", msg.HTMLBody)
	}
	if !strings.Contains(msg.TextBody, "- Keep the account") {
		t.Fatalf("option text: %s", msg.TextBody)
	}
	if !strings.Contains(msg.HTMLBody, "<strong>29 January 2026</strong>") {
		t.Fatalf("deadline html: %s", msg.HTMLBody)
	}
	if !strings.Contains(msg.TextBody, "Reply by 29 January 2026.") {
		t.Fatalf("deadline text: %s", msg.TextBody)
	}
	if strings.Contains(msg.TextBody, "**") {
		t.Fatalf("text body should drop emphasis markers: %s", msg.TextBody)
	}
}

func TestRenderAccountEmailMarkdownPlaceholderIsLiteral(t *testing.T) {
	msg := RenderAccountEmail(accountreview.AccountDetails{
		AccountID:   "111111111111",
		AccountName: "*prod* <x>",
		GeneratedAt: time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC),
	}, RenderOptions{Intro: "Review **{account}**."})
	if strings.Contains(msg.HTMLBody, "<em>") || strings.Contains(msg.HTMLBody, "<x>") {
		t.Fatalf("account name should not be parsed as markup: %s", msg.HTMLBody)
	}
	if !strings.Contains(msg.HTMLBody, "<strong>AWS account *prod* &lt;x&gt; (111111111111)</strong>") {
		t.Fatalf("intro html: %s", msg.HTMLBody)
	}
	if !strings.Contains(msg.TextBody, "AWS account *prod* <x> (111111111111)") {
		t.Fatalf("intro text: %s", msg.TextBody)
	}
}

func TestRenderAccountEmailMarkdownNewlineAndList(t *testing.T) {
	msg := RenderAccountEmail(accountreview.AccountDetails{
		AccountID:   "111111111111",
		AccountName: "test-account",
		GeneratedAt: time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC),
	}, RenderOptions{Intro: "Line one\nLine two\n\n- Alpha\n- Beta"})
	if !strings.Contains(msg.HTMLBody, "Line one<br>") || !strings.Contains(msg.HTMLBody, "<ul>") || !strings.Contains(msg.HTMLBody, "<li>Alpha</li>") {
		t.Fatalf("html: %s", msg.HTMLBody)
	}
	if !strings.Contains(msg.TextBody, "Line one\nLine two") || !strings.Contains(msg.TextBody, "- Alpha\n- Beta") {
		t.Fatalf("text: %s", msg.TextBody)
	}
}
