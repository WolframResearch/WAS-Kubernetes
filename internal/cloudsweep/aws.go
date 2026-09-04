package cloudsweep

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing"
	elbtypes "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"

	"github.com/WolframResearch/WAS-Kubernetes/internal/runner"
)

// ── Client interfaces (narrow; allows mock injection in tests) ────────────────

type ec2API interface {
	DescribeAddresses(ctx context.Context, params *ec2.DescribeAddressesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeAddressesOutput, error)
	DescribeNetworkInterfaces(ctx context.Context, params *ec2.DescribeNetworkInterfacesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeNetworkInterfacesOutput, error)
	DescribeSecurityGroups(ctx context.Context, params *ec2.DescribeSecurityGroupsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeSecurityGroupsOutput, error)
	DescribeVpcEndpoints(ctx context.Context, params *ec2.DescribeVpcEndpointsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeVpcEndpointsOutput, error)
	ReleaseAddress(ctx context.Context, params *ec2.ReleaseAddressInput, optFns ...func(*ec2.Options)) (*ec2.ReleaseAddressOutput, error)
	DeleteNetworkInterface(ctx context.Context, params *ec2.DeleteNetworkInterfaceInput, optFns ...func(*ec2.Options)) (*ec2.DeleteNetworkInterfaceOutput, error)
	DeleteSecurityGroup(ctx context.Context, params *ec2.DeleteSecurityGroupInput, optFns ...func(*ec2.Options)) (*ec2.DeleteSecurityGroupOutput, error)
	DeleteVpcEndpoints(ctx context.Context, params *ec2.DeleteVpcEndpointsInput, optFns ...func(*ec2.Options)) (*ec2.DeleteVpcEndpointsOutput, error)
	RevokeSecurityGroupIngress(ctx context.Context, params *ec2.RevokeSecurityGroupIngressInput, optFns ...func(*ec2.Options)) (*ec2.RevokeSecurityGroupIngressOutput, error)
	RevokeSecurityGroupEgress(ctx context.Context, params *ec2.RevokeSecurityGroupEgressInput, optFns ...func(*ec2.Options)) (*ec2.RevokeSecurityGroupEgressOutput, error)
}

type elbv2API interface {
	DescribeLoadBalancers(ctx context.Context, params *elasticloadbalancingv2.DescribeLoadBalancersInput, optFns ...func(*elasticloadbalancingv2.Options)) (*elasticloadbalancingv2.DescribeLoadBalancersOutput, error)
	DescribeTargetGroups(ctx context.Context, params *elasticloadbalancingv2.DescribeTargetGroupsInput, optFns ...func(*elasticloadbalancingv2.Options)) (*elasticloadbalancingv2.DescribeTargetGroupsOutput, error)
	DescribeTags(ctx context.Context, params *elasticloadbalancingv2.DescribeTagsInput, optFns ...func(*elasticloadbalancingv2.Options)) (*elasticloadbalancingv2.DescribeTagsOutput, error)
	DeleteLoadBalancer(ctx context.Context, params *elasticloadbalancingv2.DeleteLoadBalancerInput, optFns ...func(*elasticloadbalancingv2.Options)) (*elasticloadbalancingv2.DeleteLoadBalancerOutput, error)
	DeleteTargetGroup(ctx context.Context, params *elasticloadbalancingv2.DeleteTargetGroupInput, optFns ...func(*elasticloadbalancingv2.Options)) (*elasticloadbalancingv2.DeleteTargetGroupOutput, error)
}

type elbClassicAPI interface {
	DescribeLoadBalancers(ctx context.Context, params *elasticloadbalancing.DescribeLoadBalancersInput, optFns ...func(*elasticloadbalancing.Options)) (*elasticloadbalancing.DescribeLoadBalancersOutput, error)
	DescribeTags(ctx context.Context, params *elasticloadbalancing.DescribeTagsInput, optFns ...func(*elasticloadbalancing.Options)) (*elasticloadbalancing.DescribeTagsOutput, error)
	DeleteLoadBalancer(ctx context.Context, params *elasticloadbalancing.DeleteLoadBalancerInput, optFns ...func(*elasticloadbalancing.Options)) (*elasticloadbalancing.DeleteLoadBalancerOutput, error)
}

// ── AWSSweeper ────────────────────────────────────────────────────────────────

// AWSSweeper sweeps orphaned Kubernetes-created AWS resources (NLBs, EIPs,
// ENIs, security groups) from a specific VPC/cluster before terraform destroy.
//
// Every deletion is gated by HARD SAFETY RULES:
//   - Resource must be in the target VPC
//   - Resource must carry kubernetes.io/cluster/<clusterName> = owned|shared
//   - ENIs use a 4-condition rule instead (see eniIsSweepable)
//   - DryRun=true prints decisions without calling delete APIs
type AWSSweeper struct {
	ec2c            ec2API
	elbv2           elbv2API
	elbc            elbClassicAPI
	opts            SweepOptions
	log             runner.Reporter
	vpcPollInterval time.Duration // pause between WaitForVPCClear iterations; defaults to 15s
}

// NewAWSSweeper creates a sweeper backed by real AWS clients in the given region.
func NewAWSSweeper(ctx context.Context, opts SweepOptions, rep runner.Reporter) (*AWSSweeper, error) {
	if opts.ClusterName == "" {
		return nil, fmt.Errorf(
			"SAFETY VIOLATION: sweep called without cluster name; refusing to proceed. " +
				"This is a bug — please report at https://github.com/WolframResearch/WAS-Kubernetes/issues")
	}
	if opts.VPCID == "" {
		return nil, fmt.Errorf("cloudsweep: VPCID is required")
	}
	cfg, err := awscfg.LoadDefaultConfig(ctx, awscfg.WithRegion(opts.Region))
	if err != nil {
		return nil, fmt.Errorf("cloudsweep: load AWS config: %w", err)
	}
	return newAWSSweeper(
		ec2.NewFromConfig(cfg),
		elasticloadbalancingv2.NewFromConfig(cfg),
		elasticloadbalancing.NewFromConfig(cfg),
		opts, rep,
	), nil
}

// NewAWSSweeperFromConfig creates a sweeper using a pre-loaded aws.Config.
// Use this when the caller has already authenticated once and wants to share
// the same credentials across multiple sweep operations without re-loading.
func NewAWSSweeperFromConfig(cfg aws.Config, opts SweepOptions, rep runner.Reporter) (*AWSSweeper, error) {
	if opts.ClusterName == "" {
		return nil, fmt.Errorf(
			"SAFETY VIOLATION: sweep called without cluster name; refusing to proceed. " +
				"This is a bug — please report at https://github.com/WolframResearch/WAS-Kubernetes/issues")
	}
	if opts.VPCID == "" {
		return nil, fmt.Errorf("cloudsweep: VPCID is required")
	}
	return newAWSSweeper(
		ec2.NewFromConfig(cfg),
		elasticloadbalancingv2.NewFromConfig(cfg),
		elasticloadbalancing.NewFromConfig(cfg),
		opts, rep,
	), nil
}

