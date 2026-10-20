// Package telesense fetches per-cluster, per-month vCPU-hour usage from the
// Telesense Snowflake mart (TELESENSE_DB.OPENSHIFT_MARTS.OCP_USAGE). Results
// are returned entirely in memory; callers join them to hcphierarchy rows by
// CLUSTER_ID == HierarchyRow.CustomerClusterID (OCM's EXTERNAL_ID) in
// application code. OCP_USAGE lives in a different Snowflake account than
// OCM_MAPPING, so a cross-database SQL join is never attempted here — see
// golang-executive-summary-spec.md sections 2 and 11.1.
package telesense

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/openshift-online/finops-tools/core/snowflake"
	"github.com/openshift-online/finops-tools/core/sqlrows"
)

const (
	defaultUsageView = "TELESENSE_DB.OPENSHIFT_MARTS.OCP_USAGE"
	// batchSize caps the number of cluster IDs per IN(...) clause, matching the
	// ~4,000-ID guidance in golang-executive-summary-spec.md section 5 step 5.
	batchSize = 4000
)

// SnowflakeQueryer executes a SQL query and returns iterable rows.
// *sql.DB satisfies this interface via a thin adapter; inject a mock in tests.
// Call Close when finished to release the underlying connection pool.
type SnowflakeQueryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (SnowflakeRows, error)
	Close() error
}

// SnowflakeRows mirrors database/sql.Rows.
type SnowflakeRows = sqlrows.Rows

// ClusterMonthUsage is total vCPU-hours for one cluster in one calendar month.
type ClusterMonthUsage struct {
	// ClusterID is the Telesense CLUSTER_ID — matches
	// hcphierarchy.HierarchyRow.CustomerClusterID (OCM's EXTERNAL_ID).
	ClusterID string
	// Month is a YYYY-MM key.
	Month string
	// VCPUHours is SUM(WORKER_VCPUS_AVG * 24) for the month; NULL treated as 0.
	VCPUHours float64
}

// Report holds all resolved vCPU-hour usage rows. Entirely in-memory; nothing
// is written to disk.
type Report struct {
	GeneratedAt time.Time
	UsageView   string
	Rows        []ClusterMonthUsage
}

// ByClusterMonth indexes Rows as map[clusterID]map[month]vcpuHours for O(1)
// in-memory-join lookups against an hcphierarchy.Report (never a SQL join —
// see package doc).
func (r Report) ByClusterMonth() map[string]map[string]float64 {
	out := make(map[string]map[string]float64, len(r.Rows))
	for _, row := range r.Rows {
		months, ok := out[row.ClusterID]
		if !ok {
			months = make(map[string]float64)
			out[row.ClusterID] = months
		}
		months[row.Month] += row.VCPUHours
	}
	return out
}

// TotalVCPUHours sums vCPU-hours across all clusters for one YYYY-MM month.
func (r Report) TotalVCPUHours(month string) float64 {
	var total float64
	for _, row := range r.Rows {
		if row.Month == month {
			total += row.VCPUHours
		}
	}
	return total
}

// Build fetches vCPU-hours from OCP_USAGE for the given cluster external IDs
// within [windowStart, windowEnd) (windowEnd exclusive), batching the
// IN (...) clause across multiple queries when externalIDs is large so each
// statement stays within practical SQL size limits. Pass usageView = "" to
// use the default RHPROD Telesense mart location.
func Build(
	ctx context.Context,
	sf SnowflakeQueryer,
	externalIDs []string,
	windowStart, windowEnd time.Time,
	usageView string,
) (Report, error) {
	if usageView == "" {
		usageView = defaultUsageView
	}
	if err := snowflake.ValidateQualifiedIdentifier(usageView, 3); err != nil {
		return Report{}, fmt.Errorf("invalid usage view: %w", err)
	}
	if !windowStart.Before(windowEnd) {
		return Report{}, fmt.Errorf(
			"windowStart (%s) must be before windowEnd (%s)",
			windowStart.Format("2006-01-02"), windowEnd.Format("2006-01-02"),
		)
	}

	report := Report{
		GeneratedAt: time.Now().UTC(),
		UsageView:   usageView,
	}

	ids := dedupeNonEmpty(externalIDs)
	if len(ids) == 0 {
		return report, nil
	}

	for _, batch := range batchStrings(ids, batchSize) {
		rows, err := fetchUsageBatch(ctx, sf, usageView, batch, windowStart, windowEnd)
		if err != nil {
			return Report{}, err
		}
		report.Rows = append(report.Rows, rows...)
	}

	return report, nil
}

func fetchUsageBatch(
	ctx context.Context,
	sf SnowflakeQueryer,
	usageView string,
	ids []string,
	windowStart, windowEnd time.Time,
) ([]ClusterMonthUsage, error) {
	query, args := buildUsageSQL(usageView, ids, windowStart, windowEnd)
	rows, err := sf.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("ocp usage query: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	var out []ClusterMonthUsage
	for rows.Next() {
		var (
			clusterID, month string
			vcpuHours        *float64
		)
		if err := rows.Scan(&clusterID, &month, &vcpuHours); err != nil {
			return nil, fmt.Errorf("scan ocp usage row: %w", err)
		}
		u := ClusterMonthUsage{ClusterID: clusterID, Month: month}
		if vcpuHours != nil {
			u.VCPUHours = *vcpuHours
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ocp usage rows: %w", err)
	}
	return out, nil
}

// buildUsageSQL returns parameterized SQL and bind args for one batch of
// cluster IDs. usageView must already be validated (it is interpolated
// directly since Snowflake object names cannot be bound as query
// parameters); dates and cluster IDs are always passed as bind args.
// NULL WORKER_VCPUS_AVG values are summed as 0 by SUM (missing telemetry
// days simply don't contribute).
func buildUsageSQL(usageView string, ids []string, windowStart, windowEnd time.Time) (string, []any) {
	placeholders := make([]string, len(ids))
	args := make([]any, 0, len(ids)+2)
	args = append(args, windowStart.Format("2006-01-02"), windowEnd.Format("2006-01-02"))
	for i, id := range ids {
		placeholders[i] = "?"
		args = append(args, id)
	}

	query := fmt.Sprintf(`
SELECT
    CLUSTER_ID                                          AS cluster_id,
    TO_CHAR(DATE_TRUNC('month', TELE_DATE), 'YYYY-MM')  AS month,
    SUM(WORKER_VCPUS_AVG * 24)                          AS vcpu_hours
FROM %s
WHERE TELE_DATE >= ?
  AND TELE_DATE <  ?
  AND CLUSTER_ID IN (%s)
GROUP BY 1, 2
ORDER BY 2, 1`, usageView, strings.Join(placeholders, ", "))

	return strings.TrimSpace(query), args
}

// dedupeNonEmpty trims, drops empty, deduplicates, and sorts IDs so batching
// and generated SQL are deterministic (useful for tests and query caching).
func dedupeNonEmpty(ids []string) []string {
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
	sort.Strings(out)
	return out
}

func batchStrings(values []string, size int) [][]string {
	if size <= 0 || len(values) == 0 {
		return nil
	}
	out := make([][]string, 0, (len(values)+size-1)/size)
	for i := 0; i < len(values); i += size {
		end := i + size
		if end > len(values) {
			end = len(values)
		}
		out = append(out, values[i:end])
	}
	return out
}
