package checks

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/WolframResearch/WAS-Kubernetes/internal/doctor"
)

func deployedRC() *doctor.RunContext {
	rc := clusterRC()
	// appPrecondition checks helm status; mock helmFn to return "deployed"
	return rc
}

func mockHelmDeployed() func() {
	origHelm := helmFn
	helmFn = func(_ context.Context, _ *doctor.RunContext, args ...string) ([]byte, error) {
		return []byte(`{"info":{"status":"deployed"}}`), nil
	}
	return func() { helmFn = origHelm }
}

func TestChartDeployed_Pass(t *testing.T) {
	restore := mockHelmDeployed()
	defer restore()

	f := ChartDeployed{}.Run(context.Background(), clusterRC())
	if f.Status != doctor.StatusPass {
		t.Fatalf("expected pass, got %v: %s", f.Status, f.Message)
	}
}

func TestChartDeployed_Fail_NotFound(t *testing.T) {
	origHelm := helmFn
	defer func() { helmFn = origHelm }()
	helmFn = func(_ context.Context, _ *doctor.RunContext, args ...string) ([]byte, error) {
		return nil, errors.New("release not found")
	}

	f := ChartDeployed{}.Run(context.Background(), clusterRC())
	if f.Status != doctor.StatusFail {
		t.Fatalf("expected fail, got %v", f.Status)
	}
	if f.Severity != doctor.SeverityCritical {
		t.Fatalf("expected critical, got %v", f.Severity)
	}
}

func TestAppAWES_Pass(t *testing.T) {
	origHelm := helmFn
	origKubectl := kubectlFn
	defer func() { helmFn = origHelm; kubectlFn = origKubectl }()

	helmFn = func(_ context.Context, _ *doctor.RunContext, args ...string) ([]byte, error) {
		return []byte(`{"info":{"status":"deployed"}}`), nil
	}

	dep := map[string]interface{}{
		"status": map[string]interface{}{
			"replicas":      int32(2),
			"readyReplicas": int32(2),
		},
	}
	data, _ := json.Marshal(dep)
	kubectlFn = func(_ context.Context, _ *doctor.RunContext, args ...string) ([]byte, error) {
		return data, nil
	}

	f := AppAWES.Run(context.Background(), deployedRC())
	if f.Status != doctor.StatusPass {
		t.Fatalf("expected pass, got %v: %s", f.Status, f.Message)
	}
}

func TestAppAWES_Fail_NotReady(t *testing.T) {
	origHelm := helmFn
	origKubectl := kubectlFn
	defer func() { helmFn = origHelm; kubectlFn = origKubectl }()

	helmFn = func(_ context.Context, _ *doctor.RunContext, args ...string) ([]byte, error) {
		return []byte(`{"info":{"status":"deployed"}}`), nil
	}

	dep := map[string]interface{}{
		"status": map[string]interface{}{
			"replicas":      int32(2),
			"readyReplicas": int32(0),
		},
	}
	data, _ := json.Marshal(dep)
	kubectlFn = func(_ context.Context, _ *doctor.RunContext, args ...string) ([]byte, error) {
		return data, nil
	}

	f := AppAWES.Run(context.Background(), deployedRC())
	if f.Status != doctor.StatusFail {
		t.Fatalf("expected fail, got %v", f.Status)
	}
}

func TestAppIngressDNS_Skip_NoHost(t *testing.T) {
	rc := clusterRC()
	rc.Cfg.IngressHost.Value = ""
	skip, _ := AppIngressDNS{}.Precondition(context.Background(), rc)
	if !skip {
		t.Fatal("expected skip when no ingress host configured")
	}
}

func TestAppIngressDNS_Pass(t *testing.T) {
	origHelm := helmFn
	origLookup := lookupHostFn
	defer func() { helmFn = origHelm; lookupHostFn = origLookup }()

	helmFn = func(_ context.Context, _ *doctor.RunContext, _ ...string) ([]byte, error) {
		return []byte(`{"info":{"status":"deployed"}}`), nil
	}
	lookupHostFn = func(host string) ([]string, error) {
		return []string{"1.2.3.4"}, nil
	}

	rc := clusterRC()
	rc.Cfg.IngressHost.Value = "was.example.com"
	f := AppIngressDNS{}.Run(context.Background(), rc)
	if f.Status != doctor.StatusPass {
		t.Fatalf("expected pass, got %v: %s", f.Status, f.Message)
	}
}

func TestAppIngressDNS_Fail_NoDNS(t *testing.T) {
	origHelm := helmFn
	origLookup := lookupHostFn
	defer func() { helmFn = origHelm; lookupHostFn = origLookup }()

	helmFn = func(_ context.Context, _ *doctor.RunContext, _ ...string) ([]byte, error) {
		return []byte(`{"info":{"status":"deployed"}}`), nil
	}
	lookupHostFn = func(host string) ([]string, error) {
		return nil, errors.New("no such host")
	}

	rc := clusterRC()
	rc.Cfg.IngressHost.Value = "was.example.com"
	f := AppIngressDNS{}.Run(context.Background(), rc)
	if f.Status != doctor.StatusFail {
		t.Fatalf("expected fail on DNS error, got %v", f.Status)
	}
}
