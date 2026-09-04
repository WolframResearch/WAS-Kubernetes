package checks

import (
	"context"
	"errors"
	"testing"

	"github.com/WolframResearch/WAS-Kubernetes/internal/doctor"
)

func TestClusterK8sVersion_Pass(t *testing.T) {
	orig := kubectlFn
	defer func() { kubectlFn = orig }()
	kubectlFn = func(_ context.Context, _ *doctor.RunContext, args ...string) ([]byte, error) {
		return []byte(`{"serverVersion":{"gitVersion":"v1.31.2"}}`), nil
	}

	f := ClusterK8sVersion{}.Run(context.Background(), clusterRC())
	if f.Status != doctor.StatusPass {
		t.Fatalf("expected pass for v1.31, got %v: %s", f.Status, f.Message)
	}
}

func TestClusterK8sVersion_Fail_Old(t *testing.T) {
	orig := kubectlFn
	defer func() { kubectlFn = orig }()
	kubectlFn = func(_ context.Context, _ *doctor.RunContext, args ...string) ([]byte, error) {
		return []byte(`{"serverVersion":{"gitVersion":"v1.24.0"}}`), nil
	}

	f := ClusterK8sVersion{}.Run(context.Background(), clusterRC())
	if f.Status != doctor.StatusFail {
		t.Fatalf("expected fail for old k8s version, got %v", f.Status)
	}
}

func TestClusterSystemPods_Pass(t *testing.T) {
	orig := kubectlFn
	defer func() { kubectlFn = orig }()
	kubectlFn = func(_ context.Context, _ *doctor.RunContext, args ...string) ([]byte, error) {
		return []byte(`{"items":[{"metadata":{"name":"coredns"},"status":{"phase":"Running"}}]}`), nil
	}

	f := ClusterSystemPods{}.Run(context.Background(), clusterRC())
	if f.Status != doctor.StatusPass {
		t.Fatalf("expected pass, got %v: %s", f.Status, f.Message)
	}
}

func TestClusterSystemPods_Fail_Pending(t *testing.T) {
	orig := kubectlFn
	defer func() { kubectlFn = orig }()
	kubectlFn = func(_ context.Context, _ *doctor.RunContext, args ...string) ([]byte, error) {
		return []byte(`{"items":[{"metadata":{"name":"coredns"},"status":{"phase":"Pending"}}]}`), nil
	}

	f := ClusterSystemPods{}.Run(context.Background(), clusterRC())
	if f.Status != doctor.StatusFail {
		t.Fatalf("expected fail for pending pod, got %v", f.Status)
	}
}

func TestClusterMetricsServer_Pass(t *testing.T) {
	orig := kubectlFn
	defer func() { kubectlFn = orig }()
	kubectlFn = func(_ context.Context, _ *doctor.RunContext, args ...string) ([]byte, error) {
		return []byte(`{"kind":"Deployment"}`), nil
	}

	f := ClusterMetricsServer{}.Run(context.Background(), clusterRC())
	if f.Status != doctor.StatusPass {
		t.Fatalf("expected pass, got %v: %s", f.Status, f.Message)
	}
}

func TestClusterMetricsServer_NotFoundPass(t *testing.T) {
	orig := kubectlFn
	defer func() { kubectlFn = orig }()
	kubectlFn = func(_ context.Context, _ *doctor.RunContext, args ...string) ([]byte, error) {
		return nil, errors.New("not found")
	}

	f := ClusterMetricsServer{}.Run(context.Background(), clusterRC())
	if f.Status != doctor.StatusPass {
		t.Fatalf("expected pass, got %v", f.Status)
	}
}

func TestClusterIRSA_Pass(t *testing.T) {
	orig := awsEKSDescribeFn
	defer func() { awsEKSDescribeFn = orig }()
	awsEKSDescribeFn = func(_ context.Context, _, _ string) ([]byte, error) {
		return []byte("https://oidc.eks.us-east-1.amazonaws.com/id/ABCDEF"), nil
	}

	rc := clusterRC()
	rc.Cfg.Cloud = "aws"
	f := ClusterIRSA{}.Run(context.Background(), rc)
	if f.Status != doctor.StatusPass {
		t.Fatalf("expected pass, got %v: %s", f.Status, f.Message)
	}
}

func TestClusterIRSA_Fail_NoOIDC(t *testing.T) {
	orig := awsEKSDescribeFn
	defer func() { awsEKSDescribeFn = orig }()
	awsEKSDescribeFn = func(_ context.Context, _, _ string) ([]byte, error) {
		return []byte("None"), nil
	}

	rc := clusterRC()
	rc.Cfg.Cloud = "aws"
	f := ClusterIRSA{}.Run(context.Background(), rc)
	if f.Status != doctor.StatusFail {
		t.Fatalf("expected fail when OIDC is None, got %v", f.Status)
	}
}

func TestClusterUID_Skip_NoWorkspace(t *testing.T) {
	rc := clusterRC() // Workspace is nil
	skip, _ := ClusterUID{}.Precondition(context.Background(), rc)
	if !skip {
		t.Fatal("expected skip when no workspace")
	}
}
