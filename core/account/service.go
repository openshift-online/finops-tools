package account

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/organizations"
	orgtypes "github.com/aws/aws-sdk-go-v2/service/organizations/types"
	"github.com/openshift-online/finops-tools/core/apilog"
)

// OrganizationsAPI is the subset of Organizations used by core account operations.
type OrganizationsAPI interface {
	DescribeAccount(
		ctx context.Context,
		params *organizations.DescribeAccountInput,
		optFns ...func(*organizations.Options),
	) (*organizations.DescribeAccountOutput, error)
	ListAccounts(
		ctx context.Context,
		params *organizations.ListAccountsInput,
		optFns ...func(*organizations.Options),
	) (*organizations.ListAccountsOutput, error)
	ListTagsForAccount(
		ctx context.Context,
		accountID string,
		nextToken *string,
	) ([]Tag, *string, error)
	SetAccountTag(
		ctx context.Context,
		accountID, tagKey, tagValue string,
	) error
	DescribeOrganization(
		ctx context.Context,
		params *organizations.DescribeOrganizationInput,
		optFns ...func(*organizations.Options),
	) (*organizations.DescribeOrganizationOutput, error)
	ListRoots(
		ctx context.Context,
		params *organizations.ListRootsInput,
		optFns ...func(*organizations.Options),
	) (*organizations.ListRootsOutput, error)
	ListOrganizationalUnitsForParent(
		ctx context.Context,
		params *organizations.ListOrganizationalUnitsForParentInput,
		optFns ...func(*organizations.Options),
	) (*organizations.ListOrganizationalUnitsForParentOutput, error)
	ListAccountsForParent(
		ctx context.Context,
		params *organizations.ListAccountsForParentInput,
		optFns ...func(*organizations.Options),
	) (*organizations.ListAccountsForParentOutput, error)
}

type organizationsClientFactory func(aws.Config) OrganizationsAPI

func newOrganizationsClient(cfg aws.Config) OrganizationsAPI {
	client := organizations.NewFromConfig(cfg, func(o *organizations.Options) {
		o.Region = organizationsRegion
	})
	return organizationsClient{client: client}
}

type organizationsClient struct {
	client *organizations.Client
}

func (c organizationsClient) DescribeAccount(
	ctx context.Context,
	params *organizations.DescribeAccountInput,
	optFns ...func(*organizations.Options),
) (*organizations.DescribeAccountOutput, error) {
	apilog.Log(ctx, fmt.Sprintf("Organizations.DescribeAccount account=%s", aws.ToString(params.AccountId)))
	return c.client.DescribeAccount(ctx, params, optFns...)
}

func (c organizationsClient) ListAccounts(
	ctx context.Context,
	params *organizations.ListAccountsInput,
	optFns ...func(*organizations.Options),
) (*organizations.ListAccountsOutput, error) {
	if params.NextToken != nil && aws.ToString(params.NextToken) != "" {
		apilog.Log(ctx, "Organizations.ListAccounts page=next")
	} else {
		apilog.Log(ctx, "Organizations.ListAccounts")
	}
	return c.client.ListAccounts(ctx, params, optFns...)
}

func (c organizationsClient) ListTagsForAccount(
	ctx context.Context,
	accountID string,
	nextToken *string,
) ([]Tag, *string, error) {
	if nextToken != nil && aws.ToString(nextToken) != "" {
		apilog.Log(ctx, fmt.Sprintf("Organizations.ListTagsForResource account=%s page=next", accountID))
	} else {
		apilog.Log(ctx, fmt.Sprintf("Organizations.ListTagsForResource account=%s", accountID))
	}
	out, err := c.client.ListTagsForResource(ctx, &organizations.ListTagsForResourceInput{
		ResourceId: aws.String(accountID),
		NextToken:  nextToken,
	})
	if err != nil {
		return nil, nil, err
	}
	tags := make([]Tag, 0, len(out.Tags))
	for _, tag := range out.Tags {
		key := strings.TrimSpace(aws.ToString(tag.Key))
		if key == "" {
			continue
		}
		tags = append(tags, Tag{
			Key:   key,
			Value: strings.TrimSpace(aws.ToString(tag.Value)),
		})
	}
	return tags, out.NextToken, nil
}

func (c organizationsClient) SetAccountTag(
	ctx context.Context,
	accountID, tagKey, tagValue string,
) error {
	apilog.Log(ctx, fmt.Sprintf("Organizations.TagResource account=%s key=%s", accountID, tagKey))
	_, err := c.client.TagResource(ctx, &organizations.TagResourceInput{
		ResourceId: aws.String(accountID),
		Tags: []orgtypes.Tag{
			{
				Key:   aws.String(tagKey),
				Value: aws.String(tagValue),
			},
		},
	})
	return err
}

func (c organizationsClient) DescribeOrganization(
	ctx context.Context,
	params *organizations.DescribeOrganizationInput,
	optFns ...func(*organizations.Options),
) (*organizations.DescribeOrganizationOutput, error) {
	apilog.Log(ctx, "Organizations.DescribeOrganization")
	return c.client.DescribeOrganization(ctx, params, optFns...)
}

func (c organizationsClient) ListRoots(
	ctx context.Context,
	params *organizations.ListRootsInput,
	optFns ...func(*organizations.Options),
) (*organizations.ListRootsOutput, error) {
	if params.NextToken != nil && aws.ToString(params.NextToken) != "" {
		apilog.Log(ctx, "Organizations.ListRoots page=next")
	} else {
		apilog.Log(ctx, "Organizations.ListRoots")
	}
	return c.client.ListRoots(ctx, params, optFns...)
}

func (c organizationsClient) ListOrganizationalUnitsForParent(
	ctx context.Context,
	params *organizations.ListOrganizationalUnitsForParentInput,
	optFns ...func(*organizations.Options),
) (*organizations.ListOrganizationalUnitsForParentOutput, error) {
	if params.NextToken != nil && aws.ToString(params.NextToken) != "" {
		apilog.Log(ctx, fmt.Sprintf("Organizations.ListOrganizationalUnitsForParent parent=%s page=next", aws.ToString(params.ParentId)))
	} else {
		apilog.Log(ctx, fmt.Sprintf("Organizations.ListOrganizationalUnitsForParent parent=%s", aws.ToString(params.ParentId)))
	}
	return c.client.ListOrganizationalUnitsForParent(ctx, params, optFns...)
}

func (c organizationsClient) ListAccountsForParent(
	ctx context.Context,
	params *organizations.ListAccountsForParentInput,
	optFns ...func(*organizations.Options),
) (*organizations.ListAccountsForParentOutput, error) {
	if params.NextToken != nil && aws.ToString(params.NextToken) != "" {
		apilog.Log(ctx, fmt.Sprintf("Organizations.ListAccountsForParent parent=%s page=next", aws.ToString(params.ParentId)))
	} else {
		apilog.Log(ctx, fmt.Sprintf("Organizations.ListAccountsForParent parent=%s", aws.ToString(params.ParentId)))
	}
	return c.client.ListAccountsForParent(ctx, params, optFns...)
}
