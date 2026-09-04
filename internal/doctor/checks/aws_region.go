package checks

import (
	"context"
	"fmt"
	"strings"

	"github.com/WolframResearch/WAS-Kubernetes/internal/doctor"
)

// eksRegions is the set of AWS regions known to support EKS. This list covers
// all generally-available regions as of mid-2025; it is a fast offline check.
var eksRegions = map[string]bool{
	"us-east-1": true, "us-east-2": true, "us-west-1": true, "us-west-2": true,
	"eu-west-1": true, "eu-west-2": true, "eu-west-3": true,
	"eu-central-1": true, "eu-central-2": true, "eu-north-1": true,
	"eu-south-1": true, "eu-south-2": true,
	"ap-northeast-1": true, "ap-northeast-2": true, "ap-northeast-3": true,
	"ap-southeast-1": true, "ap-southeast-2": true, "ap-southeast-3": true,
	"ap-south-1": true, "ap-south-2": true,
	"ap-east-1": true,
	"ca-central-1": true, "ca-west-1": true,
	"sa-east-1": true,
	"me-south-1": true, "me-central-1": true,
	"af-south-1": true,
	"il-central-1": true,
}

// awsDescribeRegionFn shells out to validate the region against the AWS API.
// Overridable for tests.
var awsDescribeRegionFn = func(ctx context.Context, region string) (string, error) {
	out, err := runOutput(ctx, "aws", "ec2", "describe-availability-zones",
		"--region", region, "--query", "AvailabilityZones[0].RegionName",
		"--output", "text")
	return strings.TrimSpace(string(out)), err
}

// AWSRegion verifies the configured region exists and supports EKS.
type AWSRegion struct{ BaseCheck }

func (AWSRegion) ID() string          { return "aws.region" }
func (AWSRegion) Name() string        { return "AWS region exists and supports EKS" }
func (AWSRegion) Category() doctor.Category { return doctor.CategoryEnvironment }
func (AWSRegion) Description() string {
	return "Verifies the configured AWS region is valid and supports EKS."
}
func (AWSRegion) Precondition(_ context.Context, rc *doctor.RunContext) (bool, string) {
	return awsOnlyPrecondition(rc)
}

func (c AWSRegion) Run(ctx context.Context, rc *doctor.RunContext) doctor.Finding {
	region := rc.Cfg.Region.Value
	if region == "" {
		return fail(c.ID(), c.Name(), doctor.SeverityProblem,
			"no AWS region configured",
			"Set --region or WAS_REGION. Example: --region us-east-1")
	}

	// Fast offline check against known EKS regions
	if !eksRegions[region] {
		return fail(c.ID(), c.Name(), doctor.SeverityProblem,
			fmt.Sprintf("region %q is not a known EKS-enabled region", region),
			"Use one of the supported regions: us-east-1, us-east-2, us-west-2, eu-west-1, etc.\n"+
				"See: https://docs.aws.amazon.com/eks/latest/userguide/getting-started.html")
	}

	// Online check: describe-availability-zones proves the region is live
	got, err := awsDescribeRegionFn(ctx, region)
	if err != nil {
		// Don't fail — the offline check passed. Network issue or credentials
		// failure is surfaced by aws.credentials.
		return pass(c.ID(), c.Name(), fmt.Sprintf("region %s is in the EKS-enabled list (online check skipped: %v)", region, err))
	}
	if got != region {
		return fail(c.ID(), c.Name(), doctor.SeverityProblem,
			fmt.Sprintf("region mismatch: configured %s but AWS returned %s", region, got),
			"Verify your WAS_REGION or --region value.")
	}
	return pass(c.ID(), c.Name(), fmt.Sprintf("region %s confirmed", region))
}
