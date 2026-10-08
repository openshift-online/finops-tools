package inventory

import (
	"strings"
	"testing"
)

func TestCoalesceRegionWarningsDropsServiceNoiseOnTimeout(t *testing.T) {
	t.Parallel()
	got := coalesceRegionWarnings([]RegionWarning{
		{Region: "me-south-1", Message: "ec2: instances: operation error EC2: DescribeInstances, context deadline exceeded (Client.Timeout exceeded while awaiting headers)"},
		{Region: "me-south-1", Message: "scan timed out after 45s"},
		{Region: "us-east-1", Message: "ec2: access denied"},
	})
	if len(got) != 2 {
		t.Fatalf("got = %+v", got)
	}
	var meSouth, usEast string
	for _, w := range got {
		switch w.Region {
		case "me-south-1":
			meSouth = w.Message
		case "us-east-1":
			usEast = w.Message
		}
	}
	if meSouth != "scan timed out after 45s" {
		t.Fatalf("me-south-1 = %q", meSouth)
	}
	if strings.Contains(meSouth, "ec2:") {
		t.Fatalf("timeout region kept service noise: %q", meSouth)
	}
	if usEast != "ec2: access denied" {
		t.Fatalf("us-east-1 = %q", usEast)
	}
}

func TestCoalesceRegionWarningsNoTimeoutUnchanged(t *testing.T) {
	t.Parallel()
	in := []RegionWarning{
		{Region: "us-west-2", Message: "ec2: access denied"},
		{Region: "us-west-2", Message: "rds: throttled"},
	}
	got := coalesceRegionWarnings(in)
	if len(got) != 2 {
		t.Fatalf("got = %+v", got)
	}
}
