package account

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/openshift-online/finops-tools/core/progress"
)

const (
	organizationsRegion      = "us-east-1"
	accountNameListThreshold = 50
)

// Tag is one AWS Organizations account tag.
type Tag struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// OrganizationAccount is one AWS Organizations account directory entry.
type OrganizationAccount struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// OrganizationalUnit is one AWS Organizations OU directory entry.
type OrganizationalUnit struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ListAccountsInOUOptions configures ListAccountsInOU.
type ListAccountsInOUOptions struct {
	// DirectOnly lists accounts directly in ouID only (not descendant OUs).
	DirectOnly bool
	// Status filters accounts by Organizations status (default ACTIVE).
	Status string
}

// OrganizationAccountTags is one organization account and its Organizations tags.
type OrganizationAccountTags struct {
	Account OrganizationAccount `json:"account"`
	Tags    []Tag               `json:"tags"`
}

// TagFilterProgress reports long-running steps while filtering accounts by tag.
type TagFilterProgress = progress.Reporter

// AccountKind describes whether a validated account session is payer or linked.
type AccountKind string

const (
	AccountKindPayer   AccountKind = "payer"
	AccountKindLinked  AccountKind = "linked"
	AccountKindUnknown AccountKind = "unknown"
)

// Query identifies a target account and the credentials used to query it.
type Query struct {
	AccountID string
	AWSConfig aws.Config
}
