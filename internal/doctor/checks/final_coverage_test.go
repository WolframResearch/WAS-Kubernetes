package checks

// final_coverage_test.go covers the remaining missed branches.

import (
	"context"
	"errors"
	"testing"

	"github.com/WolframResearch/WAS-Kubernetes/internal/doctor"
	"github.com/WolframResearch/WAS-Kubernetes/internal/metabucket"
	"github.com/WolframResearch/WAS-Kubernetes/internal/tools"
	"github.com/WolframResearch/WAS-Kubernetes/internal/workspace"
)

// wsWithMeta creates a minimal *workspace.Workspace for tests.
func wsWithMeta(uid string) *workspace.Workspace {
	ws := &workspace.Workspace{}
	ws.Meta = &metabucket.Metadata{ClusterUID: uid, ClusterName: "test"}
	return ws
}

// ClusterUID — Run with matching UID
func TestClusterUID_Pass_Matches(t *testing.T) {
	orig := kubectlFn
	defer func() { kubectlFn = orig }()
	kubectlFn = func(_ context.Context, _ *doctor.RunContext, args ...string) ([]byte, error) {
		return []byte("abc-def-123"), nil
	}

	id := tools.CallerIdentity{Account: "123456789012"}
	rc := clusterRC()
	rc.CallerID = &id
	rc.Workspace = wsWithMeta("abc-def-123")
	f := ClusterUID{}.Run(context.Background(), rc)
	if f.Status != doctor.StatusPass {
		t.Fatalf("expected pass on matching UID, got %v: %s", f.Status, f.Message)
	}
}

// ClusterUID — Run with stored UID empty (first install)
func TestClusterUID_Pass_NoStoredUID(t *testing.T) {
	orig := kubectlFn
	defer func() { kubectlFn = orig }()
	kubectlFn = func(_ context.Context, _ *doctor.RunContext, args ...string) ([]byte, error) {
		return []byte("live-uid"), nil
	}

	rc := clusterRC()
	rc.Workspace = wsWithMeta("") // empty UID = not yet stored
	f := ClusterUID{}.Run(context.Background(), rc)
	if f.Status != doctor.StatusPass {
		t.Fatalf("expected pass when no UID stored yet, got %v: %s", f.Status, f.Message)
	}
}

// ClusterUID — Run with mismatching UID
func TestClusterUID_Fail_Mismatch(t *testing.T) {
	orig := kubectlFn
	defer func() { kubectlFn = orig }()
	kubectlFn = func(_ context.Context, _ *doctor.RunContext, args ...string) ([]byte, error) {
		return []byte("live-uid-999"), nil
	}

	rc := clusterRC()
	rc.Workspace = wsWithMeta("stored-uid-000")
	f := ClusterUID{}.Run(context.Background(), rc)
	if f.Status != doctor.StatusFail {
		t.Fatalf("expected fail on UID mismatch, got %v", f.Status)
	}
	if f.Severity != doctor.SeverityCritical {
		t.Fatalf("expected critical severity, got %v", f.Severity)
	}
}

// AWSCredentials — generic error (not expired, not no-creds)
func TestAWSCredentials_Fail_GenericError(t *testing.T) {
	orig := getCallerIdentityFn
	defer func() { getCallerIdentityFn = orig }()
	getCallerIdentityFn = func(_ context.Context, _ string) (tools.CallerIdentity, error) {
		return tools.CallerIdentity{}, errors.New("network timeout")
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

// AppKafka — kubectl error (exec fails entirely)
func TestAppKafka_Error_KubectlFails(t *testing.T) {
	origHelm := helmFn
	origKubectl := kubectlFn
	defer func() { helmFn = origHelm; kubectlFn = origKubectl }()

	helmFn = func(_ context.Context, _ *doctor.RunContext, _ ...string) ([]byte, error) {
		return []byte(`{"info":{"status":"deployed"}}`), nil
	}
	kubectlFn = func(_ context.Context, _ *doctor.RunContext, args ...string) ([]byte, error) {
		return nil, errors.New("CRD not found")
	}

	f := AppKafka{}.Run(context.Background(), deployedRC())
	if f.Status != doctor.StatusFail {
		t.Fatalf("expected fail when kubectl fails, got %v", f.Status)
	}
}

// AppObjectStorage — no errors in logs (happy path)
func TestAppObjectStorage_Pass(t *testing.T) {
	origHelm := helmFn
	origKubectl := kubectlFn
	defer func() { helmFn = origHelm; kubectlFn = origKubectl }()

	helmFn = func(_ context.Context, _ *doctor.RunContext, _ ...string) ([]byte, error) {
		return []byte(`{"info":{"status":"deployed"}}`), nil
	}
	kubectlFn = func(_ context.Context, _ *doctor.RunContext, args ...string) ([]byte, error) {
		return []byte("2024-01-01 INFO Uploaded 50 resources to S3 successfully."), nil
	}

	f := AppObjectStorage{}.Run(context.Background(), deployedRC())
	if f.Status != doctor.StatusPass {
		t.Fatalf("expected pass with clean logs, got %v: %s", f.Status, f.Message)
	}
}

// AWSQuotaEIP — pass
func TestAWSQuotaEIP_Pass(t *testing.T) {
	orig := getServiceQuotaFn
	defer func() { getServiceQuotaFn = orig }()
	getServiceQuotaFn = func(_ context.Context, _, _, _ string) (float64, error) {
		return 10.0, nil
	}

	f := AWSQuotaEIP.Run(context.Background(), callerRC())
	if f.Status != doctor.StatusPass {
		t.Fatalf("expected pass, got %v: %s", f.Status, f.Message)
	}
}

// MetaBucketLockTable — fail access denied
func TestMetaBucketLockTable_Fail_AccessDenied(t *testing.T) {
	orig := describeTableFn
	defer func() { describeTableFn = orig }()
	describeTableFn = func(_ context.Context, table, _ string) (string, error) {
		return "denied", errors.New("access denied to DynamoDB table " + table)
	}

	f := MetaBucketLockTable{}.Run(context.Background(), metaRC())
	if f.Status != doctor.StatusFail {
		t.Fatalf("expected fail on access denied, got %v", f.Status)
	}
	if f.Severity != doctor.SeverityCritical {
		t.Fatalf("expected critical, got %v", f.Severity)
	}
}
