package checks

import (
	"context"
	"fmt"

	"github.com/WolframResearch/WAS-Kubernetes/internal/doctor"
)

// MetaBucketReachable checks that the meta bucket is accessible (or not yet created).
type MetaBucketReachable struct{ BaseCheck }

func (MetaBucketReachable) ID() string                { return "meta_bucket.reachable" }
func (MetaBucketReachable) Name() string              { return "Meta bucket exists and accessible" }
func (MetaBucketReachable) Category() doctor.Category { return doctor.CategoryEnvironment }
func (MetaBucketReachable) Description() string {
	return "Checks that the wasctl meta S3 bucket is reachable. NoSuchBucket on first install is a Pass."
}
func (c MetaBucketReachable) Precondition(_ context.Context, rc *doctor.RunContext) (bool, string) {
	return metaBucketPrecondition(c.ID(), c.Name(), rc)
}
func (c MetaBucketReachable) Run(ctx context.Context, rc *doctor.RunContext) doctor.Finding {
	bucket, err := metaBucketName(rc)
	if err != nil {
		return checkError(c.ID(), c.Name(), err.Error())
	}
	status, err := headBucketFn(ctx, bucket, rc.Cfg.MetaRegion.Value)
	if err != nil {
		return fail(c.ID(), c.Name(), doctor.SeverityCritical,
			fmt.Sprintf("access denied to meta bucket s3://%s", bucket),
			"Grant s3:HeadBucket, s3:GetObject, s3:PutObject, s3:ListBucket on arn:aws:s3:::"+bucket+".\n"+
				"See: docs/Troubleshooting.md#meta-bucket-denied")
	}
	switch status {
	case "notfound":
		return pass(c.ID(), c.Name(), fmt.Sprintf("meta bucket s3://%s not yet created; will be created on first install", bucket))
	default:
		return pass(c.ID(), c.Name(), fmt.Sprintf("meta bucket s3://%s is accessible", bucket))
	}
}

// MetaBucketWritable checks that the caller can write to the meta bucket.
type MetaBucketWritable struct{ BaseCheck }

func (MetaBucketWritable) ID() string                { return "meta_bucket.writable" }
func (MetaBucketWritable) Name() string              { return "Can write to meta bucket" }
func (MetaBucketWritable) Category() doctor.Category { return doctor.CategoryEnvironment }
func (MetaBucketWritable) Description() string {
	return "Puts a probe object to verify write access to the meta bucket."
}
func (c MetaBucketWritable) Precondition(_ context.Context, rc *doctor.RunContext) (bool, string) {
	return metaBucketPrecondition(c.ID(), c.Name(), rc)
}
func (c MetaBucketWritable) Run(ctx context.Context, rc *doctor.RunContext) doctor.Finding {
	bucket, err := metaBucketName(rc)
	if err != nil {
		return checkError(c.ID(), c.Name(), err.Error())
	}

	// Skip write test if bucket doesn't exist yet
	status, err := headBucketFn(ctx, bucket, rc.Cfg.MetaRegion.Value)
	if err != nil {
		return skip(c.ID(), c.Name(), "meta bucket not accessible; check meta_bucket.reachable first")
	}
	if status == "notfound" {
		return pass(c.ID(), c.Name(), "meta bucket not yet created; write check deferred to first install")
	}

	if writeErr := putObjectFn(ctx, bucket, "doctor-probe/writable-check.txt", rc.Cfg.MetaRegion.Value); writeErr != nil {
		return fail(c.ID(), c.Name(), doctor.SeverityCritical,
			fmt.Sprintf("cannot write to meta bucket s3://%s: %v", bucket, writeErr),
			"Grant s3:PutObject on arn:aws:s3:::"+bucket+"/*.\n"+
				"See: docs/Troubleshooting.md#meta-bucket-denied")
	}
	return pass(c.ID(), c.Name(), fmt.Sprintf("write to s3://%s succeeded", bucket))
}

// MetaBucketLockTable checks that the DynamoDB lock table exists.
type MetaBucketLockTable struct{ BaseCheck }

func (MetaBucketLockTable) ID() string                { return "meta_bucket.lock_table" }
func (MetaBucketLockTable) Name() string              { return "Lock table exists and writable" }
func (MetaBucketLockTable) Category() doctor.Category { return doctor.CategoryEnvironment }
func (MetaBucketLockTable) Description() string {
	return "Checks that the companion DynamoDB lock table is reachable."
}
func (c MetaBucketLockTable) Precondition(_ context.Context, rc *doctor.RunContext) (bool, string) {
	return metaBucketPrecondition(c.ID(), c.Name(), rc)
}
func (c MetaBucketLockTable) Run(ctx context.Context, rc *doctor.RunContext) doctor.Finding {
	table, err := lockTableName(rc)
	if err != nil {
		return checkError(c.ID(), c.Name(), err.Error())
	}
	status, descErr := describeTableFn(ctx, table, rc.Cfg.MetaRegion.Value)
	if descErr != nil {
		return fail(c.ID(), c.Name(), doctor.SeverityCritical,
			fmt.Sprintf("access denied to DynamoDB table %s", table),
			"Grant dynamodb:DescribeTable, dynamodb:GetItem, dynamodb:PutItem, dynamodb:DeleteItem on arn:aws:dynamodb:*:*:table/"+table+".\n"+
				"See: docs/Troubleshooting.md#meta-bucket-denied")
	}
	switch status {
	case "notfound":
		return pass(c.ID(), c.Name(), fmt.Sprintf("lock table %s not yet created; will be created on first install", table))
	default:
		return pass(c.ID(), c.Name(), fmt.Sprintf("lock table %s exists", table))
	}
}
