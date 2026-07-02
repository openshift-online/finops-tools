package apilog

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
)

type getCostAndUsageAPI interface {
	GetCostAndUsage(context.Context, *costexplorer.GetCostAndUsageInput, ...func(*costexplorer.Options)) (*costexplorer.GetCostAndUsageOutput, error)
}

type loggingGetCostAndUsage struct {
	inner getCostAndUsageAPI
}

// WrapGetCostAndUsage returns a client that logs GetCostAndUsage when ctx has an apilog logger.
func WrapGetCostAndUsage(inner getCostAndUsageAPI) getCostAndUsageAPI {
	if inner == nil {
		return nil
	}
	return loggingGetCostAndUsage{inner: inner}
}

func (l loggingGetCostAndUsage) GetCostAndUsage(
	ctx context.Context,
	params *costexplorer.GetCostAndUsageInput,
	optFns ...func(*costexplorer.Options),
) (*costexplorer.GetCostAndUsageOutput, error) {
	LogGetCostAndUsage(ctx, params)
	return l.inner.GetCostAndUsage(ctx, params, optFns...)
}

// LogGetCostAndUsage logs a Cost Explorer GetCostAndUsage request when verbose mode is enabled.
func LogGetCostAndUsage(ctx context.Context, in *costexplorer.GetCostAndUsageInput) {
	if in == nil {
		return
	}
	Log(ctx, formatGetCostAndUsage(in))
}

func formatGetCostAndUsage(in *costexplorer.GetCostAndUsageInput) string {
	var b strings.Builder
	b.WriteString("CostExplorer.GetCostAndUsage")
	appendCETimePeriod(&b, in.TimePeriod)
	if in.Granularity != "" {
		fmt.Fprintf(&b, " granularity=%s", in.Granularity)
	}
	if len(in.Metrics) > 0 {
		fmt.Fprintf(&b, " metrics=%s", strings.Join(in.Metrics, ","))
	}
	for _, group := range in.GroupBy {
		if key := aws.ToString(group.Key); key != "" {
			fmt.Fprintf(&b, " groupBy=%s", key)
		}
	}
	if filter := formatCEFilter(in.Filter); filter != "" {
		fmt.Fprintf(&b, " filter=%s", filter)
	}
	appendCEPageToken(&b, in.NextPageToken)
	return b.String()
}

type savingsPlansAPI interface {
	GetSavingsPlansCoverage(context.Context, *costexplorer.GetSavingsPlansCoverageInput, ...func(*costexplorer.Options)) (*costexplorer.GetSavingsPlansCoverageOutput, error)
	GetSavingsPlansUtilization(context.Context, *costexplorer.GetSavingsPlansUtilizationInput, ...func(*costexplorer.Options)) (*costexplorer.GetSavingsPlansUtilizationOutput, error)
}

type loggingSavingsPlans struct {
	inner savingsPlansAPI
}

// WrapSavingsPlans returns a client that logs Savings Plans CE calls when ctx has an apilog logger.
func WrapSavingsPlans(inner savingsPlansAPI) savingsPlansAPI {
	if inner == nil {
		return nil
	}
	return loggingSavingsPlans{inner: inner}
}

func (l loggingSavingsPlans) GetSavingsPlansCoverage(
	ctx context.Context,
	params *costexplorer.GetSavingsPlansCoverageInput,
	optFns ...func(*costexplorer.Options),
) (*costexplorer.GetSavingsPlansCoverageOutput, error) {
	LogGetSavingsPlansCoverage(ctx, params)
	return l.inner.GetSavingsPlansCoverage(ctx, params, optFns...)
}

func (l loggingSavingsPlans) GetSavingsPlansUtilization(
	ctx context.Context,
	params *costexplorer.GetSavingsPlansUtilizationInput,
	optFns ...func(*costexplorer.Options),
) (*costexplorer.GetSavingsPlansUtilizationOutput, error) {
	LogGetSavingsPlansUtilization(ctx, params)
	return l.inner.GetSavingsPlansUtilization(ctx, params, optFns...)
}

type costAnomaliesAPI interface {
	GetAnomalies(context.Context, *costexplorer.GetAnomaliesInput, ...func(*costexplorer.Options)) (*costexplorer.GetAnomaliesOutput, error)
}

type loggingCostAnomalies struct {
	inner costAnomaliesAPI
}

// WrapCostAnomalies returns a client that logs GetAnomalies when ctx has an apilog logger.
func WrapCostAnomalies(inner costAnomaliesAPI) costAnomaliesAPI {
	if inner == nil {
		return nil
	}
	return loggingCostAnomalies{inner: inner}
}

func (l loggingCostAnomalies) GetAnomalies(
	ctx context.Context,
	params *costexplorer.GetAnomaliesInput,
	optFns ...func(*costexplorer.Options),
) (*costexplorer.GetAnomaliesOutput, error) {
	LogGetAnomalies(ctx, params)
	return l.inner.GetAnomalies(ctx, params, optFns...)
}

