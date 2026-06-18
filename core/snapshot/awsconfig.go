package snapshot

import "github.com/aws/aws-sdk-go-v2/aws"

const defaultAPIRegion = "us-east-1"

func awsConfigWithDefaultRegion(cfg aws.Config) aws.Config {
	if cfg.Region != "" {
		return cfg
	}
	out := cfg.Copy()
	out.Region = defaultAPIRegion
	return out
}
