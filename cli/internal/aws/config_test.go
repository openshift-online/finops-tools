package aws

import (
	"context"
	"testing"
)

func TestLoadConfigFromSession(t *testing.T) {
	cfg, err := LoadConfigFromSession(context.Background(), ProfileSession{
		AccessKeyID:     "AK",
		SecretAccessKey: "SK",
		SessionToken:    "ST",
		Region:          "eu-west-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Region != "eu-west-1" {
		t.Fatalf("region = %q, want eu-west-1", cfg.Region)
	}
}

func TestLoadConfigFromSessionRejectsIncomplete(t *testing.T) {
	_, err := LoadConfigFromSession(context.Background(), ProfileSession{
		AccessKeyID: "AK",
	})
	if err == nil {
		t.Fatal("expected error for incomplete session")
	}
}
