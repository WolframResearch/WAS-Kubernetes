package checks

// coverage_test.go covers paths that are otherwise missed.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/WolframResearch/WAS-Kubernetes/internal/config"
	"github.com/WolframResearch/WAS-Kubernetes/internal/doctor"
	"github.com/WolframResearch/WAS-Kubernetes/internal/tools"
)

// ── aws_region.go ─────────────────────────────────────────────────────────────

func TestAWSRegion_Fail_RegionMismatch(t *testing.T) {
	orig := awsDescribeRegionFn
	defer func() { awsDescribeRegionFn = orig }()
	awsDescribeRegionFn = func(_ context.Context, _ string) (string, error) {
		return "us-west-2", nil // configured us-east-1 but got us-west-2
	}

	rc := &doctor.RunContext{Cfg: &config.Config{
		Region:     config.Field[string]{Value: "us-east-1"},
		MetaRegion: config.Field[string]{Value: "us-east-1"},
	}}
	f := AWSRegion{}.Run(context.Background(), rc)
	if f.Status != doctor.StatusFail {
		t.Fatalf("expected fail on region mismatch, got %v", f.Status)
	}
}

// ── aws_iam.go ────────────────────────────────────────────────────────────────

func TestAWSIAMCreateOIDCProvider_Pass(t *testing.T) {
	orig := simulatePolicyFn
	defer func() { simulatePolicyFn = orig }()
	simulatePolicyFn = func(_ context.Context, _, _ string) ([]byte, error) {
		return []byte(`{"EvaluationResults":[{"EvalDecision":"allowed"}]}`), nil
	}
	f := AWSIAMCreateOIDCProvider{}.Run(context.Background(), callerRC())
	if f.Status != doctor.StatusPass {
		t.Fatalf("expected pass, got %v: %s", f.Status, f.Message)
	}
}

func TestAWSIAMCreatePolicy_Pass(t *testing.T) {
	orig := simulatePolicyFn
	defer func() { simulatePolicyFn = orig }()
	simulatePolicyFn = func(_ context.Context, _, _ string) ([]byte, error) {
		return []byte(`{"EvaluationResults":[{"EvalDecision":"allowed"}]}`), nil
	}
	f := AWSIAMCreatePolicy{}.Run(context.Background(), callerRC())
	if f.Status != doctor.StatusPass {
		t.Fatalf("expected pass, got %v: %s", f.Status, f.Message)
	}
}

func TestAWSIAMCreateRole_CheckError_ParseFail(t *testing.T) {
	orig := simulatePolicyFn
	defer func() { simulatePolicyFn = orig }()
	simulatePolicyFn = func(_ context.Context, _, _ string) ([]byte, error) {
		return []byte(`not valid json`), nil
	}
	f := AWSIAMCreateRole{}.Run(context.Background(), callerRC())
	if f.Status != doctor.StatusError {
		t.Fatalf("expected error on bad JSON, got %v: %s", f.Status, f.Message)
	}
}

// ── cluster_checks.go ─────────────────────────────────────────────────────────

func TestClusterWorkloadIdentity_Pass(t *testing.T) {
	orig := kubectlFn
	defer func() { kubectlFn = orig }()
	kubectlFn = func(_ context.Context, _ *doctor.RunContext, args ...string) ([]byte, error) {
		return []byte(`{"kind":"Deployment"}`), nil
	}

	rc := clusterRC()
	rc.Cfg.Cloud = "azure"
	f := ClusterWorkloadIdentity{}.Run(context.Background(), rc)
	if f.Status != doctor.StatusPass {
		t.Fatalf("expected pass, got %v: %s", f.Status, f.Message)
	}
}

func TestClusterWorkloadIdentity_Pass_StaticAuthNoWebhook(t *testing.T) {
	orig := kubectlFn
	defer func() { kubectlFn = orig }()
	kubectlFn = func(_ context.Context, _ *doctor.RunContext, args ...string) ([]byte, error) {
		// No WI annotation/labels and no webhook deployments.
		return []byte(""), fmt.Errorf("not found")
	}

	rc := clusterRC()
	rc.Cfg.Cloud = "azure"
	f := ClusterWorkloadIdentity{}.Run(context.Background(), rc)
	if f.Status != doctor.StatusPass {
		t.Fatalf("expected pass when WI unused, got %v: %s", f.Status, f.Message)
	}
}

func TestClusterWorkloadIdentity_Fail_WhenWIRequired(t *testing.T) {
	orig := kubectlFn
	defer func() { kubectlFn = orig }()
	kubectlFn = func(_ context.Context, _ *doctor.RunContext, args ...string) ([]byte, error) {
		joined := strings.Join(args, " ")
		if strings.Contains(joined, "resource-manager") {
			return []byte("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"), nil
		}
		return nil, errors.New("not found")
	}

	rc := clusterRC()
	rc.Cfg.Cloud = "azure"
	f := ClusterWorkloadIdentity{}.Run(context.Background(), rc)
	if f.Status != doctor.StatusFail {
		t.Fatalf("expected fail when WI configured without webhook, got %v", f.Status)
	}
}

