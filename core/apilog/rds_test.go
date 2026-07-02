package apilog

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
)

func TestFormatRDSCall(t *testing.T) {
	got := formatRDSCall("RDS.DescribeDBSnapshots", aws.String("marker"))
	if got != "RDS.DescribeDBSnapshots page=next" {
		t.Fatalf("got %q", got)
	}
	got = formatRDSCall("RDS.DescribeDBInstances", nil)
	if got != "RDS.DescribeDBInstances" {
		t.Fatalf("got %q", got)
	}
}