func newAWSSweeper(ec2c ec2API, elbv2 elbv2API, elbc elbClassicAPI, opts SweepOptions, rep runner.Reporter) *AWSSweeper {
	return &AWSSweeper{
		ec2c: ec2c, elbv2: elbv2, elbc: elbc, opts: opts, log: rep,
		vpcPollInterval: 15 * time.Second,
	}
}

// Sweep runs all orphan resource sweeps in sequence. Errors for individual
// resources are logged + recorded in the report but do not stop the sweep.
// An error is returned only if an AWS API call itself fails.
func (s *AWSSweeper) Sweep(ctx context.Context) (*SweepReport, error) {
	if s.opts.ClusterName == "" {
		return nil, fmt.Errorf(
			"SAFETY VIOLATION: sweep called without cluster name; refusing to proceed")
	}
	report := &SweepReport{}

	if err := s.sweepLoadBalancersV2(ctx, report); err != nil {
		return report, fmt.Errorf("sweep ELBv2: %w", err)
	}
	if err := s.sweepLoadBalancersClassic(ctx, report); err != nil {
		return report, fmt.Errorf("sweep classic ELB: %w", err)
	}
	if err := s.sweepVPCEndpoints(ctx, report); err != nil {
		return report, fmt.Errorf("sweep VPC endpoints: %w", err)
	}
	if err := s.sweepElasticIPs(ctx, report); err != nil {
		return report, fmt.Errorf("sweep EIPs: %w", err)
	}
	if err := s.sweepENIs(ctx, report); err != nil {
		return report, fmt.Errorf("sweep ENIs: %w", err)
	}
	if err := s.sweepSecurityGroups(ctx, report); err != nil {
		return report, fmt.Errorf("sweep SGs: %w", err)
	}
	return report, nil
}

// WaitForCleanup polls ELBv2 and classic ELB for load balancers still active
// in the target VPC. Returns nil when the VPC is clear; returns a non-fatal
// timeout error if cleanup hasn't completed within the deadline. The caller
// should log the timeout and proceed to Sweep (which handles stragglers).
func (s *AWSSweeper) WaitForCleanup(ctx context.Context, timeout time.Duration) error {
	s.log.LogLine(fmt.Sprintf("[sweep] waiting up to %s for ELB cleanup in VPC %s", timeout, s.opts.VPCID))
	deadline := time.Now().Add(timeout)
	for {
		v2, err := s.listLBsV2InVPC(ctx)
		if err != nil {
			return err
		}
		classic, err := s.listClassicLBsInVPC(ctx)
		if err != nil {
			return err
		}
		total := len(v2) + len(classic)
		if total == 0 {
			s.log.LogLine("[sweep] ELB cleanup complete")
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for %d load balancer(s) to finish deleting", total)
		}
		s.log.LogLine(fmt.Sprintf("[sweep] waiting for %d load balancer(s) to finish deleting in VPC %s", total, s.opts.VPCID))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Second):
		}
	}
}

// ── ELBv2 ────────────────────────────────────────────────────────────────────

func (s *AWSSweeper) sweepLoadBalancersV2(ctx context.Context, report *SweepReport) error {
	inVPC, err := s.listLBsV2InVPC(ctx)
	if err != nil {
		return err
	}
	s.log.LogLine(fmt.Sprintf("[sweep] found %d ELBv2 load balancer(s) in VPC %s", len(inVPC), s.opts.VPCID))
	if len(inVPC) == 0 {
		return nil
	}

	tagsByARN, err := s.fetchLBv2Tags(ctx, inVPC)
	if err != nil {
		return err
	}

	for _, lb := range inVPC {
		arn := aws.ToString(lb.LoadBalancerArn)
		name := aws.ToString(lb.LoadBalancerName)

		if err := s.checkLBv2Safety(lb, tagsByARN[arn]); err != nil {
			s.log.LogLine(fmt.Sprintf("[sweep] ELBv2 %s (%s) → SKIPPING: %v", arn, name, err))
			report.record(false, ResourceEntry{
				Type: "load-balancer-v2", ID: arn, Desc: name,
				Action: "skipped", Reason: err.Error(),
			})
			continue
		}

		matchedTag := lbMatchedTagV2(tagsByARN[arn], s.opts.ClusterName)
		s.log.LogLine(fmt.Sprintf("[sweep] ELBv2 %s (%s) → matched: %s → %s",
			arn, name, matchedTag, s.deleteOrDryRun()))

		if s.opts.DryRun {
			report.record(true, ResourceEntry{
				Type: "load-balancer-v2", ID: arn, Desc: name,
				Action: "dry-run-would-delete", Reason: "matched tag: " + matchedTag,
			})
			continue
		}

		if _, err := s.elbv2.DeleteLoadBalancer(ctx, &elasticloadbalancingv2.DeleteLoadBalancerInput{
			LoadBalancerArn: lb.LoadBalancerArn,
		}); err != nil {
			s.log.LogLine(fmt.Sprintf("[sweep] WARN: delete ELBv2 %s: %v", arn, err))
			report.record(false, ResourceEntry{
				Type: "load-balancer-v2", ID: arn, Desc: name,
				Action: "skipped", Reason: "delete failed: " + err.Error(),
			})
			continue
		}
		s.log.LogLine(fmt.Sprintf("[sweep] deleted ELBv2 %s (%s)", arn, name))
		report.record(true, ResourceEntry{
			Type: "load-balancer-v2", ID: arn, Desc: name,
			Action: "deleted", Reason: "matched tag: " + matchedTag,
		})
	}
	return nil
}

// checkLBv2Safety enforces BOTH preconditions before any deletion attempt.
// Returns nil when the LB is owned by this cluster and safe to delete.
// Returns a SAFETY VIOLATION error for resources owned by another cluster.
// Returns a plain skip error for resources with no Kubernetes tags.
func (s *AWSSweeper) checkLBv2Safety(lb elbv2types.LoadBalancer, tags []elbv2types.Tag) error {
	// PRECONDITION 1: VPC scope
	if aws.ToString(lb.VpcId) != s.opts.VPCID {
		return fmt.Errorf("SAFETY VIOLATION: LB %s is in VPC %s, not target VPC %s",
			aws.ToString(lb.LoadBalancerArn), aws.ToString(lb.VpcId), s.opts.VPCID)
	}
	// PRECONDITION 2: ownership — check all three tag patterns
	if lbBelongsToClusterV2(tags, s.opts.ClusterName) {
		return nil
	}
	// An LB tagged for a DIFFERENT cluster is a hard safety violation.
	if wrong := lbWrongClusterTagV2(tags, s.opts.ClusterName); wrong != "" {
		return fmt.Errorf("SAFETY VIOLATION: LB %s has cluster tag for %q, not %q — will not delete",
			aws.ToString(lb.LoadBalancerArn), wrong, s.opts.ClusterName)
	}
	return fmt.Errorf("no Kubernetes ownership tags on LB %s — leaving for terraform",
		aws.ToString(lb.LoadBalancerArn))
}

func (s *AWSSweeper) listLBsV2InVPC(ctx context.Context) ([]elbv2types.LoadBalancer, error) {
	var result []elbv2types.LoadBalancer
	var marker *string
	for {
		out, err := s.elbv2.DescribeLoadBalancers(ctx, &elasticloadbalancingv2.DescribeLoadBalancersInput{
			Marker: marker,
		})
		if err != nil {
			return nil, err
		}
		for _, lb := range out.LoadBalancers {
			if aws.ToString(lb.VpcId) == s.opts.VPCID {
				result = append(result, lb)
			}
		}
		if out.NextMarker == nil {
			break
		}
		marker = out.NextMarker
	}
	return result, nil
}

