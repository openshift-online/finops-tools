package gmailauth

import (
	"os"
	"strings"
)

// DefaultGmailQuotaProject is the GCP project used for Gmail API quota/billing
// when FINOPS_GMAIL_QUOTA_PROJECT is unset.
const DefaultGmailQuotaProject = "hcmfinops"

// QuotaProject returns override when set, otherwise ResolveQuotaProject.
func QuotaProject(override string) string {
	if v := strings.TrimSpace(override); v != "" {
		return v
	}
	return ResolveQuotaProject()
}

// ResolveQuotaProject returns the GCP project ID used for Gmail API quota/billing.
// Priority: FINOPS_GMAIL_QUOTA_PROJECT, GOOGLE_CLOUD_QUOTA_PROJECT, DefaultGmailQuotaProject.
func ResolveQuotaProject() string {
	for _, key := range []string{"FINOPS_GMAIL_QUOTA_PROJECT", "GOOGLE_CLOUD_QUOTA_PROJECT"} {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v
		}
	}
	return DefaultGmailQuotaProject
}

// QuotaProjectHint explains how the Gmail API quota project is chosen.
func QuotaProjectHint() string {
	return "quota project defaults to " + DefaultGmailQuotaProject + " (set FINOPS_GMAIL_QUOTA_PROJECT to override); ADC file is not modified"
}
