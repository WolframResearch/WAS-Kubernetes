package checks

import (
	"context"
	"fmt"
	"strings"

	"github.com/WolframResearch/WAS-Kubernetes/internal/doctor"
	"github.com/WolframResearch/WAS-Kubernetes/internal/tools"
)

// getCallerIdentityFn is overridable for tests.
var getCallerIdentityFn = func(ctx context.Context, region string) (tools.CallerIdentity, error) {
	return tools.GetCallerIdentity(ctx, region)
}

// AWSCredentials checks that AWS credentials are valid and not expired.
type AWSCredentials struct{ BaseCheck }

func (AWSCredentials) ID() string                { return "aws.credentials" }
func (AWSCredentials) Name() string              { return "AWS credentials valid and unexpired" }
func (AWSCredentials) Category() doctor.Category { return doctor.CategoryEnvironment }
func (AWSCredentials) Description() string {
	return "Calls aws sts get-caller-identity to verify credentials are valid and can authenticate."
}
func awsOnlyPrecondition(rc *doctor.RunContext) (bool, string) {
	if rc.Cfg != nil && rc.Cfg.Cloud == "azure" {
		return true, "AWS checks only apply to AWS deployments"
	}
	return false, ""
}

func (AWSCredentials) Precondition(_ context.Context, rc *doctor.RunContext) (bool, string) {
	return awsOnlyPrecondition(rc)
}

func (c AWSCredentials) Run(ctx context.Context, rc *doctor.RunContext) doctor.Finding {
	id, err := getCallerIdentityFn(ctx, rc.Cfg.MetaRegion.Value)
	if err != nil {
		msg := err.Error()
		remediation := "Configure AWS credentials:\n" +
			"  aws configure\n" +
			"  export AWS_PROFILE=<profile>\n" +
			"  aws sso login\n" +
			"See: docs/Troubleshooting.md#aws-credentials"
		if strings.Contains(msg, "ExpiredToken") || strings.Contains(msg, "expired") {
			msg = "AWS credentials are expired"
			remediation = "Refresh your credentials:\n" +
				"  aws sso login\n" +
				"  Or: aws configure"
		} else if strings.Contains(msg, "NoCredentialProviders") || strings.Contains(msg, "no credential") {
			msg = "No AWS credentials found"
		}
		return fail(c.ID(), c.Name(), doctor.SeverityCritical, msg, remediation)
	}
	return pass(c.ID(), c.Name(), fmt.Sprintf("authenticated as %s (account %s)", id.ARN, id.Account))
}