func (s *AWSSweeper) fetchLBv2Tags(ctx context.Context, lbs []elbv2types.LoadBalancer) (map[string][]elbv2types.Tag, error) {
	result := make(map[string][]elbv2types.Tag)
	for i := 0; i < len(lbs); i += 20 {
		end := i + 20
		if end > len(lbs) {
			end = len(lbs)
		}
		batch := lbs[i:end]
		arns := make([]string, len(batch))
		for j, lb := range batch {
			arns[j] = aws.ToString(lb.LoadBalancerArn)
		}
		out, err := s.elbv2.DescribeTags(ctx, &elasticloadbalancingv2.DescribeTagsInput{
			ResourceArns: arns,
		})
		if err != nil {
			return nil, fmt.Errorf("DescribeTags ELBv2: %w", err)
		}
		for _, d := range out.TagDescriptions {
			result[aws.ToString(d.ResourceArn)] = d.Tags
		}
	}
	return result, nil
}

// lbBelongsToClusterV2 returns true when any of the three Kubernetes tag
// patterns confirm this LB belongs to the target cluster:
//
//   - Pattern 1 (in-tree CCM): kubernetes.io/cluster/<name> = owned|shared
//   - Pattern 2 (AWS LBC):     elbv2.k8s.aws/cluster = <name>
//   - Pattern 3 (service-name companion): kubernetes.io/service-name + Pattern 1
func lbBelongsToClusterV2(tags []elbv2types.Tag, clusterName string) bool {
	ccmKey := "kubernetes.io/cluster/" + clusterName
	for _, t := range tags {
		k := aws.ToString(t.Key)
		v := aws.ToString(t.Value)
		// Pattern 1
		if k == ccmKey && (v == "owned" || v == "shared") {
			return true
		}
		// Pattern 2
		if k == "elbv2.k8s.aws/cluster" && v == clusterName {
			return true
		}
	}
	// Pattern 3: kubernetes.io/service-name ONLY when Pattern 1 also present
	for _, t := range tags {
		if aws.ToString(t.Key) == "kubernetes.io/service-name" {
			for _, other := range tags {
				if aws.ToString(other.Key) == ccmKey &&
					(aws.ToString(other.Value) == "owned" || aws.ToString(other.Value) == "shared") {
					return true
				}
			}
		}
	}
	return false
}

// lbWrongClusterTagV2 returns the name of the wrong cluster if the LB carries
// a Kubernetes ownership tag for a cluster OTHER than clusterName. Used to
// distinguish "belongs to a different cluster" (SAFETY VIOLATION) from
// "no Kubernetes tags at all" (leave for terraform).
func lbWrongClusterTagV2(tags []elbv2types.Tag, clusterName string) string {
	const prefix = "kubernetes.io/cluster/"
	for _, t := range tags {
		k := aws.ToString(t.Key)
		v := aws.ToString(t.Value)
		if strings.HasPrefix(k, prefix) {
			name := k[len(prefix):]
			if name != clusterName && (v == "owned" || v == "shared") {
				return name
			}
		}
		if k == "elbv2.k8s.aws/cluster" && v != "" && v != clusterName {
			return v
		}
	}
	return ""
}

// lbMatchedTagV2 returns a short description of whichever Kubernetes tag
// matched, for use in log messages. Called only after lbBelongsToClusterV2
// returned true.
func lbMatchedTagV2(tags []elbv2types.Tag, clusterName string) string {
	ccmKey := "kubernetes.io/cluster/" + clusterName
	for _, t := range tags {
		k := aws.ToString(t.Key)
		v := aws.ToString(t.Value)
		if k == ccmKey && (v == "owned" || v == "shared") {
			return k + "=" + v
		}
		if k == "elbv2.k8s.aws/cluster" && v == clusterName {
			return k + "=" + v
		}
	}
	for _, t := range tags {
		if aws.ToString(t.Key) == "kubernetes.io/service-name" {
			return "kubernetes.io/service-name=" + aws.ToString(t.Value) + " (+cluster)"
		}
	}
	return "(unmatched)"
}

func elbv2TagsToMap(tags []elbv2types.Tag) map[string]string {
	m := make(map[string]string, len(tags))
	for _, t := range tags {
		m[aws.ToString(t.Key)] = aws.ToString(t.Value)
	}
	return m
}

// ── Classic ELB ───────────────────────────────────────────────────────────────

func (s *AWSSweeper) sweepLoadBalancersClassic(ctx context.Context, report *SweepReport) error {
	inVPC, err := s.listClassicLBsInVPC(ctx)
	if err != nil {
		return err
	}
	s.log.LogLine(fmt.Sprintf("[sweep] found %d classic ELB(s) in VPC %s", len(inVPC), s.opts.VPCID))
	if len(inVPC) == 0 {
		return nil
	}

	tagsByName, err := s.fetchClassicLBTags(ctx, inVPC)
	if err != nil {
		return err
	}

	for _, lb := range inVPC {
		name := aws.ToString(lb.LoadBalancerName)

		if err := s.checkClassicLBSafety(lb, tagsByName[name]); err != nil {
			s.log.LogLine(fmt.Sprintf("[sweep] classic ELB %s → SKIPPING: %v", name, err))
			report.record(false, ResourceEntry{
				Type: "classic-load-balancer", ID: name, Desc: name,
				Action: "skipped", Reason: err.Error(),
			})
			continue
		}

		tagVal := clusterTagValueClassic(tagsByName[name], s.opts.ClusterName)
		s.log.LogLine(fmt.Sprintf("[sweep] classic ELB %s → cluster tag: %s → %s", name, tagVal, s.deleteOrDryRun()))

		if s.opts.DryRun {
			report.record(true, ResourceEntry{
				Type: "classic-load-balancer", ID: name, Desc: name,
				Action: "dry-run-would-delete", Reason: "cluster tag: " + tagVal,
			})
			continue
		}

		if _, err := s.elbc.DeleteLoadBalancer(ctx, &elasticloadbalancing.DeleteLoadBalancerInput{
			LoadBalancerName: lb.LoadBalancerName,
		}); err != nil {
			s.log.LogLine(fmt.Sprintf("[sweep] WARN: delete classic ELB %s: %v", name, err))
			report.record(false, ResourceEntry{
				Type: "classic-load-balancer", ID: name, Desc: name,
				Action: "skipped", Reason: "delete failed: " + err.Error(),
			})
			continue
		}
		s.log.LogLine(fmt.Sprintf("[sweep] deleted classic ELB %s", name))
		report.record(true, ResourceEntry{
			Type: "classic-load-balancer", ID: name, Desc: name,
			Action: "deleted", Reason: "cluster tag: " + tagVal,
		})
	}
	return nil
}

