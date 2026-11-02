// monthly_grouped.go fetches monthly net amortized cost grouped by linked account and AWS service.
package cost

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
)

// MonthlyGroupedCost is one monthly cost cell keyed by linked account and AWS service.
type MonthlyGroupedCost struct {
	// Month is the YYYY-MM calendar month key.
	Month string
	// AccountID is the linked account ID from the LINKED_ACCOUNT group key.
	AccountID string
	// Service is the AWS service name from the SERVICE group key.
	Service string
	// Amount is the net amortized cost in the billing currency.
	Amount float64
}

// monthlyGroupedGroupDefs is the two-dimension group-by used by the executive summary:
// group keys come back in the same order, so Keys[0] is the linked account ID and
// Keys[1] is the AWS service name.
var monthlyGroupedGroupDefs = []types.GroupDefinition{
	{Type: types.GroupDefinitionTypeDimension, Key: aws.String("LINKED_ACCOUNT")},
	{Type: types.GroupDefinitionTypeDimension, Key: aws.String("SERVICE")},
}

// sumNetAmortizedMonthlyGrouped retrieves monthly NetAmortizedCost grouped by
// LINKED_ACCOUNT and SERVICE over dr (month-aligned half-open range), consuming
// every page. It fails on mixed currencies and unparseable amounts.
func sumNetAmortizedMonthlyGrouped(
	ctx context.Context,
	ce CostExplorerAPI,
	dr DateRange,
	filter *types.Expression,
) ([]MonthlyGroupedCost, string, error) {
	var rows []MonthlyGroupedCost
	currency := ""
	var token *string

	for {
		out, err := ce.GetCostAndUsage(ctx, &costexplorer.GetCostAndUsageInput{
			TimePeriod: &types.DateInterval{
				Start: aws.String(FormatDate(dr.Start)),
				End:   aws.String(FormatDate(dr.End)),
			},
			Granularity:   types.GranularityMonthly,
			Metrics:       []string{MetricNetAmortized},
			GroupBy:       monthlyGroupedGroupDefs,
			Filter:        filter,
			NextPageToken: token,
		})
		if err != nil {
			return nil, "", fmt.Errorf("cost explorer GetCostAndUsage: %w", err)
		}

		for _, row := range out.ResultsByTime {
			month := monthKeyFromPeriod(row.TimePeriod)
			if month == "" {
				continue
			}
			for _, g := range row.Groups {
				if len(g.Keys) < 2 {
					continue
				}
				m, ok := g.Metrics[MetricNetAmortized]
				if !ok {
					continue
				}
				amt, err := strconv.ParseFloat(aws.ToString(m.Amount), 64)
				if err != nil {
					return nil, "", fmt.Errorf("parse %s amount for %s/%s/%s: %w",
						MetricNetAmortized, month, g.Keys[0], g.Keys[1], err)
				}
				if amt == 0 {
					continue
				}
				unit := aws.ToString(m.Unit)
				if unit != "" {
					if currency == "" {
						currency = unit
					} else if unit != currency {
						return nil, "", fmt.Errorf("cannot merge %s results with mixed currency (%s vs %s)",
							MetricNetAmortized, currency, unit)
					}
				}
				rows = append(rows, MonthlyGroupedCost{
					Month:     month,
					AccountID: strings.TrimSpace(g.Keys[0]),
					Service:   strings.TrimSpace(g.Keys[1]),
					Amount:    amt,
				})
			}
		}

		if out.NextPageToken == nil || *out.NextPageToken == "" {
			break
		}
		token = out.NextPageToken
	}

	if currency == "" {
		currency = "USD"
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Month != rows[j].Month {
			return rows[i].Month < rows[j].Month
		}
		if rows[i].AccountID != rows[j].AccountID {
			return rows[i].AccountID < rows[j].AccountID
		}
		return rows[i].Service < rows[j].Service
	})
	return rows, currency, nil
}
