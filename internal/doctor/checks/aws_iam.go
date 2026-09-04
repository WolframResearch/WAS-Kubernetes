package checks

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/WolframResearch/WAS-Kubernetes/internal/doctor"
)

// simulatePolicyFn shells out to aws iam simulate-principal-policy.
// Overridable for tests.
var simulatePolicyFn = func(ctx context.Context, arn, action string) ([]byte, error) {
	return runOutput(ctx, "aws", "iam", "simulate-principal-policy",
		"--policy-source-arn", arn,
		"--action-names", action,
		"--output", "json")
}

type iamSimResult struct {
	EvaluationResults []struct {
		EvalDecision string `json:"EvalDecision"`
	} `json:"EvaluationResults"`
}

func checkIAMAction(ctx context.Context, rc *doctor.RunContext, action string) (bool, error) {
	if rc.CallerID == nil {
		return false, fmt.Errorf("caller identity unavailable")
	}
	out, err := simulatePolicyFn(ctx, rc.CallerID.ARN, action)
	if err != nil {
		// If simulate-principal-policy itself fails (insufficient perms), treat as unknown
		return false, fmt.Errorf("simulate-principal-policy: %w", err)
	}
	var result iamSimResult
	if err := json.Unmarshal(out, &result); err != nil {
		return false, fmt.Errorf("parse simulate result: %w", err)
	}
	for _, r := range result.EvaluationResults {
		if strings.EqualFold(r.EvalDecision, "allowed") {
			return true, nil
		}
	}
	return false, nil
}

// AWSIAMCreateRole checks the caller can create IAM roles.
type AWSIAMCreateRole struct{ BaseCheck }

func (AWSIAMCreateRole) ID() string                { return "aws.iam.create_role" }
func (AWSIAMCreateRole) Name() string              { return "Can create IAM roles" }
func (AWSIAMCreateRole) Category() doctor.Category { return doctor.CategoryEnvironment }
func (AWSIAMCreateRole) Description() string {
	return "Verifies iam:CreateRole is allowed for the current identity."
}
func (c AWSIAMCreateRole) Precondition(_ context.Context, rc *doctor.RunContext) (bool, string) {
	if skip, reason := awsOnlyPrecondition(rc); skip {
		return true, reason
	}
	if rc.CallerID == nil {
		return true, "AWS credentials unavailable; check aws.credentials first"
	}
	return false, ""
}
func (c AWSIAMCreateRole) Run(ctx context.Context, rc *doctor.RunContext) doctor.Finding {
	return runIAMCheck(ctx, rc, c.ID(), c.Name(), "iam:CreateRole",
		"Add iam:CreateRole to your IAM policy.\nSee: README.md (Prerequisites) and docs/Install.md")
}

// AWSIAMCreateOIDCProvider checks the caller can create OIDC providers.
type AWSIAMCreateOIDCProvider struct{ BaseCheck }

func (AWSIAMCreateOIDCProvider) ID() string                { return "aws.iam.create_oidc_provider" }
func (AWSIAMCreateOIDCProvider) Name() string              { return "Can create OIDC provider" }
func (AWSIAMCreateOIDCProvider) Category() doctor.Category { return doctor.CategoryEnvironment }
func (AWSIAMCreateOIDCProvider) Description() string {
	return "Verifies iam:CreateOpenIDConnectProvider is allowed (required for IRSA)."
}
func (c AWSIAMCreateOIDCProvider) Precondition(_ context.Context, rc *doctor.RunContext) (bool, string) {
	if skip, reason := awsOnlyPrecondition(rc); skip {
		return true, reason
	}
	if rc.CallerID == nil {
		return true, "AWS credentials unavailable; check aws.credentials first"
	}
	return false, ""
}
func (c AWSIAMCreateOIDCProvider) Run(ctx context.Context, rc *doctor.RunContext) doctor.Finding {
	return runIAMCheck(ctx, rc, c.ID(), c.Name(), "iam:CreateOpenIDConnectProvider",
		"Add iam:CreateOpenIDConnectProvider to your IAM policy.\nRequired for EKS IRSA setup.")
}

// AWSIAMCreatePolicy checks the caller can create IAM policies.
type AWSIAMCreatePolicy struct{ BaseCheck }

func (AWSIAMCreatePolicy) ID() string                { return "aws.iam.create_policy" }
func (AWSIAMCreatePolicy) Name() string              { return "Can create scoped IAM policy" }
func (AWSIAMCreatePolicy) Category() doctor.Category { return doctor.CategoryEnvironment }
func (AWSIAMCreatePolicy) Description() string {
	return "Verifies iam:CreatePolicy is allowed (required for Terraform-managed policies)."
}
func (c AWSIAMCreatePolicy) Precondition(_ context.Context, rc *doctor.RunContext) (bool, string) {
	if skip, reason := awsOnlyPrecondition(rc); skip {
		return true, reason
	}
	if rc.CallerID == nil {
		return true, "AWS credentials unavailable; check aws.credentials first"
	}
	return false, ""
}
func (c AWSIAMCreatePolicy) Run(ctx context.Context, rc *doctor.RunContext) doctor.Finding {
	return runIAMCheck(ctx, rc, c.ID(), c.Name(), "iam:CreatePolicy",
		"Add iam:CreatePolicy to your IAM policy.\nSee: README.md (Prerequisites) and docs/Install.md")
}

func runIAMCheck(ctx context.Context, rc *doctor.RunContext, id, name, action, remediation string) doctor.Finding {
	allowed, err := checkIAMAction(ctx, rc, action)
	if err != nil {
		// simulate-principal-policy requires iam:SimulatePrincipalPolicy; if denied, skip gracefully
		if strings.Contains(err.Error(), "AccessDenied") || strings.Contains(err.Error(), "is not authorized") {
			return skip(id, name, fmt.Sprintf("IAM identity lacks iam:SimulatePrincipalPolicy; cannot verify %s", action))
		}
		return checkError(id, name, fmt.Sprintf("IAM simulation failed: %v", err))
	}
	if !allowed {
		return fail(id, name, doctor.SeverityProblem,
			fmt.Sprintf("%s is denied for current identity", action), remediation)
	}
	return pass(id, name, fmt.Sprintf("%s allowed", action))
}