func (s *AWSSweeper) checkClassicLBSafety(lb elbtypes.LoadBalancerDescription, tags []elbtypes.Tag) error {
	// PRECONDITION 1: VPC scope
	if aws.ToString(lb.VPCId) != s.opts.VPCID {
		return fmt.Errorf("SAFETY VIOLATION: classic ELB %s is in VPC %s, not target VPC %s",
			aws.ToString(lb.LoadBalancerName), aws.ToString(lb.VPCId), s.opts.VPCID)
	}
	// PRECONDITION 2: cluster ownership tag
	if lbBelongsToClusterClassic(tags, s.opts.ClusterName) {
		return nil
	}
	if wrong := lbWrongClusterTagClassic(tags, s.opts.ClusterName); wrong != "" {
		return fmt.Errorf("SAFETY VIOLATION: classic ELB %s has cluster tag for %q, not %q — will not delete",
			aws.ToString(lb.LoadBalancerName), wrong, s.opts.ClusterName)
	}
	return fmt.Errorf("no kubernetes.io/cluster/%s tag (owned|shared); leaving for terraform",
		s.opts.ClusterName)
}

func (s *AWSSweeper) listClassicLBsInVPC(ctx context.Context) ([]elbtypes.LoadBalancerDescription, error) {
	var result []elbtypes.LoadBalancerDescription
	var marker *string
	for {
		out, err := s.elbc.DescribeLoadBalancers(ctx, &elasticloadbalancing.DescribeLoadBalancersInput{
			Marker: marker,
		})
		if err != nil {
			return nil, err
		}
		for _, lb := range out.LoadBalancerDescriptions {
			if aws.ToString(lb.VPCId) == s.opts.VPCID {
				result = append(result, lb)
			}
		}
		if out.NextMarker == nil {
			break
		}
		marker = out.NextMarker
	}
	return result, nil
}

func (s *AWSSweeper) fetchClassicLBTags(ctx context.Context, lbs []elbtypes.LoadBalancerDescription) (map[string][]elbtypes.Tag, error) {
	result := make(map[string][]elbtypes.Tag)
	// Classic ELB DescribeTags takes up to 20 names per call.
	for i := 0; i < len(lbs); i += 20 {
		end := i + 20
		if end > len(lbs) {
			end = len(lbs)
		}
		batch := lbs[i:end]
		names := make([]string, len(batch))
		for j, lb := range batch {
			names[j] = aws.ToString(lb.LoadBalancerName)
		}
		out, err := s.elbc.DescribeTags(ctx, &elasticloadbalancing.DescribeTagsInput{
			LoadBalancerNames: names,
		})
		if err != nil {
			return nil, fmt.Errorf("DescribeTags classic ELB: %w", err)
		}
		for _, d := range out.TagDescriptions {
			result[aws.ToString(d.LoadBalancerName)] = d.Tags
		}
	}
	return result, nil
}

func lbBelongsToClusterClassic(tags []elbtypes.Tag, clusterName string) bool {
	key := "kubernetes.io/cluster/" + clusterName
	for _, t := range tags {
		if aws.ToString(t.Key) == key {
			v := aws.ToString(t.Value)
			return v == "owned" || v == "shared"
		}
	}
	return false
}

func lbWrongClusterTagClassic(tags []elbtypes.Tag, clusterName string) string {
	const prefix = "kubernetes.io/cluster/"
	for _, t := range tags {
		k := aws.ToString(t.Key)
		v := aws.ToString(t.Value)
		if strings.HasPrefix(k, prefix) {
			name := k[len(prefix):]
			if name != clusterName && (v == "owned" || v == "shared") {
				return name
			}
		}
	}
	return ""
}

func clusterTagValueClassic(tags []elbtypes.Tag, clusterName string) string {
	key := "kubernetes.io/cluster/" + clusterName
	for _, t := range tags {
		if aws.ToString(t.Key) == key {
			return aws.ToString(t.Value)
		}
	}
	return ""
}

// ── Elastic IPs ───────────────────────────────────────────────────────────────

// sweepElasticIPs sweeps EIPs tagged with the cluster ownership tag. EIPs do
// not have a VpcId field, so the cluster tag is the sole safety scope filter.
func (s *AWSSweeper) sweepElasticIPs(ctx context.Context, report *SweepReport) error {
	tagKey := "kubernetes.io/cluster/" + s.opts.ClusterName
	out, err := s.ec2c.DescribeAddresses(ctx, &ec2.DescribeAddressesInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("tag:" + tagKey), Values: []string{"owned", "shared"}},
		},
	})
	if err != nil {
		return err
	}
	s.log.LogLine(fmt.Sprintf("[sweep] found %d EIP(s) tagged for cluster %s", len(out.Addresses), s.opts.ClusterName))

	for _, addr := range out.Addresses {
		allocID := aws.ToString(addr.AllocationId)
		publicIP := aws.ToString(addr.PublicIp)
		tagVal := ec2TagValue(addr.Tags, tagKey)

		// Only sweep EIPs that are not currently associated. An associated EIP
		// is still attached to a running resource (instance, NLB ENI, etc.);
		// sweeping it would disrupt live workloads. Orphaned EIPs left by a
		// deleted NLB will have an empty AssociationId.
		if aws.ToString(addr.AssociationId) != "" {
			s.log.LogLine(fmt.Sprintf("[sweep] EIP %s (%s) → SKIPPING: still associated (%s)",
				allocID, publicIP, aws.ToString(addr.AssociationId)))
			report.record(false, ResourceEntry{
				Type: "elastic-ip", ID: allocID, Desc: publicIP,
				Action: "skipped",
				Reason: "still associated: " + aws.ToString(addr.AssociationId),
			})
			continue
		}

		s.log.LogLine(fmt.Sprintf("[sweep] EIP %s (%s) → cluster tag: %s → %s",
			allocID, publicIP, tagVal, s.deleteOrDryRun()))

		if s.opts.DryRun {
			report.record(true, ResourceEntry{
				Type: "elastic-ip", ID: allocID, Desc: publicIP,
				Action: "dry-run-would-delete", Reason: "cluster tag: " + tagVal,
			})
			continue
		}

		if _, err := s.ec2c.ReleaseAddress(ctx, &ec2.ReleaseAddressInput{
			AllocationId: addr.AllocationId,
		}); err != nil {
			s.log.LogLine(fmt.Sprintf("[sweep] WARN: release EIP %s: %v", allocID, err))
			report.record(false, ResourceEntry{
				Type: "elastic-ip", ID: allocID, Desc: publicIP,
				Action: "skipped", Reason: "release failed: " + err.Error(),
			})
			continue
		}
		s.log.LogLine(fmt.Sprintf("[sweep] released EIP %s (%s)", allocID, publicIP))
		report.record(true, ResourceEntry{
			Type: "elastic-ip", ID: allocID, Desc: publicIP,
			Action: "deleted", Reason: "cluster tag: " + tagVal,
		})
	}
	return nil
}

// ── ENIs ──────────────────────────────────────────────────────────────────────

