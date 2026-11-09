package extendedsupport

import (
	"fmt"
	"strings"

	"github.com/openshift-online/finops-tools/core/awsregions"
	"github.com/openshift-online/finops-tools/core/cost"
)

type CostGroupBy string

const (
	CostGroupByService        CostGroupBy = "service"
	CostGroupByAccount        CostGroupBy = "account"
	CostGroupByServiceAccount CostGroupBy = "service,account"
)

func ParseCostGroupBy(value string) (CostGroupBy, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "service":
		return CostGroupByService, nil
	case "account":
		return CostGroupByAccount, nil
	case "service,account", "account,service":
		return CostGroupByServiceAccount, nil
	default:
		return "", fmt.Errorf("unknown group-by %q (supported: service, account, service,account)", value)
	}
}

// ParseCostGranularity defaults to monthly independently of the date range.
func ParseCostGranularity(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return "monthly", nil
	case "daily", "monthly":
		return strings.ToLower(strings.TrimSpace(value)), nil
	default:
		return "", fmt.Errorf("unknown granularity %q (supported: daily, monthly)", value)
	}
}

func ParseCostMetric(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "netunblendedcost":
		return "NetUnblendedCost", nil
	case "unblendedcost":
		return "UnblendedCost", nil
	default:
		return "", fmt.Errorf("unknown metric %q (supported: NetUnblendedCost, UnblendedCost)", value)
	}
}

// CostQuery uses billing credentials only; member role assumption and resource
// discovery are not required. An unscoped payer target includes its whole bill.
type CostQuery struct {
	Targets     []cost.AccountTarget
	Range       cost.DateRange
	Services    []Service
	Regions     []string // Optional billing-region inclusion list; empty means all.
	SkipRegions []string // Takes precedence over Regions, including when all are excluded.
	GroupBy     CostGroupBy
	Granularity string
	TotalsOnly  bool // Combine time buckets into one period total per group.
	Metric      string
	// allServices is reserved for the account-only discovery-gap check. It
	// removes SERVICE filters rather than expanding to the known service list.
	allServices bool
	newClient   func(cost.AccountTarget) BillingAPI
}

// CostAmounts distinguishes recorded billing from identifiable incremental
// surcharges. A nil AvoidableAmount means some charges cannot safely be split.
// KnownAvoidableAmount remains a subtotal, never a complete savings forecast.
type CostAmounts struct {
	Amount               float64  `json:"amount"`
	AvoidableAmount      *float64 `json:"avoidable_amount"`
	KnownAvoidableAmount float64  `json:"known_avoidable_amount"`
}

type CostRow struct {
	StartDate string  `json:"start_date"`
	EndDate   string  `json:"end_date"` // Inclusive, like other cost command output.
	Service   Service `json:"service,omitempty"`
	AccountID string  `json:"account_id,omitempty"`
	Estimated bool    `json:"estimated"`
	CostAmounts
}

type CostResult struct {
	StartDate       string      `json:"start_date"`
	EndDate         string      `json:"end_date"`
	Granularity     string      `json:"granularity"`
	GroupBy         CostGroupBy `json:"group_by"`
	Services        []Service   `json:"services"`
	Regions         []string    `json:"regions,omitempty"`
	ExcludedRegions []string    `json:"excluded_regions,omitempty"`
	Metric          string      `json:"metric"`
	Currency        string      `json:"currency"`
	Estimated       bool        `json:"estimated"`
	UsageTypes      []string    `json:"usage_types"`
	Rows            []CostRow   `json:"rows"`
	Notices         []string    `json:"notices"`
	CostAmounts
}

func emptyCostAmounts() CostAmounts {
	zero := 0.0
	return CostAmounts{AvoidableAmount: &zero}
}

func NewCostResult(q CostQuery) CostResult {
	result := CostResult{
		StartDate: cost.FormatDate(q.Range.Start), EndDate: cost.FormatDate(q.Range.End.AddDate(0, 0, -1)),
		Granularity: q.Granularity, GroupBy: q.GroupBy, Services: q.Services, Metric: q.Metric,
		Regions: awsregions.Normalize(q.Regions), ExcludedRegions: awsregions.Normalize(q.SkipRegions),
		Rows: []CostRow{}, UsageTypes: []string{}, CostAmounts: emptyCostAmounts(),
		Notices: []string{
			"Recorded charges cover Extended Support usage types, not total resource running costs. Billing data may be delayed or revised.",
			"Avoidable amounts identify historical, separately billed support surcharges under the selected metric; they are not a forecast of net savings after an upgrade or a change in usage.",
		},
	}
	if q.TotalsOnly {
		result.Granularity = "total"
	}
	if q.Metric == "UnblendedCost" {
		result.Notices = append(result.Notices, "UnblendedCost reports gross charges; applicable discounts can reduce actual avoidable spending. Use NetUnblendedCost for after-discount amounts.")
	}
	if len(result.Regions) > 0 && len(awsregions.Exclude(result.Regions, result.ExcludedRegions)) == 0 {
		result.Notices = append(result.Notices, "All requested billing regions are excluded.")
	}
	return result
}
