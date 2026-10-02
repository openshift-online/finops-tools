package inventory

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing"
	elbtypes "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	r53types "github.com/aws/aws-sdk-go-v2/service/route53/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type fakeELBv2 struct {
	names []string
	err   error
}

func (f fakeELBv2) DescribeLoadBalancers(
	context.Context,
	*elasticloadbalancingv2.DescribeLoadBalancersInput,
	...func(*elasticloadbalancingv2.Options),
) (*elasticloadbalancingv2.DescribeLoadBalancersOutput, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := &elasticloadbalancingv2.DescribeLoadBalancersOutput{}
	for _, name := range f.names {
		n := name
		out.LoadBalancers = append(out.LoadBalancers, elbv2types.LoadBalancer{
			LoadBalancerName: &n,
			Type:             elbv2types.LoadBalancerTypeEnumApplication,
		})
	}
	return out, nil
}

type fakeClassicELB struct {
	names []string
	err   error
}

func (f fakeClassicELB) DescribeLoadBalancers(
	context.Context,
	*elasticloadbalancing.DescribeLoadBalancersInput,
	...func(*elasticloadbalancing.Options),
) (*elasticloadbalancing.DescribeLoadBalancersOutput, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := &elasticloadbalancing.DescribeLoadBalancersOutput{}
	for _, name := range f.names {
		n := name
		out.LoadBalancerDescriptions = append(out.LoadBalancerDescriptions, elbtypes.LoadBalancerDescription{
			LoadBalancerName: &n,
		})
	}
	return out, nil
}

func TestListLoadBalancersKeepsV2WhenClassicFails(t *testing.T) {
	t.Parallel()
	got, err := listLoadBalancers(context.Background(), fakeELBv2{names: []string{"alb-1"}}, fakeClassicELB{err: fmt.Errorf("classic denied")}, "us-east-1")
	if err == nil {
		t.Fatal("expected classic error")
	}
	if len(got) != 1 || got[0].Name != "alb-1" {
		t.Fatalf("got %+v, want alb-1", got)
	}
}

func TestListLoadBalancersKeepsClassicWhenV2Fails(t *testing.T) {
	t.Parallel()
	got, err := listLoadBalancers(context.Background(), fakeELBv2{err: fmt.Errorf("v2 denied")}, fakeClassicELB{names: []string{"classic-1"}}, "us-east-1")
	if err == nil {
		t.Fatal("expected v2 error")
	}
	if len(got) != 1 || got[0].Name != "classic-1" {
		t.Fatalf("got %+v, want classic-1", got)
	}
}

type fakeS3 struct {
	buckets       []s3types.Bucket
	pageSize      int
	listErr       error
	listErrAfter  int // succeed this many ListBuckets calls, then return listErr
	locations     map[string]s3types.BucketLocationConstraint
	locationErrs  map[string]error
	locationCalls []string
	listCalls     int
}

func (f *fakeS3) ListBuckets(_ context.Context, params *s3.ListBucketsInput, _ ...func(*s3.Options)) (*s3.ListBucketsOutput, error) {
	if f.listErr != nil && (f.listErrAfter <= 0 || f.listCalls >= f.listErrAfter) {
		return nil, f.listErr
	}
	f.listCalls++
	start := 0
	if params != nil {
		if token := aws.ToString(params.ContinuationToken); token != "" {
			n, err := strconv.Atoi(token)
			if err != nil {
				return nil, err
			}
			start = n
		}
	}
	if start > len(f.buckets) {
		start = len(f.buckets)
	}
	pageSize := f.pageSize
	if pageSize <= 0 {
		pageSize = len(f.buckets) - start
	}
	end := start + pageSize
	if end > len(f.buckets) {
		end = len(f.buckets)
	}
	out := &s3.ListBucketsOutput{}
	if start < end {
		out.Buckets = f.buckets[start:end]
	}
	if end < len(f.buckets) {
		out.ContinuationToken = aws.String(strconv.Itoa(end))
	}
	return out, nil
}

func (f *fakeS3) GetBucketLocation(_ context.Context, params *s3.GetBucketLocationInput, _ ...func(*s3.Options)) (*s3.GetBucketLocationOutput, error) {
	name := aws.ToString(params.Bucket)
	f.locationCalls = append(f.locationCalls, name)
	if err := f.locationErrs[name]; err != nil {
		return nil, err
	}
	return &s3.GetBucketLocationOutput{LocationConstraint: f.locations[name]}, nil
}