// sweepENIs sweeps detached ENIs that match Kubernetes description patterns.
// ENIs do not always carry the cluster tag (CCM may not tag them), so the
// 4-condition rule is applied instead of the standard VPC+tag rule:
//
//  a. In target VPC
//  b. Status = available (detached)
//  c. Description matches a Kubernetes ELB pattern
//  d. No active attachments
func (s *AWSSweeper) sweepENIs(ctx context.Context, report *SweepReport) error {
	out, err := s.ec2c.DescribeNetworkInterfaces(ctx, &ec2.DescribeNetworkInterfacesInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("vpc-id"), Values: []string{s.opts.VPCID}},
			{Name: aws.String("status"), Values: []string{"available"}},
		},
	})
	if err != nil {
		return err
	}
	s.log.LogLine(fmt.Sprintf("[sweep] found %d available ENI(s) in VPC %s", len(out.NetworkInterfaces), s.opts.VPCID))

	for _, eni := range out.NetworkInterfaces {
		id := aws.ToString(eni.NetworkInterfaceId)
		desc := aws.ToString(eni.Description)

		sweep, reason := eniIsSweepable(eni, s.opts.VPCID)
		if !sweep {
			s.log.LogLine(fmt.Sprintf("[sweep] ENI %s (desc: %q) → SKIPPING: %s", id, desc, reason))
			report.record(false, ResourceEntry{
				Type: "eni", ID: id, Desc: desc,
				Action: "skipped", Reason: reason,
			})
			continue
		}

		s.log.LogLine(fmt.Sprintf("[sweep] ENI %s (desc: %q) → %s", id, desc, s.deleteOrDryRun()))

		if s.opts.DryRun {
			report.record(true, ResourceEntry{
				Type: "eni", ID: id, Desc: desc,
				Action: "dry-run-would-delete", Reason: reason,
			})
			continue
		}

		if _, err := s.ec2c.DeleteNetworkInterface(ctx, &ec2.DeleteNetworkInterfaceInput{
			NetworkInterfaceId: eni.NetworkInterfaceId,
		}); err != nil {
			s.log.LogLine(fmt.Sprintf("[sweep] WARN: delete ENI %s: %v", id, err))
			report.record(false, ResourceEntry{
				Type: "eni", ID: id, Desc: desc,
				Action: "skipped", Reason: "delete failed: " + err.Error(),
			})
			continue
		}
		s.log.LogLine(fmt.Sprintf("[sweep] deleted ENI %s (desc: %q)", id, desc))
		report.record(true, ResourceEntry{
			Type: "eni", ID: id, Desc: desc,
			Action: "deleted", Reason: reason,
		})
	}
	return nil
}

// eniIsSweepable enforces the 4-condition ENI rule (§3 of the safety amendment).
// Returns (true, reason) when all four conditions hold; (false, reason) otherwise.
func eniIsSweepable(eni ec2types.NetworkInterface, vpcID string) (bool, string) {
	// Condition (a): target VPC
	if aws.ToString(eni.VpcId) != vpcID {
		return false, fmt.Sprintf("SAFETY VIOLATION: ENI %s is in VPC %s, not target VPC %s",
			aws.ToString(eni.NetworkInterfaceId), aws.ToString(eni.VpcId), vpcID)
	}
	// Condition (b): available (already filtered server-side, but double-check)
	if eni.Status != ec2types.NetworkInterfaceStatusAvailable {
		return false, "status is not available (has active attachment)"
	}
	// Condition (c): description matches known Kubernetes ELB patterns
	desc := aws.ToString(eni.Description)
	if !eniMatchesKubernetes(desc) {
		return false, fmt.Sprintf("unknown ENI in VPC — leaving for terraform (description: %q)", desc)
	}
	// Condition (d): no active attachment
	if eni.Attachment != nil && eni.Attachment.Status != ec2types.AttachmentStatusDetached {
		return false, fmt.Sprintf("has active attachment (status: %s)", eni.Attachment.Status)
	}
	return true, fmt.Sprintf("Kubernetes ELB ENI (description: %q)", desc)
}

func eniMatchesKubernetes(desc string) bool {
	return strings.HasPrefix(desc, "ELB ") ||
		strings.HasPrefix(desc, "amazon-elb/") ||
		strings.Contains(desc, "kubernetes.io/")
}

// ── Security groups ───────────────────────────────────────────────────────────

// sweepSecurityGroups removes Kubernetes-created security groups from the VPC.
//
// The AWS console's "Delete VPC" flow does two things terraform's aws_vpc
// destroy does NOT: (1) revokes all rules on every SG (clearing cross-SG
// references that cause DependencyViolation), and (2) deletes non-default SGs.
// This function replicates that flow so DeleteVpc succeeds on the first attempt.
//
// Safety model:
//   - Default SG (GroupName == "default"): rules revoked, never deleted.
//   - Non-default SG with kubernetes.io/cluster/<name>=owned|shared tag: deleted.
//   - Non-default SG whose name matches a known k8s pattern (k8s-elb-*, etc.): deleted.
//   - Anything else: logged and skipped — never touched.
//
// Two-pass ordering is required: pass 1 revokes ALL rules across ALL eligible SGs
// before any deletion so that circular cross-SG rule references are untangled.
func (s *AWSSweeper) sweepSecurityGroups(ctx context.Context, report *SweepReport) error {
	// Query all SGs in the VPC — no tag filter; we need untagged k8s-elb-* SGs
	// and the default SG.
	out, err := s.ec2c.DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("vpc-id"), Values: []string{s.opts.VPCID}},
		},
	})
	if err != nil {
		return err
	}
	s.log.LogLine(fmt.Sprintf("[sweep] found %d security group(s) in VPC %s",
		len(out.SecurityGroups), s.opts.VPCID))

	tagKey := "kubernetes.io/cluster/" + s.opts.ClusterName

	var defaultSG *ec2types.SecurityGroup
	var toProcess []ec2types.SecurityGroup // non-default SGs to revoke+delete

	for i := range out.SecurityGroups {
		sg := &out.SecurityGroups[i]
		sgID := aws.ToString(sg.GroupId)
		sgName := aws.ToString(sg.GroupName)

		if aws.ToString(sg.VpcId) != s.opts.VPCID {
			// Server-side filter applied; this is a defensive double-check.
			s.log.LogLine(fmt.Sprintf("[sweep] SG %s (%s) → SAFETY VIOLATION: wrong VPC — skipping", sgID, sgName))
			continue
		}

		if sgName == "default" {
			defaultSG = sg
			continue
		}

		// Allow deletion if the SG has the cluster ownership tag.
		hasTag := sgHasClusterTag(sg.Tags, tagKey)
		// Also allow deletion if the name matches a known k8s-created pattern.
		// In-tree CCM ELB SGs (k8s-elb-*) and NLB SGs (k8s-traffic-*) are
		// created without the cluster tag.
		hasPattern := sgNameMatchesKubernetes(sgName)

		if !hasTag && !hasPattern {
			s.log.LogLine(fmt.Sprintf(
				"[sweep] SG %s (%s) → SKIPPING: no cluster tag and no k8s name pattern — leaving for manual investigation",
				sgID, sgName))
			report.record(false, ResourceEntry{
				Type: "security-group", ID: sgID, Desc: sgName,
				Action: "skipped", Reason: "no cluster tag and no k8s name pattern",
			})
			continue
		}

		toProcess = append(toProcess, *sg)
	}

	// Pass 1: revoke all ingress/egress rules on eligible SGs and on the default
	// SG. Must happen before any deletion to untangle circular rule references.
	if defaultSG != nil {
		s.revokeAllSGRules(ctx, *defaultSG)
	}
	for _, sg := range toProcess {
		s.revokeAllSGRules(ctx, sg)
	}

	// Pass 2: delete non-default eligible SGs.
	for _, sg := range toProcess {
		sgID := aws.ToString(sg.GroupId)
		sgName := aws.ToString(sg.GroupName)

		reason := "k8s name pattern"
		if sgHasClusterTag(sg.Tags, tagKey) {
			reason = "cluster tag: " + ec2TagValue(sg.Tags, tagKey)
		}

		s.log.LogLine(fmt.Sprintf("[sweep] SG %s (%s) → %s [%s]", sgID, sgName, s.deleteOrDryRun(), reason))

		if s.opts.DryRun {
			report.record(true, ResourceEntry{
				Type: "security-group", ID: sgID, Desc: sgName,
				Action: "dry-run-would-delete", Reason: reason,
			})
			continue
		}

		var deleteErr error
		for attempt := 1; attempt <= 12; attempt++ {
			_, deleteErr = s.ec2c.DeleteSecurityGroup(ctx, &ec2.DeleteSecurityGroupInput{
				GroupId: sg.GroupId,
			})
			if deleteErr == nil {
				break
			}
			if strings.Contains(deleteErr.Error(), "DependencyViolation") {
				s.log.LogLine(fmt.Sprintf("[sweep] SG %s still in use by ENIs (EKS background cleanup in progress), waiting 15s (attempt %d/12)...", sgID, attempt))
				select {
				case <-ctx.Done():
					deleteErr = ctx.Err()
					break
				case <-time.After(15 * time.Second):
				}
				continue
			}
			break
		}
		if deleteErr != nil {
			s.log.LogLine(fmt.Sprintf("[sweep] WARN: delete SG %s: %v", sgID, deleteErr))
			report.record(false, ResourceEntry{
				Type: "security-group", ID: sgID, Desc: sgName,
				Action: "skipped", Reason: "delete failed: " + deleteErr.Error(),
			})
			continue
		}
		s.log.LogLine(fmt.Sprintf("[sweep] deleted SG %s (%s)", sgID, sgName))
		report.record(true, ResourceEntry{
			Type: "security-group", ID: sgID, Desc: sgName,
			Action: "deleted", Reason: reason,
		})
	}
	return nil
}

