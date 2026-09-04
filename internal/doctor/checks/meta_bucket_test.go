package checks

import (
	"context"
	"errors"
	"testing"

	"github.com/WolframResearch/WAS-Kubernetes/internal/config"
	"github.com/WolframResearch/WAS-Kubernetes/internal/doctor"
	"github.com/WolframResearch/WAS-Kubernetes/internal/tools"
)

func metaRC() *doctor.RunContext {
	id := tools.CallerIdentity{Account: "123456789012"}
	rc := &doctor.RunContext{Cfg: testCfg()}
	rc.CallerID = &id
	return rc
}

func TestMetaBucketReachable_Pass_Exists(t *testing.T) {
	orig := headBucketFn
	defer func() { headBucketFn = orig }()
	headBucketFn = func(_ context.Context, _, _ string) (string, error) { return "exists", nil }

	f := MetaBucketReachable{}.Run(context.Background(), metaRC())
	if f.Status != doctor.StatusPass {
		t.Fatalf("expected pass, got %v: %s", f.Status, f.Message)
	}
}

func TestMetaBucketReachable_Pass_NotFound(t *testing.T) {
	orig := headBucketFn
	defer func() { headBucketFn = orig }()
	headBucketFn = func(_ context.Context, _, _ string) (string, error) { return "notfound", nil }

	f := MetaBucketReachable{}.Run(context.Background(), metaRC())
	if f.Status != doctor.StatusPass {
		t.Fatalf("expected pass on not-found (first install), got %v: %s", f.Status, f.Message)
	}
}

func TestMetaBucketReachable_Fail_AccessDenied(t *testing.T) {
	orig := headBucketFn
	defer func() { headBucketFn = orig }()
	headBucketFn = func(_ context.Context, bucket, _ string) (string, error) {
		return "denied", errors.New("access denied to s3://" + bucket)
	}

	f := MetaBucketReachable{}.Run(context.Background(), metaRC())
	if f.Status != doctor.StatusFail {
		t.Fatalf("expected fail on access denied, got %v", f.Status)
	}
	if f.Severity != doctor.SeverityCritical {
		t.Fatalf("expected critical, got %v", f.Severity)
	}
}

func TestMetaBucketReachable_Skip_ChartOnly(t *testing.T) {
	rc := &doctor.RunContext{Cfg: &config.Config{
		ChartOnly:   true,
		MetaRegion:  config.Field[string]{Value: "us-east-1"},
		ClusterName: config.Field[string]{Value: "test"},
		Region:      config.Field[string]{Value: "us-east-1"},
	}}
	skip, reason := MetaBucketReachable{}.Precondition(context.Background(), rc)
	if !skip {
		t.Fatal("expected skip in chart-only mode")
	}
	if reason == "" {
		t.Fatal("expected non-empty skip reason")
	}
}

func TestMetaBucketLockTable_Pass_Exists(t *testing.T) {
	orig := describeTableFn
	defer func() { describeTableFn = orig }()
	describeTableFn = func(_ context.Context, _, _ string) (string, error) { return "exists", nil }

	f := MetaBucketLockTable{}.Run(context.Background(), metaRC())
	if f.Status != doctor.StatusPass {
		t.Fatalf("expected pass, got %v: %s", f.Status, f.Message)
	}
}

func TestMetaBucketLockTable_Pass_NotFound(t *testing.T) {
	orig := describeTableFn
	defer func() { describeTableFn = orig }()
	describeTableFn = func(_ context.Context, _, _ string) (string, error) { return "notfound", nil }

	f := MetaBucketLockTable{}.Run(context.Background(), metaRC())
	if f.Status != doctor.StatusPass {
		t.Fatalf("expected pass on not-found, got %v: %s", f.Status, f.Message)
	}
}
