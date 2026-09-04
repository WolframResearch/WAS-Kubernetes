package checks

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/WolframResearch/WAS-Kubernetes/internal/doctor"
	"github.com/WolframResearch/WAS-Kubernetes/internal/metabucket"
)

// metaBucketName returns the canonical meta bucket name for the account.
func metaBucketName(rc *doctor.RunContext) (string, error) {
	if rc.CallerID == nil {
		return "", fmt.Errorf("caller identity unavailable")
	}
	return metabucket.BucketName(rc.Cfg.ClusterName.Value, rc.CallerID.Account), nil
}

// lockTableName returns the lock table name for the account.
func lockTableName(rc *doctor.RunContext) (string, error) {
	if rc.CallerID == nil {
		return "", fmt.Errorf("caller identity unavailable")
	}
	return metabucket.LockTableName(rc.Cfg.ClusterName.Value, rc.CallerID.Account), nil
}

// metaBucketPrecondition is the shared precondition for all meta_bucket checks.
func metaBucketPrecondition(id, name string, rc *doctor.RunContext) (bool, string) {
	if skip, reason := awsOnlyPrecondition(rc); skip {
		return true, reason
	}
	if rc.Cfg.ChartOnly {
		return true, "chart-only mode; no wasctl-managed infrastructure"
	}
	if rc.CallerID == nil {
		return true, "AWS credentials unavailable; check aws.credentials first"
	}
	return false, ""
}

// headBucketFn shells out to check whether the meta bucket exists.
// Returns ("exists", nil), ("notfound", nil), ("denied", err), or ("", err).
// Overridable for tests.
var headBucketFn = func(ctx context.Context, bucket, region string) (string, error) {
	out, err := runOutput(ctx, "aws", "s3api", "head-bucket",
		"--bucket", bucket,
		"--region", region)
	_ = out
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "NoSuchBucket") || strings.Contains(msg, "404") {
			return "notfound", nil
		}
		if strings.Contains(msg, "AccessDenied") || strings.Contains(msg, "403") {
			return "denied", fmt.Errorf("access denied to s3://%s", bucket)
		}
		return "", err
	}
	return "exists", nil
}

// putObjectFn shells out to write a probe object, used by meta_bucket.writable.
// Overridable for tests.
var putObjectFn = func(ctx context.Context, bucket, key, region string) error {
	tmpFile, err := os.CreateTemp("", "wasctl-probe-*.txt")
	if err != nil {
		return fmt.Errorf("create temporary probe file: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	_ = tmpFile.Close()

	_, err = runOutput(ctx, "aws", "s3api", "put-object",
		"--bucket", bucket,
		"--key", key,
		"--body", tmpFile.Name(),
		"--region", region)
	if err != nil {
		return fmt.Errorf("put probe object: %w", err)
	}
	_, _ = runOutput(ctx, "aws", "s3api", "delete-object",
		"--bucket", bucket, "--key", key, "--region", region)
	return nil
}

// describeTableFn shells out to check whether the DynamoDB lock table exists.
// Overridable for tests.
var describeTableFn = func(ctx context.Context, table, region string) (string, error) {
	_, err := runOutput(ctx, "aws", "dynamodb", "describe-table",
		"--table-name", table,
		"--region", region)
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "ResourceNotFoundException") {
			return "notfound", nil
		}
		if strings.Contains(msg, "AccessDenied") || strings.Contains(msg, "is not authorized") {
			return "denied", fmt.Errorf("access denied to DynamoDB table %s", table)
		}
		return "", err
	}
	return "exists", nil
}
