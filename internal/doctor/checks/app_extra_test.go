package checks

import (
	"context"
	"errors"
	"testing"

	"github.com/WolframResearch/WAS-Kubernetes/internal/doctor"
)

func TestChartDeployed_Skip_NoKubeconfig(t *testing.T) {
	rc := testRC() // no kubeconfig
	skip, _ := ChartDeployed{}.Precondition(context.Background(), rc)
	if !skip {
		t.Fatal("expected skip when no kubeconfig")
	}
}

func TestAppKafka_Pass(t *testing.T) {
	origHelm := helmFn
	origKubectl := kubectlFn
	defer func() { helmFn = origHelm; kubectlFn = origKubectl }()

	helmFn = func(_ context.Context, _ *doctor.RunContext, _ ...string) ([]byte, error) {
		return []byte(`{"info":{"status":"deployed"}}`), nil
	}
	kubectlFn = func(_ context.Context, _ *doctor.RunContext, args ...string) ([]byte, error) {
		return []byte(`{"items":[{"metadata":{"name":"kafka-persistent"},"status":{"conditions":[{"type":"Ready","status":"True"}]}}]}`), nil
	}

	f := AppKafka{}.Run(context.Background(), deployedRC())
	if f.Status != doctor.StatusPass {
		t.Fatalf("expected pass, got %v: %s", f.Status, f.Message)
	}
}

func TestAppKafka_Fail_NotReady(t *testing.T) {
	origHelm := helmFn
	origKubectl := kubectlFn
	defer func() { helmFn = origHelm; kubectlFn = origKubectl }()

	helmFn = func(_ context.Context, _ *doctor.RunContext, _ ...string) ([]byte, error) {
		return []byte(`{"info":{"status":"deployed"}}`), nil
	}
	kubectlFn = func(_ context.Context, _ *doctor.RunContext, args ...string) ([]byte, error) {
		return []byte(`{"items":[{"metadata":{"name":"kafka-persistent"},"status":{"conditions":[{"type":"Ready","status":"False"}]}}]}`), nil
	}

	f := AppKafka{}.Run(context.Background(), deployedRC())
	if f.Status != doctor.StatusFail {
		t.Fatalf("expected fail when Kafka not Ready, got %v", f.Status)
	}
}

func TestAppKafkaTopics_Pass(t *testing.T) {
	origHelm := helmFn
	origKubectl := kubectlFn
	defer func() { helmFn = origHelm; kubectlFn = origKubectl }()

	helmFn = func(_ context.Context, _ *doctor.RunContext, _ ...string) ([]byte, error) {
		return []byte(`{"info":{"status":"deployed"}}`), nil
	}
	kubectlFn = func(_ context.Context, _ *doctor.RunContext, args ...string) ([]byte, error) {
		return []byte("api-request api-response resource-info nodefile-info endpoint-info"), nil
	}

	f := AppKafkaTopics{}.Run(context.Background(), deployedRC())
	if f.Status != doctor.StatusPass {
		t.Fatalf("expected pass, got %v: %s", f.Status, f.Message)
	}
}

func TestAppKafkaTopics_Fail_Missing(t *testing.T) {
	origHelm := helmFn
	origKubectl := kubectlFn
	defer func() { helmFn = origHelm; kubectlFn = origKubectl }()

	helmFn = func(_ context.Context, _ *doctor.RunContext, _ ...string) ([]byte, error) {
		return []byte(`{"info":{"status":"deployed"}}`), nil
	}
	kubectlFn = func(_ context.Context, _ *doctor.RunContext, args ...string) ([]byte, error) {
		return []byte("api-request api-response"), nil // missing 3 topics
	}

	f := AppKafkaTopics{}.Run(context.Background(), deployedRC())
	if f.Status != doctor.StatusFail {
		t.Fatalf("expected fail for missing topics, got %v", f.Status)
	}
}

