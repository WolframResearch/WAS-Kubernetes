package checks

import (
	"context"
	"errors"
	"testing"

	"github.com/WolframResearch/WAS-Kubernetes/internal/doctor"
)

func TestAWSQuotaVPC_Pass(t *testing.T) {
	orig := getServiceQuotaFn
	defer func() { getServiceQuotaFn = orig }()
	getServiceQuotaFn = func(_ context.Context, _, _, _ string) (float64, error) {
		return 20.0, nil
	}

	f := AWSQuotaVPC.Run(context.Background(), callerRC())
	if f.Status != doctor.StatusPass {
		t.Fatalf("expected pass, got %v: %s", f.Status, f.Message)
	}
}

func TestAWSQuotaVPC_Fail_TooLow(t *testing.T) {
	orig := getServiceQuotaFn
	defer func() { getServiceQuotaFn = orig }()
	getServiceQuotaFn = func(_ context.Context, _, _, _ string) (float64, error) {
		return 2.0, nil
	}

	f := AWSQuotaVPC.Run(context.Background(), callerRC())
	if f.Status != doctor.StatusFail {
		t.Fatalf("expected fail, got %v", f.Status)
	}
}

func TestAWSQuotaVPC_Skip_AccessDenied(t *testing.T) {
	orig := getServiceQuotaFn
	defer func() { getServiceQuotaFn = orig }()
	getServiceQuotaFn = func(_ context.Context, _, _, _ string) (float64, error) {
		return 0, errors.New("AccessDenied: User is not authorized to perform servicequotas:GetServiceQuota")
	}

	f := AWSQuotaVPC.Run(context.Background(), callerRC())
	if f.Status != doctor.StatusSkip {
		t.Fatalf("expected skip on AccessDenied, got %v: %s", f.Status, f.Message)
	}
}

func TestAWSQuotaVPC_Skip_NoCredentials(t *testing.T) {
	rc := &doctor.RunContext{Cfg: testCfg()} // CallerID is nil
	skip, _ := AWSQuotaVPC.Precondition(context.Background(), rc)
	if !skip {
		t.Fatal("expected precondition to skip when no credentials")
	}
}