// LogGetSavingsPlansCoverage logs a GetSavingsPlansCoverage request when verbose mode is enabled.
func LogGetSavingsPlansCoverage(ctx context.Context, in *costexplorer.GetSavingsPlansCoverageInput) {
	if in == nil {
		return
	}
	Log(ctx, formatGetSavingsPlansCoverage(in))
}

// LogGetSavingsPlansUtilization logs a GetSavingsPlansUtilization request when verbose mode is enabled.
func LogGetSavingsPlansUtilization(ctx context.Context, in *costexplorer.GetSavingsPlansUtilizationInput) {
	if in == nil {
		return
	}
	Log(ctx, formatGetSavingsPlansUtilization(in))
}

// LogGetAnomalies logs a GetAnomalies request when verbose mode is enabled.
func LogGetAnomalies(ctx context.Context, in *costexplorer.GetAnomaliesInput) {
	if in == nil {
		return
	}
	Log(ctx, formatGetAnomalies(in))
}

func formatGetSavingsPlansCoverage(in *costexplorer.GetSavingsPlansCoverageInput) string {
	var b strings.Builder
	b.WriteString("CostExplorer.GetSavingsPlansCoverage")
	appendCETimePeriod(&b, in.TimePeriod)
	if in.Granularity != "" {
		fmt.Fprintf(&b, " granularity=%s", in.Granularity)
	}
	if filter := formatCEFilter(in.Filter); filter != "" {
		fmt.Fprintf(&b, " filter=%s", filter)
	}
	appendCEPageToken(&b, in.NextToken)
	return b.String()
}

func formatGetSavingsPlansUtilization(in *costexplorer.GetSavingsPlansUtilizationInput) string {
	var b strings.Builder
	b.WriteString("CostExplorer.GetSavingsPlansUtilization")
	appendCETimePeriod(&b, in.TimePeriod)
	if in.Granularity != "" {
		fmt.Fprintf(&b, " granularity=%s", in.Granularity)
	}
	if filter := formatCEFilter(in.Filter); filter != "" {
		fmt.Fprintf(&b, " filter=%s", filter)
	}
	return b.String()
}

func formatGetAnomalies(in *costexplorer.GetAnomaliesInput) string {
	var b strings.Builder
	b.WriteString("CostExplorer.GetAnomalies")
	if in.DateInterval != nil {
		fmt.Fprintf(&b, " start=%s end=%s", aws.ToString(in.DateInterval.StartDate), aws.ToString(in.DateInterval.EndDate))
	}
	appendCEPageToken(&b, in.NextPageToken)
	return b.String()
}

func appendCETimePeriod(b *strings.Builder, tp *types.DateInterval) {
	if tp == nil {
		return
	}
	fmt.Fprintf(b, " start=%s end=%s", aws.ToString(tp.Start), aws.ToString(tp.End))
}

func appendCEPageToken(b *strings.Builder, token *string) {
	if token != nil && aws.ToString(token) != "" {
		b.WriteString(" page=next")
	}
}

func formatCEFilter(expr *types.Expression) string {
	if expr == nil {
		return ""
	}
	if expr.Dimensions != nil {
		return formatCEKeyValues(string(expr.Dimensions.Key), expr.Dimensions.Values)
	}
	if expr.Tags != nil {
		return formatNamedCEValues("tag", aws.ToString(expr.Tags.Key), expr.Tags.Values)
	}
	if expr.CostCategories != nil {
		return formatNamedCEValues("costCategory", aws.ToString(expr.CostCategories.Key), expr.CostCategories.Values)
	}
	if len(expr.And) > 0 {
		parts := make([]string, 0, len(expr.And))
		for i := range expr.And {
			if part := formatCEFilter(&expr.And[i]); part != "" {
				parts = append(parts, part)
			}
		}
		return strings.Join(parts, " AND ")
	}
	if len(expr.Or) > 0 {
		parts := make([]string, 0, len(expr.Or))
		for i := range expr.Or {
			if part := formatCEFilter(&expr.Or[i]); part != "" {
				parts = append(parts, part)
			}
		}
		return strings.Join(parts, " OR ")
	}
	if expr.Not != nil {
		if part := formatCEFilter(expr.Not); part != "" {
			return "NOT " + part
		}
	}
	return ""
}

func formatCEKeyValues(key string, vals []string) string {
	if key == "" {
		return ""
	}
	if len(vals) == 0 {
		return key
	}
	if len(vals) <= 3 {
		return fmt.Sprintf("%s=%s", key, strings.Join(vals, "|"))
	}
	return fmt.Sprintf("%s=%s,... (%d)", key, strings.Join(vals[:3], "|"), len(vals))
}

func formatNamedCEValues(kind, key string, vals []string) string {
	if key == "" {
		return kind
	}
	return formatCEKeyValues(kind+":"+key, vals)
}
