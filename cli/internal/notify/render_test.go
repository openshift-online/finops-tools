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
	msg := RenderAccountEmail(accountreview.AccountReport{
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
	})
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
	if !strings.Contains(msg.TextBody, "2 weeks") {
		t.Fatalf("text body missing deadline: %s", msg.TextBody)
	}
	if !strings.Contains(msg.TextBody, "reply to this email") {
		t.Fatalf("text body missing reply instruction")
	}
	actionIdx := strings.Index(msg.TextBody, "Action required")
	accountIdx := strings.Index(msg.TextBody, "Account:")
	if actionIdx < 0 || accountIdx < 0 || actionIdx > accountIdx {
		t.Fatalf("action required should appear before account details: action=%d account=%d", actionIdx, accountIdx)
	}
	if !strings.Contains(msg.HTMLBody, "Action required") {
		t.Fatalf("html body missing action required section")
	}
	htmlActionIdx := strings.Index(msg.HTMLBody, "Action required")
	htmlAccountIdx := strings.Index(msg.HTMLBody, "test-account")
	if htmlActionIdx < 0 || htmlAccountIdx < 0 || htmlActionIdx > htmlAccountIdx {
		t.Fatalf("html action required should appear before account details")
	}
	if !strings.Contains(msg.HTMLBody, "2026-01-15") {
		t.Fatalf("html body missing generated-at date: %s", msg.HTMLBody)
	}
	if !strings.Contains(msg.HTMLBody, "cluster-1") {
		t.Fatalf("html body missing RDS cluster: %s", msg.HTMLBody)
	}
}

func TestRenderAccountEmailKeepsMonthsWhenTopServicesFail(t *testing.T) {
	msg := RenderAccountEmail(accountreview.AccountReport{
		AccountID:   "111111111111",
		AccountName: "test-account",
		OwnerEmail:  "jdoe@redhat.com",
		MonthlyCosts: cost.AccountMonthlyCosts{
			Currency:         "USD",
			Months:           []cost.MonthlyCostPoint{{Month: "2026-01", Amount: 1234.5}},
			Total:            1234.5,
			TopServicesError: "service breakdown denied",
		},
	})
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