func TestClusterK8sVersion_ParseError(t *testing.T) {
	orig := kubectlFn
	defer func() { kubectlFn = orig }()
	kubectlFn = func(_ context.Context, _ *doctor.RunContext, args ...string) ([]byte, error) {
		return []byte(`not json`), nil
	}

	f := ClusterK8sVersion{}.Run(context.Background(), clusterRC())
	if f.Status != doctor.StatusError {
		t.Fatalf("expected error on parse failure, got %v", f.Status)
	}
}

func TestClusterNodesReady_Error_KubectlFails(t *testing.T) {
	orig := kubectlFn
	defer func() { kubectlFn = orig }()
	kubectlFn = func(_ context.Context, _ *doctor.RunContext, args ...string) ([]byte, error) {
		return nil, errors.New("connection refused")
	}

	f := ClusterNodesReady{}.Run(context.Background(), clusterRC())
	if f.Status != doctor.StatusError {
		t.Fatalf("expected error, got %v", f.Status)
	}
}

// ── app_checks.go ─────────────────────────────────────────────────────────────

func TestAppIngressAddress_Fail_NoAddress(t *testing.T) {
	origHelm := helmFn
	origKubectl := kubectlFn
	defer func() { helmFn = origHelm; kubectlFn = origKubectl }()

	helmFn = func(_ context.Context, _ *doctor.RunContext, _ ...string) ([]byte, error) {
		return []byte(`{"info":{"status":"deployed"}}`), nil
	}
	kubectlFn = func(_ context.Context, _ *doctor.RunContext, args ...string) ([]byte, error) {
		return []byte(""), nil
	}

	f := AppIngressAddress{}.Run(context.Background(), deployedRC())
	if f.Status != doctor.StatusFail {
		t.Fatalf("expected fail when no address, got %v", f.Status)
	}
}

func TestAppTLS_Skip_NoHost(t *testing.T) {
	rc := clusterRC()
	rc.Cfg.IngressHost.Value = ""
	skip, _ := AppTLS{}.Precondition(context.Background(), rc)
	if !skip {
		t.Fatal("expected skip for TLS when no ingress host")
	}
}

func TestCLIKubectl_Pass(t *testing.T) {
	origLook := lookPath
	origVer := cliVersionOutputFn
	defer func() { lookPath = origLook; cliVersionOutputFn = origVer }()

	lookPath = func(s string) (string, error) { return "/usr/bin/" + s, nil }
	cliVersionOutputFn = func(_ context.Context, _ string, _ []string) (string, error) {
		return "Client Version: v1.30.2", nil
	}

	rc := &doctor.RunContext{Cfg: testCfg()}
	f := CLIKubectl.Run(context.Background(), rc)
	if f.Status != doctor.StatusPass {
		t.Fatalf("expected pass, got %v: %s", f.Status, f.Message)
	}
}

func TestCLIAWS_Pass(t *testing.T) {
	origLook := lookPath
	origVer := cliVersionOutputFn
	defer func() { lookPath = origLook; cliVersionOutputFn = origVer }()

	lookPath = func(s string) (string, error) { return "/usr/bin/" + s, nil }
	cliVersionOutputFn = func(_ context.Context, _ string, _ []string) (string, error) {
		return "aws-cli/2.15.0 Python/3.11.0 Linux/6.8", nil
	}

	rc := &doctor.RunContext{Cfg: testCfg()}
	f := CLIAWS.Run(context.Background(), rc)
	if f.Status != doctor.StatusPass {
		t.Fatalf("expected pass, got %v: %s", f.Status, f.Message)
	}
}

func TestCLIAWS_Fail_V1(t *testing.T) {
	origLook := lookPath
	origVer := cliVersionOutputFn
	defer func() { lookPath = origLook; cliVersionOutputFn = origVer }()

	lookPath = func(s string) (string, error) { return "/usr/bin/" + s, nil }
	cliVersionOutputFn = func(_ context.Context, _ string, _ []string) (string, error) {
		return "aws-cli/1.18.0 Python/3.8.0 Linux/5.4", nil
	}

	rc := &doctor.RunContext{Cfg: testCfg()}
	f := CLIAWS.Run(context.Background(), rc)
	if f.Status != doctor.StatusFail {
		t.Fatalf("expected fail for aws-cli v1, got %v: %s", f.Status, f.Message)
	}
}

// ── meta_bucket_common.go helpers ────────────────────────────────────────────

func TestMetaBucketName_NoCreds(t *testing.T) {
	rc := &doctor.RunContext{Cfg: testCfg()} // CallerID nil
	_, err := metaBucketName(rc)
	if err == nil {
		t.Fatal("expected error when CallerID is nil")
	}
}

func TestLockTableName_NoCreds(t *testing.T) {
	rc := &doctor.RunContext{Cfg: testCfg()} // CallerID nil
	_, err := lockTableName(rc)
	if err == nil {
		t.Fatal("expected error when CallerID is nil")
	}
}

func TestMetaBucketNames_WithCreds(t *testing.T) {
	id := tools.CallerIdentity{Account: "123456789012"}
	rc := &doctor.RunContext{Cfg: testCfg()}
	rc.CallerID = &id

	name, err := metaBucketName(rc)
	if err != nil || name == "" {
		t.Fatalf("unexpected error or empty name: %v %s", err, name)
	}

	table, err := lockTableName(rc)
	if err != nil || table == "" {
		t.Fatalf("unexpected error or empty table: %v %s", err, table)
	}
}
