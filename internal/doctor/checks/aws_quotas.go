package checks

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/WolframResearch/WAS-Kubernetes/internal/doctor"
)

// getServiceQuotaFn shells out to aws service-quotas get-service-quota.
// Results are cached in RunContext.quotaCache across checks in the same run.
// Overridable for tests.
var getServiceQuotaFn = func(ctx context.Context, region, serviceCode, quotaCode string) (float64, error) {
	out, err := runOutput(ctx, "aws", "service-quotas", "get-service-quota",
		"--region", region,
		"--service-code", serviceCode,
		"--quota-code", quotaCode,
		"--output", "json")
	if err != nil {
		return 0, err
	}
	var resp struct {
		Quota struct {
			Value float64 `json:"Value"`
		} `json:"Quota"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return 0, fmt.Errorf("parse quota: %w", err)
	}
	return resp.Quota.Value, nil
}

func fetchQuota(ctx context.Context, rc *doctor.RunContext, svc, code string) (float64, bool, error) {
	cacheKey := svc + "/" + code
	if v, ok := rc.QuotaCached(cacheKey); ok {
		f, err := strconv.ParseFloat(v, 64)
		return f, true, err
	}
	val, err := getServiceQuotaFn(ctx, rc.Cfg.Region.Value, svc, code)
	if err != nil {
		if strings.Contains(err.Error(), "AccessDenied") || strings.Contains(err.Error(), "is not authorized") {
			return 0, false, nil // treat AccessDenied as "skip"
		}
		return 0, false, err
	}
	rc.QuotaStore(cacheKey, strconv.FormatFloat(val, 'f', -1, 64))
	return val, true, nil
}

func makeQuotaCheck(id, name, svc, quotaCode string, threshold float64, sev doctor.Severity, detail string) quotaCheck {
	return quotaCheck{
		id: id, name: name, svc: svc, code: quotaCode,
		threshold: threshold, sev: sev, detail: detail,
	}
}

type quotaCheck struct {
	BaseCheck
	id, name, svc, code string
	threshold            float64
	sev                  doctor.Severity
	detail               string
}

func (q quotaCheck) ID() string               { return q.id }
func (q quotaCheck) Name() string             { return q.name }
func (q quotaCheck) Category() doctor.Category { return doctor.CategoryEnvironment }
func (q quotaCheck) Description() string      { return q.detail }
func (q quotaCheck) Precondition(_ context.Context, rc *doctor.RunContext) (bool, string) {
	if skip, reason := awsOnlyPrecondition(rc); skip {
		return true, reason
	}
	if rc.CallerID == nil {
		return true, "AWS credentials unavailable; check aws.credentials first"
	}
	return false, ""
}
func (q quotaCheck) Run(ctx context.Context, rc *doctor.RunContext) doctor.Finding {
	val, found, err := fetchQuota(ctx, rc, q.svc, q.code)
	if err != nil {
		return checkError(q.id, q.name, fmt.Sprintf("quota lookup failed: %v", err))
	}
	if !found {
		return skip(q.id, q.name,
			"IAM identity lacks servicequotas:GetServiceQuota; cannot verify quota. "+
				"Grant the permission or check quotas manually in the AWS console.")
	}
	if val < q.threshold {
		return fail(q.id, q.name, q.sev,
			fmt.Sprintf("quota is %.0f; recommend ≥ %.0f for a WAS deployment", val, q.threshold),
			fmt.Sprintf("Increase the quota in the AWS Service Quotas console for region %s.", rc.Cfg.Region.Value))
	}
	return pass(q.id, q.name, fmt.Sprintf("quota is %.0f (threshold %.0f)", val, q.threshold))
}

// The four quota checks registered in the registry.

// AWSQuotaVPC checks the VPC limit.
var AWSQuotaVPC = makeQuotaCheck(
	"aws.quota.vpc", "VPC quota available in region",
	"vpc", "L-F678F1CE", 5, doctor.SeverityWarning,
	"Checks that the account has at least 5 VPCs available in the region.")

// AWSQuotaEIP checks Elastic IP allocation.
var AWSQuotaEIP = makeQuotaCheck(
	"aws.quota.eip", "Elastic IP quota for NAT gateways",
	"ec2", "L-0263D0A3", 5, doctor.SeverityWarning,
	"Checks that the account has at least 5 Elastic IPs available for NAT gateways.")

// AWSQuotaEKSClusters checks EKS cluster quota.
var AWSQuotaEKSClusters = makeQuotaCheck(
	"aws.quota.eks_clusters", "EKS cluster quota",
	"eks", "L-1194D53C", 2, doctor.SeverityProblem,
	"Checks that the account can create at least 2 EKS clusters (1 minimum for WAS).")

// AWSQuotaEC2Instances checks EC2 on-demand quota.
var AWSQuotaEC2Instances = makeQuotaCheck(
	"aws.quota.ec2_instances", "EC2 on-demand quota for desired node type",
	"ec2", "L-1216C47A", 8, doctor.SeverityProblem,
	"Checks that the account has quota for at least 8 on-demand vCPUs (2 × c5.2xlarge = 16 vCPUs recommended).")
