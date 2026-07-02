package apilog

import (
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
)

func TestFormatDescribeSnapshots(t *testing.T) {
	got := formatDescribeSnapshots(&ec2.DescribeSnapshotsInput{
		OwnerIds:  []string{"self"},
		NextToken: aws.String("token"),
	})
	for _, want := range []string{
		"EC2.DescribeSnapshots",
		"owners=self",
		"page=next",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatDescribeSnapshots() = %q, missing %q", got, want)
		}
	}
}

func TestFormatDescribeRegions(t *testing.T) {
	got := formatDescribeRegions(&ec2.DescribeRegionsInput{
		AllRegions: aws.Bool(false),
	})
	if !strings.Contains(got, "EC2.DescribeRegions") || !strings.Contains(got, "allRegions=false") {
		t.Fatalf("formatDescribeRegions() = %q", got)
	}
}