// revokeAllSGRules revokes all ingress and egress rules on sg. Non-fatal:
// errors are logged and the function continues to the egress phase.
func (s *AWSSweeper) revokeAllSGRules(ctx context.Context, sg ec2types.SecurityGroup) {
	sgID := aws.ToString(sg.GroupId)
	sgName := aws.ToString(sg.GroupName)

	if len(sg.IpPermissions) > 0 {
		s.log.LogLine(fmt.Sprintf("[sweep] revoking %d ingress rule(s) on SG %s (%s)",
			len(sg.IpPermissions), sgID, sgName))
		if !s.opts.DryRun {
			if _, err := s.ec2c.RevokeSecurityGroupIngress(ctx, &ec2.RevokeSecurityGroupIngressInput{
				GroupId:       sg.GroupId,
				IpPermissions: sg.IpPermissions,
			}); err != nil {
				s.log.LogLine(fmt.Sprintf("[sweep] WARN: revoke ingress on SG %s: %v", sgID, err))
			}
		}
	}
	if len(sg.IpPermissionsEgress) > 0 {
		s.log.LogLine(fmt.Sprintf("[sweep] revoking %d egress rule(s) on SG %s (%s)",
			len(sg.IpPermissionsEgress), sgID, sgName))
		if !s.opts.DryRun {
			if _, err := s.ec2c.RevokeSecurityGroupEgress(ctx, &ec2.RevokeSecurityGroupEgressInput{
				GroupId:       sg.GroupId,
				IpPermissions: sg.IpPermissionsEgress,
			}); err != nil {
				s.log.LogLine(fmt.Sprintf("[sweep] WARN: revoke egress on SG %s: %v", sgID, err))
			}
		}
	}
}

// sgHasClusterTag reports whether sg carries kubernetes.io/cluster/<name>=owned|shared.
func sgHasClusterTag(tags []ec2types.Tag, tagKey string) bool {
	for _, t := range tags {
		if aws.ToString(t.Key) == tagKey {
			v := aws.ToString(t.Value)
			if v == "owned" || v == "shared" {
				return true
			}
		}
	}
	return false
}

// sgNameMatchesKubernetes reports whether name is a CCM-created security group
// that may lack a cluster ownership tag. Only patterns for resources that CCM
// creates and that terraform does NOT manage are included.
//
// Deliberately excluded:
//   - eks-cluster-sg-* — created by the EKS service, managed by terraform's
//     aws_eks_cluster resource; deleted by terraform before the VPC.
//   - amazon-eks-*     — same: EKS service / terraform-managed.
//
// Those patterns must never be in a sweep because they are live while EKS runs
// and are already gone (via terraform) by the time any post-destroy sweep runs.
func sgNameMatchesKubernetes(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasPrefix(lower, "k8s-elb-") ||
		strings.HasPrefix(lower, "k8s-traffic-")
}

// ── VPC endpoints ─────────────────────────────────────────────────────────────

// sweepVPCEndpoints deletes Interface VPC endpoints tagged for this cluster.
// Gateway endpoints (S3, DynamoDB) are AWS-managed and cannot be deleted via
// this API — they are skipped automatically by AWS.
func (s *AWSSweeper) sweepVPCEndpoints(ctx context.Context, report *SweepReport) error {
	out, err := s.ec2c.DescribeVpcEndpoints(ctx, &ec2.DescribeVpcEndpointsInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("vpc-id"), Values: []string{s.opts.VPCID}},
			{Name: aws.String("vpc-endpoint-state"), Values: []string{"available", "pending"}},
		},
	})
	if err != nil {
		return err
	}
	s.log.LogLine(fmt.Sprintf("[sweep] found %d VPC endpoint(s) in VPC %s", len(out.VpcEndpoints), s.opts.VPCID))
	if len(out.VpcEndpoints) == 0 {
		return nil
	}

	tagKey := "kubernetes.io/cluster/" + s.opts.ClusterName
	var toDelete []string
	for _, ep := range out.VpcEndpoints {
		epID := aws.ToString(ep.VpcEndpointId)
		svcName := aws.ToString(ep.ServiceName)
		owned := false
		for _, t := range ep.Tags {
			if aws.ToString(t.Key) == tagKey &&
				(aws.ToString(t.Value) == "owned" || aws.ToString(t.Value) == "shared") {
				owned = true
				break
			}
		}
		if !owned {
			s.log.LogLine(fmt.Sprintf("[sweep] VPC endpoint %s (%s) → SKIPPING: no cluster tag", epID, svcName))
			report.record(false, ResourceEntry{
				Type: "vpc-endpoint", ID: epID, Desc: svcName,
				Action: "skipped", Reason: "no kubernetes.io/cluster/" + s.opts.ClusterName + " tag",
			})
			continue
		}
		s.log.LogLine(fmt.Sprintf("[sweep] VPC endpoint %s (%s) → %s", epID, svcName, s.deleteOrDryRun()))
		if s.opts.DryRun {
			report.record(true, ResourceEntry{
				Type: "vpc-endpoint", ID: epID, Desc: svcName,
				Action: "dry-run-would-delete", Reason: "cluster tag owned",
			})
			continue
		}
		toDelete = append(toDelete, epID)
	}
	if len(toDelete) == 0 {
		return nil
	}

	delOut, err := s.ec2c.DeleteVpcEndpoints(ctx, &ec2.DeleteVpcEndpointsInput{
		VpcEndpointIds: toDelete,
	})
	if err != nil {
		return fmt.Errorf("DeleteVpcEndpoints: %w", err)
	}
	// Mark each deletion as successful unless it appeared in the Unsuccessful list.
	failed := make(map[string]string)
	for _, item := range delOut.Unsuccessful {
		if item.Error != nil {
			failed[aws.ToString(item.ResourceId)] = aws.ToString(item.Error.Message)
		}
	}
	// We need to look up service names for the deletion report. Reconstruct from
	// the original out.VpcEndpoints slice.
	svcByID := make(map[string]string)
	for _, ep := range out.VpcEndpoints {
		svcByID[aws.ToString(ep.VpcEndpointId)] = aws.ToString(ep.ServiceName)
	}
	for _, id := range toDelete {
		if msg, bad := failed[id]; bad {
			s.log.LogLine(fmt.Sprintf("[sweep] WARN: delete VPC endpoint %s failed: %s", id, msg))
			report.record(false, ResourceEntry{
				Type: "vpc-endpoint", ID: id, Desc: svcByID[id],
				Action: "skipped", Reason: "delete failed: " + msg,
			})
		} else {
			s.log.LogLine(fmt.Sprintf("[sweep] deleted VPC endpoint %s (%s)", id, svcByID[id]))
			report.record(true, ResourceEntry{
				Type: "vpc-endpoint", ID: id, Desc: svcByID[id],
				Action: "deleted", Reason: "cluster tag owned",
			})
		}
	}
	return nil
}

