package gmailauth

import (
	"testing"
)

func TestResolveQuotaProjectFromEnv(t *testing.T) {
	t.Setenv("FINOPS_GMAIL_QUOTA_PROJECT", "env-project")
	t.Setenv("GOOGLE_CLOUD_QUOTA_PROJECT", "other-project")
	if got := ResolveQuotaProject(); got != "env-project" {
		t.Fatalf("ResolveQuotaProject() = %q, want env-project", got)
	}
}

func TestResolveQuotaProjectDefault(t *testing.T) {
	t.Setenv("FINOPS_GMAIL_QUOTA_PROJECT", "")
	t.Setenv("GOOGLE_CLOUD_QUOTA_PROJECT", "")
	if got := ResolveQuotaProject(); got != DefaultGmailQuotaProject {
		t.Fatalf("ResolveQuotaProject() = %q, want %q", got, DefaultGmailQuotaProject)
	}
}

func TestQuotaProjectOverride(t *testing.T) {
	t.Setenv("FINOPS_GMAIL_QUOTA_PROJECT", "env-project")
	if got := QuotaProject("flag-project"); got != "flag-project" {
		t.Fatalf("QuotaProject() = %q, want flag-project", got)
	}
	if got := QuotaProject("  "); got != "env-project" {
		t.Fatalf("QuotaProject empty = %q, want env-project", got)
	}
}
