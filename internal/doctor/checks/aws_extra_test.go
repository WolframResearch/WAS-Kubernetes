package checks

import (
	"context"
	"testing"

	"github.com/WolframResearch/WAS-Kubernetes/internal/config"
	"github.com/WolframResearch/WAS-Kubernetes/internal/doctor"
)

func TestAWSRegion_EmptyRegion(t *testing.T) {
	rc := &doctor.RunContext{Cfg: &config.Config{
		Region:     config.Field[string]{Value: ""},
		MetaRegion: config.Field[string]{Value: "us-east-1"},
	}}
	f := AWSRegion{}.Run(context.Background(), rc)
	if f.Status != doctor.StatusFail {
		t.Fatalf("expected fail for empty region, got %v", f.Status)
	}
}

func TestAWSRegion_NoPrecondition(t *testing.T) {
	// AWSRegion has no precondition — always runs
	rc := &doctor.RunContext{Cfg: testCfg()}
	skip, _ := AWSRegion{}.Precondition(context.Background(), rc)
	if skip {
		t.Fatal("AWSRegion should not skip by precondition")
	}
}

func TestAWSCredentials_NoPrecondition(t *testing.T) {
	rc := &doctor.RunContext{Cfg: testCfg()}
	skip, _ := AWSCredentials{}.Precondition(context.Background(), rc)
	if skip {
		t.Fatal("AWSCredentials should not skip by precondition")
	}
}

func TestAWSIAMCreatePolicy_SkipIfNoCreds(t *testing.T) {
	rc := &doctor.RunContext{Cfg: testCfg()} // CallerID is nil
	skip, reason := AWSIAMCreatePolicy{}.Precondition(context.Background(), rc)
	if !skip {
		t.Fatal("expected skip when CallerID is nil")
	}
	if reason == "" {
		t.Fatal("expected non-empty skip reason")
	}
}

func TestVersionMatrix_NoPrecondition(t *testing.T) {
	rc := &doctor.RunContext{Cfg: testCfg()}
	skip, _ := VersionMatrix{}.Precondition(context.Background(), rc)
	if skip {
		t.Fatal("VersionMatrix should not have a failing precondition")
	}
}

func TestCLIAz_Pass_Azure(t *testing.T) {
	origLook := lookPath
	origVer := cliVersionOutputFn
	defer func() { lookPath = origLook; cliVersionOutputFn = origVer }()

	lookPath = func(s string) (string, error) { return "/usr/bin/az", nil }
	cliVersionOutputFn = func(_ context.Context, _ string, _ []string) (string, error) {
		return "2.62.0", nil
	}

	rc := &doctor.RunContext{Cfg: &config.Config{
		Cloud:      "azure",
		MetaRegion: config.Field[string]{Value: "us-east-1"},
		Region:     config.Field[string]{Value: "us-east-1"},
	}}
	f := CLIAz.Run(context.Background(), rc)
	if f.Status != doctor.StatusPass {
		t.Fatalf("expected pass for azure az CLI, got %v: %s", f.Status, f.Message)
	}
}

func TestAWSQuotaEKSClusters_Pass(t *testing.T) {
	orig := getServiceQuotaFn
	defer func() { getServiceQuotaFn = orig }()
	getServiceQuotaFn = func(_ context.Context, _, _, _ string) (float64, error) {
		return 50.0, nil
	}

	f := AWSQuotaEKSClusters.Run(context.Background(), callerRC())
	if f.Status != doctor.StatusPass {
		t.Fatalf("expected pass, got %v: %s", f.Status, f.Message)
	}
}

func TestAWSQuotaEC2Instances_Fail(t *testing.T) {
	orig := getServiceQuotaFn
	defer func() { getServiceQuotaFn = orig }()
	getServiceQuotaFn = func(_ context.Context, _, _, _ string) (float64, error) {
		return 4.0, nil
	}

	f := AWSQuotaEC2Instances.Run(context.Background(), callerRC())
	if f.Status != doctor.StatusFail {
		t.Fatalf("expected fail for low EC2 quota, got %v", f.Status)
	}
	if f.Severity != doctor.SeverityProblem {
		t.Fatalf("expected Problem severity, got %v", f.Severity)
	}
}