func TestAppStorage_Pass(t *testing.T) {
	origHelm := helmFn
	origKubectl := kubectlFn
	defer func() { helmFn = origHelm; kubectlFn = origKubectl }()

	helmFn = func(_ context.Context, _ *doctor.RunContext, _ ...string) ([]byte, error) {
		return []byte(`{"info":{"status":"deployed"}}`), nil
	}
	kubectlFn = func(_ context.Context, _ *doctor.RunContext, args ...string) ([]byte, error) {
		return []byte(`{"items":[{"metadata":{"name":"pvc-0"},"status":{"phase":"Bound"}}]}`), nil
	}

	f := AppStorage{}.Run(context.Background(), deployedRC())
	if f.Status != doctor.StatusPass {
		t.Fatalf("expected pass, got %v: %s", f.Status, f.Message)
	}
}

func TestAppStorage_Fail_Pending(t *testing.T) {
	origHelm := helmFn
	origKubectl := kubectlFn
	defer func() { helmFn = origHelm; kubectlFn = origKubectl }()

	helmFn = func(_ context.Context, _ *doctor.RunContext, _ ...string) ([]byte, error) {
		return []byte(`{"info":{"status":"deployed"}}`), nil
	}
	kubectlFn = func(_ context.Context, _ *doctor.RunContext, args ...string) ([]byte, error) {
		return []byte(`{"items":[{"metadata":{"name":"pvc-0"},"status":{"phase":"Pending"}}]}`), nil
	}

	f := AppStorage{}.Run(context.Background(), deployedRC())
	if f.Status != doctor.StatusFail {
		t.Fatalf("expected fail for pending PVC, got %v", f.Status)
	}
}

func TestAppIngressAddress_Pass(t *testing.T) {
	origHelm := helmFn
	origKubectl := kubectlFn
	defer func() { helmFn = origHelm; kubectlFn = origKubectl }()

	helmFn = func(_ context.Context, _ *doctor.RunContext, _ ...string) ([]byte, error) {
		return []byte(`{"info":{"status":"deployed"}}`), nil
	}
	callCount := 0
	kubectlFn = func(_ context.Context, _ *doctor.RunContext, args ...string) ([]byte, error) {
		callCount++
		if callCount == 1 {
			return []byte("my-elb.us-east-1.elb.amazonaws.com"), nil
		}
		return []byte(""), nil
	}

	f := AppIngressAddress{}.Run(context.Background(), deployedRC())
	if f.Status != doctor.StatusPass {
		t.Fatalf("expected pass, got %v: %s", f.Status, f.Message)
	}
}

func TestAppObjectStorage_Fail_AccessDenied(t *testing.T) {
	origHelm := helmFn
	origKubectl := kubectlFn
	defer func() { helmFn = origHelm; kubectlFn = origKubectl }()

	helmFn = func(_ context.Context, _ *doctor.RunContext, _ ...string) ([]byte, error) {
		return []byte(`{"info":{"status":"deployed"}}`), nil
	}
	kubectlFn = func(_ context.Context, _ *doctor.RunContext, args ...string) ([]byte, error) {
		return []byte("2024-01-01 ERROR AccessDenied when accessing S3 bucket"), nil
	}

	f := AppObjectStorage{}.Run(context.Background(), deployedRC())
	if f.Status != doctor.StatusFail {
		t.Fatalf("expected fail on AccessDenied in logs, got %v", f.Status)
	}
}

func TestChartValuesValid_Pass(t *testing.T) {
	origHelm := helmFn
	defer func() { helmFn = origHelm }()

	callCount := 0
	helmFn = func(_ context.Context, _ *doctor.RunContext, args ...string) ([]byte, error) {
		callCount++
		return []byte(`{"info":{"status":"deployed"}}`), nil
	}

	f := ChartValuesValid{}.Run(context.Background(), deployedRC())
	if f.Status != doctor.StatusPass {
		t.Fatalf("expected pass, got %v: %s", f.Status, f.Message)
	}
}

func TestAppResourceManager_Skip_ChartNotDeployed(t *testing.T) {
	origHelm := helmFn
	defer func() { helmFn = origHelm }()
	helmFn = func(_ context.Context, _ *doctor.RunContext, _ ...string) ([]byte, error) {
		return nil, errors.New("release not found")
	}

	rc := clusterRC()
	skip, _ := AppResourceManager.Precondition(context.Background(), rc)
	if !skip {
		t.Fatal("expected skip when chart not deployed")
	}
}
