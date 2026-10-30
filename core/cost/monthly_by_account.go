package cost

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
	"github.com/openshift-online/finops-tools/core/parallel"
)

// MonthlyAccountServiceCost is net amortized cost for one (linked account,
// AWS service, month) triple.
type MonthlyAccountServiceCost struct {
	AccountID string  `json:"account_id"`
	Service   string  `json:"service"`
	Month     string  `json:"month"` // YYYY-MM
	Amount    float64 `json:"amount"`
	Currency  string  `json:"currency"`
}

// FetchMonthlyByAccountAndService retrieves net amortized cost grouped by
// calendar month, linked (member) AWS account, and AWS service, in one Cost
// Explorer query per target in q.Accounts (fetched in parallel, bounded by
// q.Workers). Cost Explorer allows up to two GroupBy dimensions per query,
// so LINKED_ACCOUNT and SERVICE are both requested alongside MONTHLY
// granularity in a single call rather than one query per dimension.
//
// This is the shape core/report/execsummary.CostRecord needs (spec section
// 3.3.A): each row keys cost by AWS account ID, service, and month for
// mapping-CSV-driven category, growing-account, and anomaly analytics.
//
// A Cost Explorer failure for any target aborts the whole batch: unlike
// FetchMonthly's best-effort per-account Error field, executive-summary
// analytics need every payer's full history to produce a meaningful report.
func FetchMonthlyByAccountAndService(ctx context.Context, q CostQuery) ([]MonthlyAccountServiceCost, error) {
	if len(q.Accounts) == 0 {
		return nil, fmt.Errorf("at least one account is required")
	}
	opts := fetchAWSOptions{
		Now:             time.Now(),
		NewCostExplorer: defaultCostExplorerFactory(),
	}
	return fetchMonthlyByAccountAndServiceWith(ctx, q, opts)
}

func fetchMonthlyByAccountAndServiceWith(ctx context.Context, q CostQuery, opts fetchAWSOptions) ([]MonthlyAccountServiceCost, error) {
	targets := q.Accounts
	if q.Progress != nil && len(targets) > 1 {
		q.Progress.Step(fmt.Sprintf("Fetching monthly costs by account and service for %d target(s)…", len(targets)))
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}

	results := make([][]MonthlyAccountServiceCost, len(targets))
	err := parallel.ForEach(ctx, q.Workers, len(targets), func(ctx context.Context, i int) error {
		acct := targets[i]
		reportFetchProgress(q.Progress, acct, i+1, len(targets), GroupByAccount)
		rows, err := fetchAccountMonthlyByAccountAndServiceWith(ctx, acct, q.Range, opts)
		if err != nil {
			return err
		}
		results[i] = rows
		return nil
	})
	if err != nil {
		return nil, err
	}

	var out []MonthlyAccountServiceCost
	for _, rows := range results {
		out = append(out, rows...)
	}
	return out, nil
}

// monthAccountServiceKey identifies one (linked account, service, month) cell.
type monthAccountServiceKey struct {
	accountID string
	service   string
	month     string
}

func fetchAccountMonthlyByAccountAndServiceWith(
	ctx context.Context,
	acct AccountTarget,
	dr DateRange,
	opts fetchAWSOptions,
) ([]MonthlyAccountServiceCost, error) {
	cfg := acct.AWSConfig
	if cfg.Region == "" {
		cfg.Region = CostExplorerRegion
	}
	ce := opts.NewCostExplorer(cfg)
	filter := LinkedAccountFilter(acct.AccountID, acct.ScopeToAccount())
	ceDR := monthlyCERange(dr)

	byKey := make(map[monthAccountServiceKey]float64)
	currency := "USD"
	var token *string

	ceGroupDefs := []types.GroupDefinition{
		{Type: types.GroupDefinitionTypeDimension, Key: aws.String("LINKED_ACCOUNT")},
		{Type: types.GroupDefinitionTypeDimension, Key: aws.String("SERVICE")},
	}

	for {
		out, err := ce.GetCostAndUsage(ctx, &costexplorer.GetCostAndUsageInput{
			TimePeriod: &types.DateInterval{
				Start: aws.String(FormatDate(ceDR.Start)),
				End:   aws.String(FormatDate(ceDR.End)),
			},
			Granularity:   types.GranularityMonthly,
			Metrics:       []string{MetricNetAmortized},
			GroupBy:       ceGroupDefs,
			Filter:        filter,
			NextPageToken: token,
		})
		if err != nil {
			return nil, fmt.Errorf("cost explorer GetCostAndUsage: %w", err)
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
				accountID, service := g.Keys[0], g.Keys[1]
				m, ok := g.Metrics[MetricNetAmortized]
				if !ok {
					continue
				}
				amt, err := strconv.ParseFloat(aws.ToString(m.Amount), 64)
				if err != nil {
					return nil, fmt.Errorf("parse %s amount for %s/%s/%s: %w", MetricNetAmortized, accountID, service, month, err)
				}
				key := monthAccountServiceKey{accountID: accountID, service: service, month: month}
				byKey[key] += amt
				if u := aws.ToString(m.Unit); u != "" {
					currency = u
				}
			}
		}

		if out.NextPageToken == nil || *out.NextPageToken == "" {
			break
		}
		token = out.NextPageToken
	}

	rows := make([]MonthlyAccountServiceCost, 0, len(byKey))
	for key, amt := range byKey {
		rows = append(rows, MonthlyAccountServiceCost{
			AccountID: key.accountID,
			Service:   key.service,
			Month:     key.month,
			Amount:    amt,
			Currency:  currency,
		})
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
	return rows, nil
}
