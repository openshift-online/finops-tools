package apilog

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/opensearch"
)

type openSearchAPI interface {
	ListDomainNames(context.Context, *opensearch.ListDomainNamesInput, ...func(*opensearch.Options)) (*opensearch.ListDomainNamesOutput, error)
	DescribeDomain(context.Context, *opensearch.DescribeDomainInput, ...func(*opensearch.Options)) (*opensearch.DescribeDomainOutput, error)
	ListTags(context.Context, *opensearch.ListTagsInput, ...func(*opensearch.Options)) (*opensearch.ListTagsOutput, error)
}

type loggingOpenSearch struct{ inner openSearchAPI }

// WrapOpenSearch logs discovery calls when the context has an API logger.
func WrapOpenSearch(inner openSearchAPI) openSearchAPI {
	if inner == nil {
		return nil
	}
	return loggingOpenSearch{inner: inner}
}

func (l loggingOpenSearch) ListDomainNames(ctx context.Context, in *opensearch.ListDomainNamesInput, opts ...func(*opensearch.Options)) (*opensearch.ListDomainNamesOutput, error) {
	Log(ctx, "OpenSearch.ListDomainNames")
	return l.inner.ListDomainNames(ctx, in, opts...)
}

func (l loggingOpenSearch) DescribeDomain(ctx context.Context, in *opensearch.DescribeDomainInput, opts ...func(*opensearch.Options)) (*opensearch.DescribeDomainOutput, error) {
	Log(ctx, fmt.Sprintf("OpenSearch.DescribeDomain name=%s", aws.ToString(in.DomainName)))
	return l.inner.DescribeDomain(ctx, in, opts...)
}

func (l loggingOpenSearch) ListTags(ctx context.Context, in *opensearch.ListTagsInput, opts ...func(*opensearch.Options)) (*opensearch.ListTagsOutput, error) {
	Log(ctx, fmt.Sprintf("OpenSearch.ListTags resource=%s", aws.ToString(in.ARN)))
	return l.inner.ListTags(ctx, in, opts...)
}
