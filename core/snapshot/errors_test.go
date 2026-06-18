package snapshot

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
)

type flakyEBSLister struct {
	failRegions map[string]error
	records     []Record
}

func (f flakyEBSLister) ListEBSSnapshots(
	_ context.Context,
	_ aws.Config,
	region, accountID string,
	_ time.Time,
	_ float64,
) ([]Record, error) {
	if err, ok := f.failRegions[region]; ok {
		return nil, err
	}
	out := make([]Record, 0, len(f.records))
	for _, rec := range f.records {
		if rec.Region == region && rec.AccountID == accountID {
			out = append(out, rec)
		}
	}
	return out, nil
}

func TestFetchContinuesWhenRegionFails(t *testing.T) {
	timeoutErr := fmt.Errorf(`describe ebs snapshots in me-south-1: operation error EC2: DescribeSnapshots, exceeded maximum number of attempts, 3, request send failed, Post "https://ec2.me-south-1.amazonaws.com/": dial tcp 99.82.136.87:443: i/o timeout`)
	ebs := Record{
		AccountID:  "111111111111",
		Region:     "us-east-1",
		Kind:       KindEBSSnapshot,
		ResourceID: "snap-old",
	}

	result, err := Fetch(context.Background(), Query{
		Targets:   []AccountTarget{{AccountID: "111111111111"}},
		OlderThan: 180 * 24 * time.Hour,
		Types:     []Kind{KindEBSSnapshot},
		regionLister: fakeRegionLister{regions: []string{"us-east-1", "me-south-1"}},
		ebsLister: flakyEBSLister{
			failRegions: map[string]error{"me-south-1": timeoutErr},
			records:     []Record{ebs},
		},
		rdsLister: fakeRDSLister{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Records) != 1 {
		t.Fatalf("records = %d, want 1", len(result.Records))
	}
	if len(result.Summary.SkippedRegions) != 1 {
		t.Fatalf("skipped = %#v", result.Summary.SkippedRegions)
	}
	if result.Summary.SkippedRegions[0].Region != "me-south-1" {
		t.Fatalf("skipped region = %q", result.Summary.SkippedRegions[0].Region)
	}
}

func TestFetchFailsWhenAllRegionsFail(t *testing.T) {
	errTimeout := errors.New("dial tcp: i/o timeout")
	_, err := Fetch(context.Background(), Query{
		Targets:   []AccountTarget{{AccountID: "111111111111"}},
		OlderThan: 180 * 24 * time.Hour,
		Types:     []Kind{KindEBSSnapshot},
		regionLister: fakeRegionLister{regions: []string{"me-south-1"}},
		ebsLister: flakyEBSLister{
			failRegions: map[string]error{"me-south-1": errTimeout},
		},
		rdsLister: fakeRDSLister{},
	})
	if err == nil {
		t.Fatal("expected error when all regions fail")
	}
}

func TestRegionErrorMessage(t *testing.T) {
	err := fmt.Errorf(`describe ebs snapshots in me-south-1: dial tcp: i/o timeout`)
	got := regionErrorMessage(err)
	if got != "i/o timeout" {
		t.Fatalf("message = %q", got)
	}
}

func TestIsSkippableRegionError(t *testing.T) {
	var timeout net.Error = fakeTimeoutError{}
	if !isSkippableRegionError(timeout) {
		t.Fatal("expected timeout to be skippable")
	}
	if isSkippableRegionError(context.Canceled) {
		t.Fatal("expected context.Canceled not to be skippable")
	}
}

type fakeTimeoutError struct{}

func (fakeTimeoutError) Error() string   { return "timeout" }
func (fakeTimeoutError) Timeout() bool   { return true }
func (fakeTimeoutError) Temporary() bool { return true }