func TestListS3BucketsEmptyConstraintIsUSEast1(t *testing.T) {
	t.Parallel()
	got, err := listS3Buckets(context.Background(), &fakeS3{
		buckets: []s3types.Bucket{{Name: aws.String("east")}},
	})
	if err != nil {
		t.Fatalf("listS3Buckets() error = %v", err)
	}
	if len(got) != 1 || got[0].Name != "east" || got[0].Region != "us-east-1" {
		t.Fatalf("got %+v, want east in us-east-1", got)
	}
}

func TestListS3BucketsUsesLocationConstraint(t *testing.T) {
	t.Parallel()
	got, err := listS3Buckets(context.Background(), &fakeS3{
		buckets: []s3types.Bucket{{Name: aws.String("eu-legacy")}, {Name: aws.String("west")}},
		locations: map[string]s3types.BucketLocationConstraint{
			"eu-legacy": s3types.BucketLocationConstraintEu,
			"west":      s3types.BucketLocationConstraintUsWest2,
		},
	})
	if err != nil {
		t.Fatalf("listS3Buckets() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d buckets, want 2", len(got))
	}
	if got[0].Region != "eu-west-1" || got[1].Region != "us-west-2" {
		t.Fatalf("regions = %+v", got)
	}
}

func TestListS3BucketsKeepsBucketWhenLocationFails(t *testing.T) {
	t.Parallel()
	got, err := listS3Buckets(context.Background(), &fakeS3{
		buckets: []s3types.Bucket{
			{Name: aws.String("ok"), BucketRegion: aws.String("eu-central-1")},
			{Name: aws.String("denied")},
		},
		locationErrs: map[string]error{"denied": fmt.Errorf("access denied")},
	})
	if err == nil || !strings.Contains(err.Error(), "denied") {
		t.Fatalf("error = %v, want denied", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %+v, want both buckets", got)
	}
	if got[0].Region != "eu-central-1" {
		t.Fatalf("listed region = %q", got[0].Region)
	}
	if got[1].Name != "denied" || got[1].Region != "" {
		t.Fatalf("failed bucket = %+v, want empty region", got[1])
	}
}

func TestListS3BucketsSkipsGetBucketLocationWhenListed(t *testing.T) {
	t.Parallel()
	fake := &fakeS3{
		buckets:      []s3types.Bucket{{Name: aws.String("ok"), BucketRegion: aws.String("ap-southeast-1")}},
		locationErrs: map[string]error{"ok": fmt.Errorf("should not be called")},
	}
	got, err := listS3Buckets(context.Background(), fake)
	if err != nil {
		t.Fatalf("listS3Buckets() error = %v", err)
	}
	if len(got) != 1 || got[0].Region != "ap-southeast-1" {
		t.Fatalf("got %+v", got)
	}
	if len(fake.locationCalls) != 0 {
		t.Fatalf("GetBucketLocation calls = %v, want none", fake.locationCalls)
	}
}

func TestListS3BucketsPaginates(t *testing.T) {
	t.Parallel()
	fake := &fakeS3{
		buckets: []s3types.Bucket{
			{Name: aws.String("a"), BucketRegion: aws.String("us-east-1")},
			{Name: aws.String("b"), BucketRegion: aws.String("us-west-2")},
			{Name: aws.String("c"), BucketRegion: aws.String("eu-west-1")},
		},
		pageSize: 1,
	}
	got, err := listS3Buckets(context.Background(), fake)
	if err != nil {
		t.Fatalf("listS3Buckets() error = %v", err)
	}
	if fake.listCalls != 3 {
		t.Fatalf("ListBuckets calls = %d, want 3", fake.listCalls)
	}
	if len(got) != 3 {
		t.Fatalf("got %d buckets, want 3", len(got))
	}
	if got[0].Name != "a" || got[1].Name != "b" || got[2].Name != "c" {
		t.Fatalf("got %+v", got)
	}
}

func TestListS3BucketsKeepsPageWhenLaterPageFails(t *testing.T) {
	t.Parallel()
	fake := &fakeS3{
		buckets: []s3types.Bucket{
			{Name: aws.String("a"), BucketRegion: aws.String("us-east-1")},
			{Name: aws.String("b"), BucketRegion: aws.String("us-west-2")},
		},
		pageSize:      1,
		listErrAfter:  1,
		listErr:       fmt.Errorf("throttled"),
	}
	got, err := listS3Buckets(context.Background(), fake)
	if err == nil || !strings.Contains(err.Error(), "throttled") {
		t.Fatalf("error = %v, want throttled", err)
	}
	if len(got) != 1 || got[0].Name != "a" {
		t.Fatalf("got %+v, want first page", got)
	}
}

type fakeRoute53 struct {
	pages   [][]string
	pageErr error
	calls   int
}

func (f *fakeRoute53) ListHostedZones(
	_ context.Context,
	_ *route53.ListHostedZonesInput,
	_ ...func(*route53.Options),
) (*route53.ListHostedZonesOutput, error) {
	idx := f.calls
	f.calls++
	if idx >= len(f.pages) {
		if f.pageErr != nil {
			return nil, f.pageErr
		}
		return &route53.ListHostedZonesOutput{}, nil
	}
	out := &route53.ListHostedZonesOutput{}
	for i, name := range f.pages[idx] {
		n := name
		id := fmt.Sprintf("/hostedzone/Z%d", idx*100+i)
		out.HostedZones = append(out.HostedZones, r53types.HostedZone{
			Id:   &id,
			Name: &n,
		})
	}
	if idx+1 < len(f.pages) || f.pageErr != nil {
		out.IsTruncated = true
		marker := "next"
		out.NextMarker = &marker
	}
	return out, nil
}

func (f *fakeRoute53) ListResourceRecordSets(
	context.Context,
	*route53.ListResourceRecordSetsInput,
	...func(*route53.Options),
) (*route53.ListResourceRecordSetsOutput, error) {
	return &route53.ListResourceRecordSetsOutput{}, nil
}

func TestListHostedZonesKeepsPageWhenLaterPageFails(t *testing.T) {
	t.Parallel()
	fake := &fakeRoute53{
		pages:   [][]string{{"a.example.", "b.example."}},
		pageErr: fmt.Errorf("throttled"),
	}
	got, err := listHostedZones(context.Background(), fake)
	if err == nil || !strings.Contains(err.Error(), "throttled") {
		t.Fatalf("error = %v, want throttled", err)
	}
	if len(got) != 2 || got[0].Name != "a.example." || got[1].Name != "b.example." {
		t.Fatalf("got %+v, want first page", got)
	}
}

type pagingELBv2 struct {
	pages   [][]string
	pageErr error
	calls   int
}

func (f *pagingELBv2) DescribeLoadBalancers(
	_ context.Context,
	_ *elasticloadbalancingv2.DescribeLoadBalancersInput,
	_ ...func(*elasticloadbalancingv2.Options),
) (*elasticloadbalancingv2.DescribeLoadBalancersOutput, error) {
	idx := f.calls
	f.calls++
	if idx >= len(f.pages) {
		if f.pageErr != nil {
			return nil, f.pageErr
		}
		return &elasticloadbalancingv2.DescribeLoadBalancersOutput{}, nil
	}
	out := &elasticloadbalancingv2.DescribeLoadBalancersOutput{}
	for _, name := range f.pages[idx] {
		n := name
		out.LoadBalancers = append(out.LoadBalancers, elbv2types.LoadBalancer{
			LoadBalancerName: &n,
			Type:             elbv2types.LoadBalancerTypeEnumApplication,
		})
	}
	if idx+1 < len(f.pages) || f.pageErr != nil {
		marker := "next"
		out.NextMarker = &marker
	}
	return out, nil
}

func TestListELBv2KeepsPageWhenLaterPageFails(t *testing.T) {
	t.Parallel()
	fake := &pagingELBv2{
		pages:   [][]string{{"alb-1"}},
		pageErr: fmt.Errorf("throttled"),
	}
	got, err := listELBv2LoadBalancers(context.Background(), fake, "us-east-1")
	if err == nil || !strings.Contains(err.Error(), "throttled") {
		t.Fatalf("error = %v, want throttled", err)
	}
	if len(got) != 1 || got[0].Name != "alb-1" {
		t.Fatalf("got %+v, want first page", got)
	}
}
