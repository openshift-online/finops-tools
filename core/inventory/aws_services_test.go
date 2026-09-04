package inventory

import (
	"context"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing"
	elbtypes "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
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
