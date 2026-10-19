// Package ocmmapping looks up OpenShift clusters by AWS account ID from the
// HCMFINOPS OCM mart in Snowflake (Dataverse).
package ocmmapping

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/openshift-online/finops-tools/core/snowflake"
	"github.com/openshift-online/finops-tools/core/sqlrows"
)

const defaultMartTable = "HCMFINOPS_DB.MARTS.OCM_MAPPING"

// Cluster is one OpenShift cluster row from OCM_MAPPING for an AWS account.
type Cluster struct {
	Environment      string
	ClusterName      string
	ClusterID        string
	ProductType      string
	State            string
	Region           string
	OpenShiftVersion string
	AWSAccountID     string
}

// RowQuerier executes a SQL query and returns iterable rows.
type RowQuerier interface {
	QueryContext(ctx context.Context, query string, args ...any) (sqlrows.Rows, error)
}

// LookupByAWSAccounts returns clusters grouped by AWS account ID for the given
// accounts. All ENVIRONMENT values (Production, Stage, Integration) are included.
// Only AWS cloud_provider rows are returned. Pass martTable = "" for the default
// HCMFINOPS_DB.MARTS.OCM_MAPPING table.
func LookupByAWSAccounts(ctx context.Context, querier RowQuerier, accountIDs []string, martTable string) (map[string][]Cluster, error) {
	if querier == nil {
		return nil, fmt.Errorf("querier is required")
	}
	ids := uniqueNonEmpty(accountIDs)
	if len(ids) == 0 {
		return map[string][]Cluster{}, nil
	}
	if martTable == "" {
		martTable = defaultMartTable
	}
	sqlText, args, err := buildLookupSQL(martTable, ids)
	if err != nil {
		return nil, err
	}
	rows, err := querier.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, fmt.Errorf("ocm mapping query: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	out := make(map[string][]Cluster, len(ids))
	for rows.Next() {
		var (
			environment, clusterName, clusterID, productType *string
			state, region, openShiftVersion, awsAccountID    *string
		)
		if scanErr := rows.Scan(
			&environment,
			&clusterName,
			&clusterID,
			&productType,
			&state,
			&region,
			&openShiftVersion,
			&awsAccountID,
		); scanErr != nil {
			return nil, fmt.Errorf("scan ocm mapping row: %w", scanErr)
		}
		c := Cluster{
			Environment:      stringOrEmpty(environment),
			ClusterName:      stringOrEmpty(clusterName),
			ClusterID:        stringOrEmpty(clusterID),
			ProductType:      stringOrEmpty(productType),
			State:            stringOrEmpty(state),
			Region:           stringOrEmpty(region),
			OpenShiftVersion: stringOrEmpty(openShiftVersion),
			AWSAccountID:     strings.TrimSpace(stringOrEmpty(awsAccountID)),
		}
		if c.AWSAccountID == "" {
			continue
		}
		out[c.AWSAccountID] = append(out[c.AWSAccountID], c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ocm mapping rows: %w", err)
	}
	for accountID, clusters := range out {
		sort.SliceStable(clusters, func(i, j int) bool {
			if clusters[i].Environment != clusters[j].Environment {
				return clusters[i].Environment < clusters[j].Environment
			}
			if clusters[i].ClusterName != clusters[j].ClusterName {
				return clusters[i].ClusterName < clusters[j].ClusterName
			}
			return clusters[i].ClusterID < clusters[j].ClusterID
		})
		out[accountID] = clusters
	}
	return out, nil
}

func buildLookupSQL(martTable string, accountIDs []string) (string, []any, error) {
	if err := snowflake.ValidateQualifiedIdentifier(martTable, 3); err != nil {
		return "", nil, fmt.Errorf("invalid mart table: %w", err)
	}
	placeholders := make([]string, len(accountIDs))
	args := make([]any, len(accountIDs))
	for i, id := range accountIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	sqlText := fmt.Sprintf(`
SELECT
    ENVIRONMENT,
    CLUSTER_NAME,
    CLUSTER_ID,
    PRODUCT_TYPE,
    STATE,
    REGION,
    OPENSHIFT_VERSION,
    AWS_ACCOUNT_ID
FROM %s
WHERE AWS_ACCOUNT_ID IN (%s)
  AND LOWER(CLOUD_PROVIDER) = 'aws'
ORDER BY ENVIRONMENT, CLUSTER_NAME, CLUSTER_ID`, martTable, strings.Join(placeholders, ", "))
	return strings.TrimSpace(sqlText), args, nil
}

func stringOrEmpty(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func uniqueNonEmpty(ids []string) []string {
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
