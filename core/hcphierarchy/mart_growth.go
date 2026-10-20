package hcphierarchy

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/openshift-online/finops-tools/core/snowflake"
)

// ClusterCountRow is the number of new worker clusters created in one
// calendar month, for one environment. Not cumulative — callers compute
// running totals and month-over-month deltas (see core/report/execsummary
// for the cumulative/MoM formula layer, golang-executive-summary-spec.md
// sections 5 step 3 and 6.11).
type ClusterCountRow struct {
	Month       string // YYYY-MM
	Environment string // Production, Stage, or Integration
	NewClusters int
}

// EnvironmentActiveCount is a current-snapshot count of active (ready or
// hibernating) worker clusters for one environment.
type EnvironmentActiveCount struct {
	Environment string
	ActiveCount int
}

// ClusterCountsReport holds golang-executive-summary-spec.md section 5 step 3
// data: monthly new-cluster counts per environment (for cumulative totals
// and MoM deltas) plus a current-snapshot active count per environment (used
// as the HCP Unit Cost denominator).
type ClusterCountsReport struct {
	GeneratedAt time.Time
	MartView    string
	Counts      []ClusterCountRow
	ActiveByEnv []EnvironmentActiveCount
}

// BuildClusterCounts resolves cluster-count-by-month-by-environment data
// with two queries against the same mart view: monthly new-cluster counts
// (for cumulative totals and MoM deltas) and a current-snapshot active count
// per environment. Pass martView = "" to use the default mart location.
func BuildClusterCounts(ctx context.Context, sf SnowflakeQueryer, martView string) (ClusterCountsReport, error) {
	if martView == "" {
		martView = defaultMartView
	}
	if err := snowflake.ValidateQualifiedIdentifier(martView, 3); err != nil {
		return ClusterCountsReport{}, fmt.Errorf("invalid mart view: %w", err)
	}

	counts, err := fetchClusterCounts(ctx, sf, martView)
	if err != nil {
		return ClusterCountsReport{}, err
	}
	active, err := fetchActiveEnvironmentCounts(ctx, sf, martView)
	if err != nil {
		return ClusterCountsReport{}, err
	}

	return ClusterCountsReport{
		GeneratedAt: time.Now().UTC(),
		MartView:    martView,
		Counts:      counts,
		ActiveByEnv: active,
	}, nil
}

