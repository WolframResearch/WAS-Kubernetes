package checks

import (
	"context"
	"errors"
	"testing"

	"github.com/WolframResearch/WAS-Kubernetes/internal/doctor"
)

func TestMetaBucketWritable_Pass(t *testing.T) {
	origHead := headBucketFn
	origPut := putObjectFn
	defer func() { headBucketFn = origHead; putObjectFn = origPut }()

	headBucketFn = func(_ context.Context, _, _ string) (string, error) { return "exists", nil }
	putObjectFn = func(_ context.Context, _, _, _ string) error { return nil }

	f := MetaBucketWritable{}.Run(context.Background(), metaRC())
	if f.Status != doctor.StatusPass {
		t.Fatalf("expected pass, got %v: %s", f.Status, f.Message)
	}
}

func TestMetaBucketWritable_Pass_NotCreatedYet(t *testing.T) {
	origHead := headBucketFn
	defer func() { headBucketFn = origHead }()
	headBucketFn = func(_ context.Context, _, _ string) (string, error) { return "notfound", nil }

	f := MetaBucketWritable{}.Run(context.Background(), metaRC())
	// Not yet created → Pass (write check deferred)
	if f.Status != doctor.StatusPass {
		t.Fatalf("expected pass for not-yet-created bucket, got %v: %s", f.Status, f.Message)
	}
}

func TestMetaBucketWritable_Fail_WriteDenied(t *testing.T) {
	origHead := headBucketFn
	origPut := putObjectFn
	defer func() { headBucketFn = origHead; putObjectFn = origPut }()

	headBucketFn = func(_ context.Context, _, _ string) (string, error) { return "exists", nil }
	putObjectFn = func(_ context.Context, _, _, _ string) error {
		return errors.New("access denied")
	}

	f := MetaBucketWritable{}.Run(context.Background(), metaRC())
	if f.Status != doctor.StatusFail {
		t.Fatalf("expected fail on write denied, got %v", f.Status)
	}
}

func TestMetaBucketWritable_Skip_HeadFails(t *testing.T) {
	origHead := headBucketFn
	defer func() { headBucketFn = origHead }()
	headBucketFn = func(_ context.Context, bucket, _ string) (string, error) {
		return "denied", errors.New("access denied to s3://" + bucket)
	}

	f := MetaBucketWritable{}.Run(context.Background(), metaRC())
	if f.Status != doctor.StatusSkip {
		t.Fatalf("expected skip when head fails, got %v", f.Status)
	}
}
