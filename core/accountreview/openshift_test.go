package accountreview

import (
	"errors"
	"testing"

	"github.com/openshift-online/finops-tools/core/ocmmapping"
)

func TestApplyOpenShiftClustersSuccess(t *testing.T) {
	t.Parallel()
	reports := []AccountReport{
		{AccountID: "111111111111"},
		{AccountID: "222222222222"},
	}
	ApplyOpenShiftClusters(reports, map[string][]ocmmapping.Cluster{
		"111111111111": {{
			Environment: "Stage",
			ClusterName: "stage-a",
			ClusterID:   "ocm-a",
			ProductType: "OSD",
			State:       "ready",
			Region:      "us-east-1",
		}},
	}, nil)
	if reports[0].OpenShiftClustersError != "" || len(reports[0].OpenShiftClusters) != 1 {
		t.Fatalf("report0 = %+v", reports[0])
	}
	if reports[0].OpenShiftClusters[0].Name != "stage-a" {
		t.Fatalf("name = %q", reports[0].OpenShiftClusters[0].Name)
	}
	if reports[1].OpenShiftClusters == nil || len(reports[1].OpenShiftClusters) != 0 {
		t.Fatalf("report1 should be non-nil empty, got %#v", reports[1].OpenShiftClusters)
	}
}

func TestApplyOpenShiftClustersError(t *testing.T) {
	t.Parallel()
	reports := []AccountReport{{AccountID: "111111111111", OpenShiftClusters: []OpenShiftClusterDetail{{Name: "keep-me"}}}}
	ApplyOpenShiftClusters(reports, nil, errors.New("token expired"))
	if reports[0].OpenShiftClusters != nil {
		t.Fatalf("clusters should be cleared, got %#v", reports[0].OpenShiftClusters)
	}
	if reports[0].OpenShiftClustersError != "token expired" {
		t.Fatalf("error = %q", reports[0].OpenShiftClustersError)
	}
}
