package checks

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/WolframResearch/WAS-Kubernetes/internal/doctor"
)

func azureContainerRC() *doctor.RunContext {
	rc := azureRC()
	rc.Cfg.ClusterName.Value = "test-cluster"
	return rc
}

// ── MetaContainerReachable ───────────────────────────────────────────────────

func TestMetaContainerReachable_Pass_Exists(t *testing.T) {
	orig := checkBlobContainerFn
	defer func() { checkBlobContainerFn = orig }()
	checkBlobContainerFn = func(_ context.Context, _, _ string) (string, error) {
		return "exists", nil
	}

	f := MetaContainerReachable{}.Run(context.Background(), azureContainerRC())
	if f.Status != doctor.StatusPass {
		t.Fatalf("expected pass, got %v: %s", f.Status, f.Message)
	}
}

func TestMetaContainerReachable_Pass_NotFound(t *testing.T) {
	orig := checkBlobContainerFn
	defer func() { checkBlobContainerFn = orig }()
	checkBlobContainerFn = func(_ context.Context, _, _ string) (string, error) {
		return "notfound", nil
	}

	f := MetaContainerReachable{}.Run(context.Background(), azureContainerRC())
	if f.Status != doctor.StatusPass {
		t.Fatalf("expected pass for not-yet-created account, got %v: %s", f.Status, f.Message)
	}
}

func TestMetaContainerReachable_Fail_Denied(t *testing.T) {
	orig := checkBlobContainerFn
	defer func() { checkBlobContainerFn = orig }()
	checkBlobContainerFn = func(_ context.Context, _, _ string) (string, error) {
		return "denied", fmt.Errorf("access denied to meta storage account")
	}

	f := MetaContainerReachable{}.Run(context.Background(), azureContainerRC())
	if f.Status != doctor.StatusFail {
		t.Fatalf("expected fail on access denied, got %v", f.Status)
	}
	if f.Severity != doctor.SeverityCritical {
		t.Fatalf("expected critical, got %v", f.Severity)
	}
}

func TestMetaContainerReachable_Skip_NotAzure(t *testing.T) {
	rc := &doctor.RunContext{Cfg: testCfg()}
	skip, _ := MetaContainerReachable{}.Precondition(context.Background(), rc)
	if !skip {
		t.Fatal("expected skip for non-Azure cloud")
	}
}

// ── MetaContainerWritable ───────────────────────────────────────────────────

func TestMetaContainerWritable_Pass(t *testing.T) {
	origCheck := checkBlobContainerFn
	origPut := putBlobFn
	defer func() { checkBlobContainerFn = origCheck; putBlobFn = origPut }()

	checkBlobContainerFn = func(_ context.Context, _, _ string) (string, error) {
		return "exists", nil
	}
	putBlobFn = func(_ context.Context, _, _, _ string, _ []byte) error {
		return nil
	}

	f := MetaContainerWritable{}.Run(context.Background(), azureContainerRC())
	if f.Status != doctor.StatusPass {
		t.Fatalf("expected pass, got %v: %s", f.Status, f.Message)
	}
}

func TestMetaContainerWritable_Pass_NotFound(t *testing.T) {
	orig := checkBlobContainerFn
	defer func() { checkBlobContainerFn = orig }()
	checkBlobContainerFn = func(_ context.Context, _, _ string) (string, error) {
		return "notfound", nil
	}

	f := MetaContainerWritable{}.Run(context.Background(), azureContainerRC())
	if f.Status != doctor.StatusPass {
		t.Fatalf("expected pass for not-yet-created account, got %v: %s", f.Status, f.Message)
	}
}

func TestMetaContainerWritable_Fail_WriteError(t *testing.T) {
	origCheck := checkBlobContainerFn
	origPut := putBlobFn
	defer func() { checkBlobContainerFn = origCheck; putBlobFn = origPut }()

	checkBlobContainerFn = func(_ context.Context, _, _ string) (string, error) {
		return "exists", nil
	}
	putBlobFn = func(_ context.Context, _, _, _ string, _ []byte) error {
		return errors.New("write denied")
	}

	f := MetaContainerWritable{}.Run(context.Background(), azureContainerRC())
	if f.Status != doctor.StatusFail {
		t.Fatalf("expected fail on write error, got %v", f.Status)
	}
}

func TestMetaContainerWritable_Skip_NotReachable(t *testing.T) {
	orig := checkBlobContainerFn
	defer func() { checkBlobContainerFn = orig }()
	checkBlobContainerFn = func(_ context.Context, _, _ string) (string, error) {
		return "denied", fmt.Errorf("denied")
	}

	f := MetaContainerWritable{}.Run(context.Background(), azureContainerRC())
	if f.Status != doctor.StatusSkip {
		t.Fatalf("expected skip when container inaccessible, got %v", f.Status)
	}
}

// ── MetaContainerLockBlob ───────────────────────────────────────────────────

func TestMetaContainerLockBlob_Pass_Exists(t *testing.T) {
	origCheck := checkBlobContainerFn
	origLock := checkLockBlobFn
	defer func() { checkBlobContainerFn = origCheck; checkLockBlobFn = origLock }()

	checkBlobContainerFn = func(_ context.Context, _, _ string) (string, error) {
		return "exists", nil
	}
	checkLockBlobFn = func(_ context.Context, _, _ string) error {
		return nil
	}

	f := MetaContainerLockBlob{}.Run(context.Background(), azureContainerRC())
	if f.Status != doctor.StatusPass {
		t.Fatalf("expected pass, got %v: %s", f.Status, f.Message)
	}
}

func TestMetaContainerLockBlob_Pass_NotFound(t *testing.T) {
	orig := checkBlobContainerFn
	defer func() { checkBlobContainerFn = orig }()
	checkBlobContainerFn = func(_ context.Context, _, _ string) (string, error) {
		return "notfound", nil
	}

	f := MetaContainerLockBlob{}.Run(context.Background(), azureContainerRC())
	if f.Status != doctor.StatusPass {
		t.Fatalf("expected pass for not-yet-created account, got %v", f.Status)
	}
}

func TestMetaContainerLockBlob_Fail_WriteError(t *testing.T) {
	origCheck := checkBlobContainerFn
	origLock := checkLockBlobFn
	defer func() { checkBlobContainerFn = origCheck; checkLockBlobFn = origLock }()

	checkBlobContainerFn = func(_ context.Context, _, _ string) (string, error) {
		return "exists", nil
	}
	checkLockBlobFn = func(_ context.Context, _, _ string) error {
		return errors.New("write denied")
	}

	f := MetaContainerLockBlob{}.Run(context.Background(), azureContainerRC())
	if f.Status != doctor.StatusFail {
		t.Fatalf("expected fail on write error, got %v", f.Status)
	}
}

func TestMetaContainerLockBlob_Skip_NoCluster(t *testing.T) {
	rc := azureRC()
	rc.Cfg.ClusterName.Value = ""
	skip, _ := MetaContainerLockBlob{}.Precondition(context.Background(), rc)
	if !skip {
		t.Fatal("expected skip when no cluster name")
	}
}

func TestMetaContainerPrecondition_ChartOnly(t *testing.T) {
	rc := azureRC()
	rc.Cfg.ChartOnly = true
	skip, _ := MetaContainerReachable{}.Precondition(context.Background(), rc)
	if !skip {
		t.Fatal("expected skip in chart-only mode")
	}
}
