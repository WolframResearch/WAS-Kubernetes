package checks

import (
	"context"
	"errors"
	"testing"

	"github.com/WolframResearch/WAS-Kubernetes/internal/config"
	"github.com/WolframResearch/WAS-Kubernetes/internal/doctor"
	"github.com/WolframResearch/WAS-Kubernetes/internal/tools"
)

func TestAWSCredentials_Pass(t *testing.T) {
	orig := getCallerIdentityFn
	defer func() { getCallerIdentityFn = orig }()
	getCallerIdentityFn = func(_ context.Context, _ string) (tools.CallerIdentity, error) {
		return tools.CallerIdentity{Account: "123456789012", ARN: "arn:aws:iam::123456789012:user/test"}, nil
	}

	rc := &doctor.RunContext{Cfg: testCfg()}
	f := AWSCredentials{}.Run(context.Background(), rc)
	if f.Status != doctor.StatusPass {
		t.Fatalf("expected pass, got %v: %s", f.Status, f.Message)
	}
}

func TestAWSCredentials_Fail_Expired(t *testing.T) {
	orig := getCallerIdentityFn
	defer func() { getCallerIdentityFn = orig }()
	getCallerIdentityFn = func(_ context.Context, _ string) (tools.CallerIdentity, error) {
		return tools.CallerIdentity{}, errors.New("ExpiredToken: token is expired")
	}

	rc := &doctor.RunContext{Cfg: testCfg()}
	f := AWSCredentials{}.Run(context.Background(), rc)
	if f.Status != doctor.StatusFail {
		t.Fatalf("expected fail, got %v", f.Status)
	}
	if f.Severity != doctor.SeverityCritical {
		t.Fatalf("expected critical, got %v", f.Severity)
	}
}

func TestAWSCredentials_Fail_NoCredentials(t *testing.T) {
	orig := getCallerIdentityFn
	defer func() { getCallerIdentityFn = orig }()
	getCallerIdentityFn = func(_ context.Context, _ string) (tools.CallerIdentity, error) {
		return tools.CallerIdentity{}, errors.New("NoCredentialProviders")
	}

	rc := &doctor.RunContext{Cfg: testCfg()}
	f := AWSCredentials{}.Run(context.Background(), rc)
	if f.Status != doctor.StatusFail {
		t.Fatalf("expected fail, got %v", f.Status)
	}
}

// testCfg returns a minimal config for tests.
func testCfg() *config.Config {
	return &config.Config{
		MetaRegion:  config.Field[string]{Value: "us-east-1"},
		Region:      config.Field[string]{Value: "us-east-1"},
		ClusterName: config.Field[string]{Value: "test-cluster"},
	}
}
