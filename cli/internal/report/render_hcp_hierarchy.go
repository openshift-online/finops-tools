package report

import (
	"fmt"
	"html/template"
	"io"
	"sync"

	corehcp "github.com/openshift-online/finops-tools/core/hcphierarchy"
)

const hcpHierarchyTemplate = "hcp-hierarchy"

var (
	hcpTplOnce sync.Once
	hcpTpl     *template.Template
	hcpTplErr  error
)

func hcpHierarchyTemplateCompiled() (*template.Template, error) {
	hcpTplOnce.Do(func() {
		hcpTpl, hcpTplErr = template.ParseFS(templateFS,
			"templates/layout.html",
			"templates/hcp-hierarchy.html",
		)
	})
	return hcpTpl, hcpTplErr
}

// HCPHierarchyRowView is one hierarchy table row for templates.
type HCPHierarchyRowView struct {
	CustomerClusterName string
	CustomerClusterID   string
	OrganizationName    string
	Classification      string
	CustomerState       string
	CustomerStateClass  string
	MCAccountID         string
	MCName              string
	MCRegion            string
	MCStatus            string
	MCStatusClass       string
	SCAccountID         string
	SCName              string
	SCRegion            string
	SCStatus            string
	SCStatusClass       string
	HierarchyComplete   bool
	BillingModel        string
}

// HCPHierarchyReportView is the template context for hcp-hierarchy.html.
type HCPHierarchyReportView struct {
	GeneratedAt    string
	MartView       string
	WorkerCount    int
	MCAccountCount int
	SCAccountCount int
	CompleteCount  int
	Rows           []HCPHierarchyRowView
}

// NewHCPHierarchyReportView maps a core hcphierarchy.Report for HTML rendering.
func NewHCPHierarchyReportView(r corehcp.Report) HCPHierarchyReportView {
	mcAccounts := uniqueAccountIDs(r.Rows, func(row corehcp.HierarchyRow) string { return row.MCAccountID })
	scAccounts := uniqueAccountIDs(r.Rows, func(row corehcp.HierarchyRow) string { return row.SCAccountID })

	rows := make([]HCPHierarchyRowView, 0, len(r.Rows))
	for _, row := range r.Rows {
		rows = append(rows, HCPHierarchyRowView{
			CustomerClusterName: row.CustomerClusterName,
			CustomerClusterID:   row.CustomerClusterID,
			OrganizationName:    row.OrganizationName,
			Classification:      row.Classification,
			CustomerState:       row.CustomerState,
			CustomerStateClass:  clusterStateClass(row.CustomerState),
			MCAccountID:         row.MCAccountID,
			MCName:              row.MCName,
			MCRegion:            displayOrDash(row.MCRegion),
			MCStatus:            displayOrDash(row.MCStatus),
			MCStatusClass:       tierStatusClass(row.MCStatus),
			SCAccountID:         row.SCAccountID,
			SCName:              row.SCName,
			SCRegion:            displayOrDash(row.SCRegion),
			SCStatus:            displayOrDash(row.SCStatus),
			SCStatusClass:       tierStatusClass(row.SCStatus),
			HierarchyComplete:   row.HierarchyComplete,
			BillingModel:        displayOrDash(row.BillingModel),
		})
	}

	return HCPHierarchyReportView{
		GeneratedAt:    r.GeneratedAt.UTC().Format("2006-01-02 15:04:05 UTC"),
		MartView:       r.MartView,
		WorkerCount:    len(r.Rows),
		MCAccountCount: len(mcAccounts),
		SCAccountCount: len(scAccounts),
		CompleteCount:  countComplete(r.Rows),
		Rows:           rows,
	}
}

func clusterStateClass(state string) string {
	switch state {
	case "ready":
		return "state-ready"
	case "deleting", "error":
		return "state-deleting"
	default:
		return "state-notready"
	}
}

func tierStatusClass(status string) string {
	if status == "ready" {
		return "state-ready"
	}
	return "state-notready"
}

func displayOrDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// RenderHCPHierarchyHTML renders a pre-built hcphierarchy.Report as HTML.
func RenderHCPHierarchyHTML(w io.Writer, r corehcp.Report) error {
	t, err := hcpHierarchyTemplateCompiled()
	if err != nil {
		return fmt.Errorf("compile hcp-hierarchy template: %w", err)
	}
	view := NewHCPHierarchyReportView(r)
	if err := t.ExecuteTemplate(w, hcpHierarchyTemplate, view); err != nil {
		return fmt.Errorf("render hcp-hierarchy template: %w", err)
	}
	return nil
}

func uniqueAccountIDs(rows []corehcp.HierarchyRow, pick func(corehcp.HierarchyRow) string) map[string]bool {
	seen := make(map[string]bool)
	for _, row := range rows {
		if id := pick(row); id != "" {
			seen[id] = true
		}
	}
	return seen
}

func countComplete(rows []corehcp.HierarchyRow) int {
	n := 0
	for _, row := range rows {
		if row.HierarchyComplete {
			n++
		}
	}
	return n
}