// ── VPC blocker wait ──────────────────────────────────────────────────────────

// VPCBlocker is a resource still present in the VPC that would prevent
// terraform from deleting the VPC, its subnets, or the IGW.
type VPCBlocker struct {
	Kind              string // "load-balancer-v2", "classic-load-balancer", "eni", "vpc-endpoint", "elastic-ip"
	ID                string
	Description       string
	Status            string // AWS-reported resource status (e.g. "available", "deleting" for ENIs)
	OwnedByKubernetes bool
}

func (b VPCBlocker) String() string {
	owner := "unknown-owner"
	if b.OwnedByKubernetes {
		owner = "k8s-owned"
	}
	if b.Status != "" {
		return fmt.Sprintf("%s/%s(%s)[%s/%s]", b.Kind, b.ID, b.Description, b.Status, owner)
	}
	return fmt.Sprintf("%s/%s(%s)[%s]", b.Kind, b.ID, b.Description, owner)
}

// WaitForVPCClear polls until the VPC has no Kubernetes-created resources that
// would block terraform destroy. It actively attempts to delete K8s-owned
// blockers on each iteration.
//
// Fast path: if the VPC is already clear on the first check, returns immediately
// without starting the polling loop.
//
// Non-fatal: if the timeout expires, it logs the remaining blockers and returns
// an error. Callers should log the error and proceed to terraform destroy anyway
// (it may still succeed if the blockers finish draining asynchronously).
func (s *AWSSweeper) WaitForVPCClear(ctx context.Context, timeout time.Duration) error {
	// Adjustment 3: fast-path — check once before entering the polling loop.
	blockers, err := s.findVPCBlockers(ctx)
	if err != nil {
		return fmt.Errorf("WaitForVPCClear: initial check: %w", err)
	}
	if len(blockers) == 0 {
		s.log.LogLine(fmt.Sprintf("[sweep] VPC %s is already clear", s.opts.VPCID))
		return nil
	}

	s.log.LogLine(fmt.Sprintf("[sweep] VPC %s has %d dependencies; waiting up to %s for cleanup",
		s.opts.VPCID, len(blockers), timeout))

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		// Actively clean up K8s-owned blockers on each iteration.
		for _, b := range blockers {
			if b.OwnedByKubernetes {
				if err := s.cleanupBlocker(ctx, b); err != nil {
					s.log.LogLine(fmt.Sprintf("[sweep] WARN: cleanup %s %s: %v", b.Kind, b.ID, err))
				}
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(s.vpcPollInterval):
		}

		blockers, err = s.findVPCBlockers(ctx)
		if err != nil {
			return fmt.Errorf("WaitForVPCClear: re-check: %w", err)
		}
		if len(blockers) == 0 {
			s.log.LogLine(fmt.Sprintf("[sweep] VPC %s is now clear", s.opts.VPCID))
			return nil
		}
		s.log.LogLine(fmt.Sprintf("[sweep] %d VPC %s dependencies remain: %s",
			len(blockers), s.opts.VPCID, describeBlockers(blockers)))
	}

	return fmt.Errorf("VPC %s still has %d dependencies after %s:\n  %s",
		s.opts.VPCID, len(blockers), timeout, describeBlockers(blockers))
}