func fetchClusterCounts(ctx context.Context, sf SnowflakeQueryer, martView string) ([]ClusterCountRow, error) {
	sql := clusterCountsSQL(martView)
	rows, err := sf.QueryContext(ctx, sql)
	if err != nil {
		return nil, fmt.Errorf("cluster counts query: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	var out []ClusterCountRow
	for rows.Next() {
		var (
			month, environment string
			newClusters        int64
		)
		if err := rows.Scan(&month, &environment, &newClusters); err != nil {
			return nil, fmt.Errorf("scan cluster count row: %w", err)
		}
		out = append(out, ClusterCountRow{
			Month:       month,
			Environment: environment,
			NewClusters: int(newClusters),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cluster count rows: %w", err)
	}
	return out, nil
}

func fetchActiveEnvironmentCounts(ctx context.Context, sf SnowflakeQueryer, martView string) ([]EnvironmentActiveCount, error) {
	sql := activeEnvironmentCountsSQL(martView)
	rows, err := sf.QueryContext(ctx, sql)
	if err != nil {
		return nil, fmt.Errorf("active environment counts query: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	var out []EnvironmentActiveCount
	for rows.Next() {
		var (
			environment string
			activeCount int64
		)
		if err := rows.Scan(&environment, &activeCount); err != nil {
			return nil, fmt.Errorf("scan active environment count row: %w", err)
		}
		out = append(out, EnvironmentActiveCount{
			Environment: environment,
			ActiveCount: int(activeCount),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active environment count rows: %w", err)
	}
	return out, nil
}

func clusterCountsSQL(martView string) string {
	return strings.TrimSpace(fmt.Sprintf(`
SELECT
    TO_CHAR(DATE_TRUNC('month', CREATION_TIMESTAMP), 'YYYY-MM') AS month,
    ENVIRONMENT                                                 AS environment,
    COUNT(DISTINCT EXTERNAL_ID)                                 AS new_this_month
FROM %s
WHERE CLUSTER_ROLE = 'worker'
  AND CREATION_TIMESTAMP IS NOT NULL
GROUP BY 1, 2
ORDER BY 1, 2`, martView))
}

func activeEnvironmentCountsSQL(martView string) string {
	return strings.TrimSpace(fmt.Sprintf(`
SELECT
    ENVIRONMENT AS environment,
    COUNT(DISTINCT EXTERNAL_ID) AS active_count
FROM %s
WHERE CLUSTER_ROLE = 'worker'
  AND STATE IN ('ready', 'hibernating')
GROUP BY 1`, martView))
}

// ClusterGrowthRow is the number of new clusters (worker or standalone)
// created in one calendar month, for one product type. Not cumulative —
// callers compute running totals (golang-executive-summary-spec.md section
// 5 step 4 and section 7.7).
type ClusterGrowthRow struct {
	Month       string // YYYY-MM
	ProductType string // e.g. "ROSA HCP", "ROSA Classic", "OSD", "OSD Trial"
	NewClusters int
}

// ClusterGrowthReport holds golang-executive-summary-spec.md section 5 step 4
// data.
type ClusterGrowthReport struct {
	GeneratedAt time.Time
	MartView    string
	Rows        []ClusterGrowthRow
}

// BuildClusterGrowth resolves cluster-growth-by-product-type data. Includes
// both worker (ROSA HCP) and standalone (ROSA Classic/OSD/OSD Trial) rows;
// management/service infrastructure rows are intentionally excluded (spec
// section 7.7 — they are infra, not customer products). Pass martView = ""
// to use the default mart location.
func BuildClusterGrowth(ctx context.Context, sf SnowflakeQueryer, martView string) (ClusterGrowthReport, error) {
	if martView == "" {
		martView = defaultMartView
	}
	if err := snowflake.ValidateQualifiedIdentifier(martView, 3); err != nil {
		return ClusterGrowthReport{}, fmt.Errorf("invalid mart view: %w", err)
	}

	sql := clusterGrowthSQL(martView)
	rows, err := sf.QueryContext(ctx, sql)
	if err != nil {
		return ClusterGrowthReport{}, fmt.Errorf("cluster growth query: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	var out []ClusterGrowthRow
	for rows.Next() {
		var (
			month, productType string
			newClusters        int64
		)
		if err := rows.Scan(&month, &productType, &newClusters); err != nil {
			return ClusterGrowthReport{}, fmt.Errorf("scan cluster growth row: %w", err)
		}
		out = append(out, ClusterGrowthRow{
			Month:       month,
			ProductType: productType,
			NewClusters: int(newClusters),
		})
	}
	if err := rows.Err(); err != nil {
		return ClusterGrowthReport{}, fmt.Errorf("iterate cluster growth rows: %w", err)
	}

	return ClusterGrowthReport{
		GeneratedAt: time.Now().UTC(),
		MartView:    martView,
		Rows:        out,
	}, nil
}

func clusterGrowthSQL(martView string) string {
	return strings.TrimSpace(fmt.Sprintf(`
SELECT
    TO_CHAR(DATE_TRUNC('month', CREATION_TIMESTAMP), 'YYYY-MM') AS month,
    PRODUCT_TYPE                                                 AS product_type,
    COUNT(DISTINCT EXTERNAL_ID)                                  AS new_this_month
FROM %s
WHERE CLUSTER_ROLE IN ('worker', 'standalone')
  AND CREATION_TIMESTAMP IS NOT NULL
GROUP BY 1, 2
ORDER BY 1, 2`, martView))
}
