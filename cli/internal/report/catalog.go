package report

import (
	"fmt"
	"strings"
)

const (
	// TemplateCosts is the AWS costs summary report template.
	TemplateCosts = "costs"
	// TemplateHCPHierarchy is the ROSA HCP SC → MC → Worker cluster hierarchy report template.
	TemplateHCPHierarchy = "hcp-hierarchy"
	// TemplateSavingsPlans is the Savings Plans coverage and utilization report template.
	TemplateSavingsPlans = "savings-plans"
	// TemplateCostAnomalies is the AWS Cost Anomaly Detection report template.
	TemplateCostAnomalies = "cost-anomalies"
	// FormatHTML is the HTML output format.
	FormatHTML = "html"
)

// TemplateInfo describes an available report template.
type TemplateInfo struct {
	Name        string
	Description string
	Formats     []string
}

// Templates returns all registered report templates.
func Templates() []TemplateInfo {
	return []TemplateInfo{
		{
			Name:        TemplateCosts,
			Description: "AWS net amortized cost: total, per linked account, per service, and daily trend",
			Formats:     []string{FormatHTML},
		},
		{
			Name:        TemplateHCPHierarchy,
			Description: "ROSA HCP SC → MC → Worker cluster hierarchy with AWS account attribution (requires Snowflake)",
			Formats:     []string{FormatHTML},
		},
		{
			Name:        TemplateSavingsPlans,
			Description: "Savings Plans coverage and utilization by month",
			Formats:     []string{FormatHTML},
		},
		{
			Name:        TemplateCostAnomalies,
			Description: "AWS Cost Anomaly Detection: detected anomalies with root cause breakdown",
			Formats:     []string{FormatHTML},
		},
	}
}

// ParseTemplate validates a report template name (positional argument to generate).
func ParseTemplate(s string) (string, error) {
	name := strings.ToLower(strings.TrimSpace(s))
	for _, t := range Templates() {
		if name == t.Name {
			return t.Name, nil
		}
	}
	return "", fmt.Errorf("unknown template %q (supported: %s)", s, templateNames())
}

// ParseFormat validates a --format flag value. Empty means html.
func ParseFormat(s string) (string, error) {
	format := strings.ToLower(strings.TrimSpace(s))
	if format == "" {
		return FormatHTML, nil
	}
	if format == FormatHTML {
		return FormatHTML, nil
	}
	return "", fmt.Errorf("unknown format %q (supported: html)", s)
}

func templateNames() string {
	templates := Templates()
	names := make([]string, len(templates))
	for i, t := range templates {
		names[i] = t.Name
	}
	return strings.Join(names, ", ")
}
