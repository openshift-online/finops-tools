package snapshot

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
)

func TestAWSConfigWithDefaultRegion(t *testing.T) {
	cfg := aws.Config{}
	got := awsConfigWithDefaultRegion(cfg)
	if got.Region != defaultAPIRegion {
		t.Fatalf("region = %q, want %q", got.Region, defaultAPIRegion)
	}

	cfg.Region = "eu-west-1"
	got = awsConfigWithDefaultRegion(cfg)
	if got.Region != "eu-west-1" {
		t.Fatalf("region = %q", got.Region)
	}
}
