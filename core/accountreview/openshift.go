package accountreview

import (
	"fmt"
	"strings"

	"github.com/openshift-online/finops-tools/core/ocmmapping"
)

// ApplyOpenShiftClusters attaches OCM lookup results to each report.
// When lookupErr is non-nil, every report gets OpenShiftClustersError and
// OpenShiftClusters stays nil (not queried / incomplete).
// On success, each report gets a non-nil OpenShiftClusters slice (empty when
// the account has no clusters) so EmptyInventoryTitles can treat zeros as scanned.
func ApplyOpenShiftClusters(reports []AccountReport, byAccount map[string][]ocmmapping.Cluster, lookupErr error) {
	if lookupErr != nil {
		msg := strings.TrimSpace(lookupErr.Error())
		if msg == "" {
			msg = "openshift cluster lookup failed"
		}
		for i := range reports {
			reports[i].OpenShiftClusters = nil
			reports[i].OpenShiftClustersError = msg
		}
		return
	}
	if byAccount == nil {
		byAccount = map[string][]ocmmapping.Cluster{}
	}
	for i := range reports {
		src := byAccount[reports[i].AccountID]
		out := make([]OpenShiftClusterDetail, len(src))
		for j, c := range src {
			out[j] = OpenShiftClusterDetail{
				Environment:      c.Environment,
				Name:             c.ClusterName,
				ClusterID:        c.ClusterID,
				ProductType:      c.ProductType,
				State:            c.State,
				Region:           c.Region,
				OpenShiftVersion: c.OpenShiftVersion,
			}
		}
		reports[i].OpenShiftClusters = out
		reports[i].OpenShiftClustersError = ""
	}
}

// ApplyOpenShiftClustersError is a convenience for soft-skip paths that never queried.
func ApplyOpenShiftClustersError(reports []AccountReport, err error) {
	if err == nil {
		ApplyOpenShiftClusters(reports, nil, fmt.Errorf("openshift cluster lookup skipped"))
		return
	}
	ApplyOpenShiftClusters(reports, nil, err)
}
