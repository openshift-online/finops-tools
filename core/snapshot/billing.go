package snapshot

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
	"github.com/openshift-online/finops-tools/core/apilog"
)

const costExplorerRegion = "us-east-1"

// CostExplorerAPI is the subset of Cost Explorer used for billed snapshot costs.
type CostExplorerAPI interface {
	GetCostAndUsage(
		ctx context.Context,
		params *costexplorer.GetCostAndUsageInput,
		optFns ...func(*costexplorer.Options),
	) (*costexplorer.GetCostAndUsageOutput, error)
}

// BilledSnapshotPeriod is the Cost Explorer window used for billed snapshot lines.
type BilledSnapshotPeriod struct {
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
}

// AccountBilledSnapshotCosts is actual billed snapshot storage from Cost Explorer.
type AccountBilledSnapshotCosts struct {
	AccountID            string               `json:"account_id"`
	Period               BilledSnapshotPeriod `json:"period"`
	EBSSnapshotUSD       float64              `json:"ebs_snapshot_usd"`
	EBSSnapshotGiBMonth  float64              `json:"ebs_snapshot_gib_month,omitempty"`
	RDSBackupUSD         float64              `json:"rds_backup_usd"`
	RDSBackupGiBMonth    float64              `json:"rds_backup_usage_gib_month,omitempty"`
}

// LastCompleteMonthRange returns the Cost Explorer date range for the last full calendar month.
func LastCompleteMonthRange(now time.Time) (start, end time.Time) {
	now = now.UTC()
	firstOfThisMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	end = firstOfThisMonth
	start = firstOfThisMonth.AddDate(0, -1, 0)
	return start, end
}

// FetchBilledSnapshotCosts queries Cost Explorer for billed EBS snapshot and RDS backup storage.
func FetchBilledSnapshotCosts(
	ctx context.Context,
	ce CostExplorerAPI,
	accountIDs []string,
	now time.Time,
) ([]AccountBilledSnapshotCosts, error) {
	if ce == nil {
		return nil, fmt.Errorf("cost explorer client is required")
	}
	start, end := LastCompleteMonthRange(now)
	period := BilledSnapshotPeriod{
		StartDate: formatBillingDate(start),
		EndDate:   formatBillingDate(end.AddDate(0, 0, -1)),
	}

	accountIDs = uniqueTrimmedAccountIDs(accountIDs)

	byAccount := make(map[string]*AccountBilledSnapshotCosts, len(accountIDs))
	for _, accountID := range accountIDs {
		costs, err := fetchAccountBilledSnapshotCosts(ctx, ce, accountID, start, end)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", accountID, err)
		}
		costs.Period = period
		byAccount[accountID] = &costs
	}

	out := make([]AccountBilledSnapshotCosts, 0, len(accountIDs))
	for _, accountID := range accountIDs {
		out = append(out, *byAccount[accountID])
	}
	return out, nil
}

func uniqueTrimmedAccountIDs(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func fetchAccountBilledSnapshotCosts(
	ctx context.Context,
	ce CostExplorerAPI,
	accountID string,
	start, end time.Time,
) (AccountBilledSnapshotCosts, error) {
	result := AccountBilledSnapshotCosts{AccountID: accountID}
	var token *string
	for {
		out, err := ce.GetCostAndUsage(ctx, &costexplorer.GetCostAndUsageInput{
			TimePeriod: &types.DateInterval{
				Start: aws.String(formatBillingDate(start)),
				End:   aws.String(formatBillingDate(end)),
			},
			Granularity: types.GranularityMonthly,
			Metrics:     []string{"UnblendedCost", "UsageQuantity"},
			GroupBy: []types.GroupDefinition{{
				Type: types.GroupDefinitionTypeDimension,
				Key:  aws.String("USAGE_TYPE"),
			}},
			Filter: accountCEFilter(accountID),
			NextPageToken: token,
		})
		if err != nil {
			return AccountBilledSnapshotCosts{}, fmt.Errorf("cost explorer GetCostAndUsage: %w", err)
		}

		for _, row := range out.ResultsByTime {
			for _, group := range row.Groups {
				if len(group.Keys) == 0 {
					continue
				}
				usageType := group.Keys[0]
				if isEBSSnapshotUsageType(usageType) {
					cost, usage, err := parseCEMetrics(group.Metrics)
					if err != nil {
						return AccountBilledSnapshotCosts{}, err
					}
					result.EBSSnapshotUSD += cost
					result.EBSSnapshotGiBMonth += usage
				}
				if isRDSBackupUsageType(usageType) {
					cost, usage, err := parseCEMetrics(group.Metrics)
					if err != nil {
						return AccountBilledSnapshotCosts{}, err
					}
					result.RDSBackupUSD += cost
					result.RDSBackupGiBMonth += usage
				}
			}
		}

		if out.NextPageToken == nil || aws.ToString(out.NextPageToken) == "" {
			break
		}
		token = out.NextPageToken
	}
	return result, nil
}

func accountCEFilter(accountID string) *types.Expression {
	return &types.Expression{
		Dimensions: &types.DimensionValues{
			Key:    types.DimensionLinkedAccount,
			Values: []string{accountID},
		},
	}
}

func isEBSSnapshotUsageType(usageType string) bool {
	usageType = strings.ToLower(usageType)
	return strings.Contains(usageType, "ebs:snapshotusage") ||
		strings.Contains(usageType, "ebs:snapshotarchivestorage")
}

func isRDSBackupUsageType(usageType string) bool {
	return strings.Contains(strings.ToLower(usageType), "chargedbackupusage")
}

func parseCEMetrics(metrics map[string]types.MetricValue) (cost, usage float64, err error) {
	if m, ok := metrics["UnblendedCost"]; ok {
		cost, err = strconv.ParseFloat(aws.ToString(m.Amount), 64)
		if err != nil {
			return 0, 0, fmt.Errorf("parse UnblendedCost: %w", err)
		}
	}
	if m, ok := metrics["UsageQuantity"]; ok {
		usage, err = strconv.ParseFloat(aws.ToString(m.Amount), 64)
		if err != nil {
			return 0, 0, fmt.Errorf("parse UsageQuantity: %w", err)
		}
	}
	return cost, usage, nil
}

func formatBillingDate(t time.Time) string {
	return t.UTC().Format("2006-01-02")
}

// NewCostExplorerClient returns a Cost Explorer client (API endpoint is us-east-1).
func NewCostExplorerClient(cfg aws.Config) CostExplorerAPI {
	return newCostExplorerClient(cfg)
}

func newCostExplorerClient(cfg aws.Config) CostExplorerAPI {
	if cfg.Region == "" {
		cfg.Region = costExplorerRegion
	}
	inner := costexplorer.NewFromConfig(cfg, func(o *costexplorer.Options) {
		o.Region = costExplorerRegion
	})
	return apilog.WrapGetCostAndUsage(inner)
}