// findVPCBlockers returns all resources currently present in the VPC that
// could cause terraform destroy to fail with DependencyViolation.
func (s *AWSSweeper) findVPCBlockers(ctx context.Context) ([]VPCBlocker, error) {
	var blockers []VPCBlocker

	// ELBv2
	lbsV2, err := s.listLBsV2InVPC(ctx)
	if err != nil {
		return nil, fmt.Errorf("findVPCBlockers ELBv2: %w", err)
	}
	if len(lbsV2) > 0 {
		tagsByARN, err := s.fetchLBv2Tags(ctx, lbsV2)
		if err != nil {
			return nil, fmt.Errorf("findVPCBlockers ELBv2 tags: %w", err)
		}
		for _, lb := range lbsV2 {
			arn := aws.ToString(lb.LoadBalancerArn)
			blockers = append(blockers, VPCBlocker{
				Kind:              "load-balancer-v2",
				ID:                arn,
				Description:       aws.ToString(lb.LoadBalancerName),
				OwnedByKubernetes: lbBelongsToClusterV2(tagsByARN[arn], s.opts.ClusterName),
			})
		}
	}

	// Classic ELB
	classicLBs, err := s.listClassicLBsInVPC(ctx)
	if err != nil {
		return nil, fmt.Errorf("findVPCBlockers classic ELB: %w", err)
	}
	if len(classicLBs) > 0 {
		tagsByName, err := s.fetchClassicLBTags(ctx, classicLBs)
		if err != nil {
			return nil, fmt.Errorf("findVPCBlockers classic ELB tags: %w", err)
		}
		for _, lb := range classicLBs {
			name := aws.ToString(lb.LoadBalancerName)
			blockers = append(blockers, VPCBlocker{
				Kind:              "classic-load-balancer",
				ID:                name,
				Description:       name,
				OwnedByKubernetes: lbBelongsToClusterClassic(tagsByName[name], s.opts.ClusterName),
			})
		}
	}

	// ENIs in "available" or "deleting" state block VPC deletion.
	// "available" ENIs can be explicitly deleted; "deleting" ENIs are already
	// being removed by AWS after NLB/NAT-GW deletion and may persist for several
	// minutes — WaitForVPCClear must wait for them to fully disappear.
	// "in-use" ENIs attached to running EC2 instances are excluded: Terraform
	// manages their lifecycle during destroy and they leave on their own.
	eniOut, err := s.ec2c.DescribeNetworkInterfaces(ctx, &ec2.DescribeNetworkInterfacesInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("vpc-id"), Values: []string{s.opts.VPCID}},
			{Name: aws.String("status"), Values: []string{"available", "deleting"}},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("findVPCBlockers ENIs: %w", err)
	}
	for _, eni := range eniOut.NetworkInterfaces {
		status := string(eni.Status)
		desc := aws.ToString(eni.Description)
		// Only mark available K8s ENIs for active cleanup; "deleting" ENIs are
		// already being removed — OwnedByKubernetes=false means we just wait.
		k8sOwned := status == "available" && eniMatchesKubernetes(desc)
		blockers = append(blockers, VPCBlocker{
			Kind:              "eni",
			ID:                aws.ToString(eni.NetworkInterfaceId),
			Description:       desc,
			Status:            status,
			OwnedByKubernetes: k8sOwned,
		})
	}

	// VPC endpoints — any state except terminal ones blocks VPC deletion
	epOut, err := s.ec2c.DescribeVpcEndpoints(ctx, &ec2.DescribeVpcEndpointsInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("vpc-id"), Values: []string{s.opts.VPCID}},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("findVPCBlockers VPC endpoints: %w", err)
	}
	tagKey := "kubernetes.io/cluster/" + s.opts.ClusterName
	for _, ep := range epOut.VpcEndpoints {
		state := string(ep.State)
		if state == "deleted" || state == "rejected" || state == "failed" {
			continue
		}
		owned := false
		for _, t := range ep.Tags {
			if aws.ToString(t.Key) == tagKey &&
				(aws.ToString(t.Value) == "owned" || aws.ToString(t.Value) == "shared") {
				owned = true
				break
			}
		}
		blockers = append(blockers, VPCBlocker{
			Kind:              "vpc-endpoint",
			ID:                aws.ToString(ep.VpcEndpointId),
			Description:       aws.ToString(ep.ServiceName),
			OwnedByKubernetes: owned,
		})
	}

	// Non-default security groups block DeleteVpc. The default SG is managed
	// internally by the DeleteVpc API itself and is not included here.
	sgOut, err := s.ec2c.DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("vpc-id"), Values: []string{s.opts.VPCID}},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("findVPCBlockers security groups: %w", err)
	}
	for _, sg := range sgOut.SecurityGroups {
		if aws.ToString(sg.GroupName) == "default" {
			continue
		}
		sgName := aws.ToString(sg.GroupName)
		k8sOwned := sgHasClusterTag(sg.Tags, tagKey) || sgNameMatchesKubernetes(sgName)
		blockers = append(blockers, VPCBlocker{
			Kind:              "security-group",
			ID:                aws.ToString(sg.GroupId),
			Description:       sgName,
			OwnedByKubernetes: k8sOwned,
		})
	}

	return blockers, nil
}

func (s *AWSSweeper) cleanupBlocker(ctx context.Context, b VPCBlocker) error {
	switch b.Kind {
	case "load-balancer-v2":
		_, err := s.elbv2.DeleteLoadBalancer(ctx, &elasticloadbalancingv2.DeleteLoadBalancerInput{
			LoadBalancerArn: aws.String(b.ID),
		})
		return err
	case "classic-load-balancer":
		_, err := s.elbc.DeleteLoadBalancer(ctx, &elasticloadbalancing.DeleteLoadBalancerInput{
			LoadBalancerName: aws.String(b.ID),
		})
		return err
	case "eni":
		_, err := s.ec2c.DeleteNetworkInterface(ctx, &ec2.DeleteNetworkInterfaceInput{
			NetworkInterfaceId: aws.String(b.ID),
		})
		return err
	case "vpc-endpoint":
		_, err := s.ec2c.DeleteVpcEndpoints(ctx, &ec2.DeleteVpcEndpointsInput{
			VpcEndpointIds: []string{b.ID},
		})
		return err
	case "security-group":
		// Re-fetch to get current rules (may differ from findVPCBlockers snapshot).
		sgOut, err := s.ec2c.DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{
			Filters: []ec2types.Filter{
				{Name: aws.String("group-id"), Values: []string{b.ID}},
			},
		})
		if err != nil {
			return fmt.Errorf("cleanupBlocker: describe SG %s: %w", b.ID, err)
		}
		var target *ec2types.SecurityGroup
		for i := range sgOut.SecurityGroups {
			if aws.ToString(sgOut.SecurityGroups[i].GroupId) == b.ID {
				target = &sgOut.SecurityGroups[i]
				break
			}
		}
		if target == nil {
			return nil // Already gone.
		}
		s.revokeAllSGRules(ctx, *target)
		_, err = s.ec2c.DeleteSecurityGroup(ctx, &ec2.DeleteSecurityGroupInput{
			GroupId: aws.String(b.ID),
		})
		return err
	}
	return nil
}

func describeBlockers(blockers []VPCBlocker) string {
	parts := make([]string, len(blockers))
	for i, b := range blockers {
		parts[i] = b.String()
	}
	return strings.Join(parts, "; ")
}

// ── VPC discovery ─────────────────────────────────────────────────────────────

// DiscoverVPCID returns the ID of the VPC tagged with
// kubernetes.io/cluster/<clusterName> = owned|shared. This is set by the AWS
// Cloud Controller Manager when EKS creates cluster resources in the VPC.
// Returns an error if no matching VPC is found or if the API call fails.
func DiscoverVPCID(ctx context.Context, region, clusterName string) (string, error) {
	if clusterName == "" {
		return "", fmt.Errorf(
			"SAFETY VIOLATION: DiscoverVPCID called without cluster name; refusing to proceed")
	}
	cfg, err := awscfg.LoadDefaultConfig(ctx, awscfg.WithRegion(region))
	if err != nil {
		return "", fmt.Errorf("cloudsweep: load AWS config: %w", err)
	}
	c := ec2.NewFromConfig(cfg)
	out, err := c.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{
		Filters: []ec2types.Filter{
			{
				Name:   aws.String("tag:kubernetes.io/cluster/" + clusterName),
				Values: []string{"owned", "shared"},
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("DescribeVpcs for cluster %s: %w", clusterName, err)
	}
	if len(out.Vpcs) == 0 {
		return "", fmt.Errorf("no VPC tagged kubernetes.io/cluster/%s found; cluster may already be torn down", clusterName)
	}
	return aws.ToString(out.Vpcs[0].VpcId), nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func (s *AWSSweeper) deleteOrDryRun() string {
	if s.opts.DryRun {
		return "dry-run-would-delete"
	}
	return "will delete"
}

func clusterTagValue(tags map[string]string, clusterName string) string {
	return tags["kubernetes.io/cluster/"+clusterName]
}

func ec2TagValue(tags []ec2types.Tag, key string) string {
	for _, t := range tags {
		if aws.ToString(t.Key) == key {
			return aws.ToString(t.Value)
		}
	}
	return ""
}
