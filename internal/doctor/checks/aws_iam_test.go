package checks

import (
	"context"
	"errors"
	"testing"

	"github.com/WolframResearch/WAS-Kubernetes/internal/doctor"
	"github.com/WolframResearch/WAS-Kubernetes/internal/tools"
)

func callerRC() *doctor.RunContext {
	id := tools.CallerIdentity{Account: "123456789012", ARN: "arn:aws:iam::123456789012:user/test"}
	rc := &doctor.RunContext{Cfg: testCfg()}
	rc.CallerID = &id
	return rc
}

func TestAWSIAMCreateRole_Pass(t *testing.T) {
	orig := simulatePolicyFn
	defer func() { simulatePolicyFn = orig }()
	simulatePolicyFn = func(_ context.Context, _, _ string) ([]byte, error) {
		return []byte(`{"EvaluationResults":[{"EvalDecision":"allowed"}]}`), nil
	}
	f := AWSIAMCreateRole{}.Run(context.Background(), callerRC())
	if f.Status != doctor.StatusPass {
		t.Fatalf("expected pass, got %v: %s", f.Status, f.Message)
	}
}

func TestAWSIAMCreateRole_Fail_Denied(t *testing.T) {
	orig := simulatePolicyFn
	defer func() { simulatePolicyFn = orig }()
	simulatePolicyFn = func(_ context.Context, _, _ string) ([]byte, error) {
		return []byte(`{"EvaluationResults":[{"EvalDecision":"implicitDeny"}]}`), nil
	}
	f := AWSIAMCreateRole{}.Run(context.Background(), callerRC())
	if f.Status != doctor.StatusFail {
		t.Fatalf("expected fail, got %v", f.Status)
	}
}

func TestAWSIAMCreateRole_Skip_SimulateDenied(t *testing.T) {
	orig := simulatePolicyFn
	defer func() { simulatePolicyFn = orig }()
	simulatePolicyFn = func(_ context.Context, _, _ string) ([]byte, error) {
		return nil, errors.New("AccessDenied: User is not authorized to perform iam:SimulatePrincipalPolicy")
	}
	f := AWSIAMCreateRole{}.Run(context.Background(), callerRC())
	if f.Status != doctor.StatusSkip {
		t.Fatalf("expected skip when simulate-policy is denied, got %v", f.Status)
	}
}
