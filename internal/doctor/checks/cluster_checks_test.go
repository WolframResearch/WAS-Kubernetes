package checks

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/WolframResearch/WAS-Kubernetes/internal/doctor"
)

func clusterRC() *doctor.RunContext {
	rc := testRC()
	rc.Kubeconfig = "/tmp/test.kubeconfig"
	rc.ContextName = "arn:aws:eks:us-east-1:123:cluster/test"
	return rc
}

func testRC() *doctor.RunContext {
	return &doctor.RunContext{Cfg: testCfg()}
}

func TestClusterReachable_Pass(t *testing.T) {
	orig := kubectlFn
	defer func() { kubectlFn = orig }()
	kubectlFn = func(_ context.Context, _ *doctor.RunContext, args ...string) ([]byte, error) {
		return []byte("Kubernetes control plane is running at https://test.example.com"), nil
	}

	f := ClusterReachable{}.Run(context.Background(), clusterRC())
	if f.Status != doctor.StatusPass {
		t.Fatalf("expected pass, got %v: %s", f.Status, f.Message)
	}
}

func TestClusterReachable_Fail(t *testing.T) {
	orig := kubectlFn
	defer func() { kubectlFn = orig }()
	kubectlFn = func(_ context.Context, _ *doctor.RunContext, args ...string) ([]byte, error) {
		return nil, errors.New("connection refused")
	}

	f := ClusterReachable{}.Run(context.Background(), clusterRC())
	if f.Status != doctor.StatusFail {
		t.Fatalf("expected fail, got %v", f.Status)
	}
	if f.Severity != doctor.SeverityCritical {
		t.Fatalf("expected critical, got %v", f.Severity)
	}
}

func TestClusterReachable_Skip_NoKubeconfig(t *testing.T) {
	rc := testRC() // Kubeconfig is ""
	skip, _ := ClusterReachable{}.Precondition(context.Background(), rc)
	if !skip {
		t.Fatal("expected skip when no kubeconfig")
	}
}

func TestClusterNodesReady_Pass(t *testing.T) {
	orig := kubectlFn
	defer func() { kubectlFn = orig }()

	nodes := map[string]interface{}{
		"items": []map[string]interface{}{
			{
				"metadata": map[string]string{"name": "node-1"},
				"status": map[string]interface{}{
					"conditions": []map[string]string{{"type": "Ready", "status": "True"}},
				},
			},
		},
	}
	data, _ := json.Marshal(nodes)
	kubectlFn = func(_ context.Context, _ *doctor.RunContext, args ...string) ([]byte, error) {
		return data, nil
	}

	f := ClusterNodesReady{}.Run(context.Background(), clusterRC())
	if f.Status != doctor.StatusPass {
		t.Fatalf("expected pass, got %v: %s", f.Status, f.Message)
	}
}

func TestClusterNodesReady_Fail_NotReady(t *testing.T) {
	orig := kubectlFn
	defer func() { kubectlFn = orig }()

	nodes := map[string]interface{}{
		"items": []map[string]interface{}{
			{
				"metadata": map[string]string{"name": "node-1"},
				"status": map[string]interface{}{
					"conditions": []map[string]string{{"type": "Ready", "status": "False"}},
				},
			},
		},
	}
	data, _ := json.Marshal(nodes)
	kubectlFn = func(_ context.Context, _ *doctor.RunContext, args ...string) ([]byte, error) {
		return data, nil
	}

	f := ClusterNodesReady{}.Run(context.Background(), clusterRC())
	if f.Status != doctor.StatusFail {
		t.Fatalf("expected fail, got %v", f.Status)
	}
}

func TestClusterIRSA_Skip_Azure(t *testing.T) {
	rc := clusterRC()
	rc.Cfg.Cloud = "azure"
	skip, _ := ClusterIRSA{}.Precondition(context.Background(), rc)
	if !skip {
		t.Fatal("expected IRSA check to skip for Azure")
	}
}

func TestClusterWorkloadIdentity_Skip_AWS(t *testing.T) {
	rc := clusterRC()
	rc.Cfg.Cloud = "aws"
	skip, _ := ClusterWorkloadIdentity{}.Precondition(context.Background(), rc)
	if !skip {
		t.Fatal("expected WorkloadIdentity check to skip for AWS")
	}
}
