// Package metabucket manages the per-AWS-account wasctl meta bucket and its
// companion DynamoDB lock table. All durable customer state (bootstrap
// terraform.tfstate, backend.hcl, workspace.json) lives here so wasctl has no
// local disk state and any machine with the right AWS credentials can continue
// where another left off.
//
// AWS SDK v2 is used only here and in the lock/metadata files. Everything else
// still shells out to the AWS CLI (respects customer's profile/SSO session).
package metabucket

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// BucketName returns the canonical meta bucket name for a cluster and AWS account.
func BucketName(clusterName, accountID string) string {
	if clusterName == "" {
		clusterName = "wasctl"
	}
	return "wolfram-" + clusterName + "-meta-" + accountID
}

// Bucket is a handle to the per-account meta S3 bucket.
type Bucket struct {
	client *s3.Client
	name   string
	region string
}

// Open returns a Bucket handle for the meta bucket. It does NOT create the
// bucket; call EnsureExists for first-run setup. Use Open when you know the
// bucket already exists (e.g., inside a workspace operation that follows
// preflight).
func Open(ctx context.Context, region, accountID, clusterName string) (*Bucket, error) {
	client, err := newS3Client(ctx, region)
	if err != nil {
		return nil, err
	}
	return &Bucket{client: client, name: BucketName(clusterName, accountID), region: region}, nil
}

// EnsureExists creates the meta bucket (plus versioning, encryption,
// public-access-block, ownership controls, and lifecycle policy) if it does
// not already exist.
//
// Three outcomes:
//   - Bucket already exists and is accessible → returns (*Bucket, nil)
//   - NoSuchBucket → creates it and returns (*Bucket, nil)
//   - AccessDenied → returns a descriptive error with required IAM permissions
func EnsureExists(ctx context.Context, region, accountID, clusterName string) (*Bucket, error) {
	client, err := newS3Client(ctx, region)
	if err != nil {
		return nil, err
	}
	name := BucketName(clusterName, accountID)
	b := &Bucket{client: client, name: name, region: region}

	_, headErr := client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(name)})
	if headErr == nil {
		return b, nil // already exists
	}

	var noKey *s3types.NoSuchKey
	var notFound *s3types.NotFound
	if errors.As(headErr, &noKey) || errors.As(headErr, &notFound) || isNotFound(headErr) {
		if createErr := b.create(ctx); createErr != nil {
			return nil, createErr
		}
		return b, nil
	}

	// Access denied or other error.
	return nil, fmt.Errorf(
		"cannot access meta bucket %s: %w\n"+
			"Required IAM permissions:\n"+
			"  s3:CreateBucket, s3:PutBucketEncryption, s3:PutBucketVersioning,\n"+
			"  s3:PutPublicAccessBlock, s3:PutBucketOwnershipControls,\n"+
			"  s3:PutLifecycleConfiguration, s3:PutBucketPolicy,\n"+
			"  s3:GetObject, s3:PutObject, s3:ListBucket\n"+
			"  on arn:aws:s3:::%s and arn:aws:s3:::%s/*",
		name, headErr, name, name,
	)
}

// CheckAccessible performs the preflight s3:ListBucket HEAD check.
// Returns nil if accessible or doesn't-exist-yet. Returns a descriptive
// error if AccessDenied.
func CheckAccessible(ctx context.Context, region, accountID, clusterName string) error {
	client, err := newS3Client(ctx, region)
	if err != nil {
		return err
	}
	name := BucketName(clusterName, accountID)
	_, headErr := client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(name)})
	if headErr == nil || isNotFound(headErr) {
		return nil
	}
	return fmt.Errorf(
		"your IAM identity can authenticate but cannot access S3.\n"+
			"Required permissions: s3:CreateBucket, s3:PutBucketEncryption,\n"+
			"s3:PutBucketVersioning, s3:PutPublicAccessBlock, s3:PutBucketPolicy,\n"+
			"s3:GetObject, s3:PutObject, s3:ListBucket\n"+
			"on %s",
		name,
	)
}

// Name returns the S3 bucket name.
func (b *Bucket) Name() string { return b.name }

// Get downloads the object at key and returns its contents.
// Returns ErrNotFound if the key doesn't exist.
func (b *Bucket) Get(ctx context.Context, key string) ([]byte, error) {
	out, err := b.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(b.name),
		Key:    aws.String(key),
	})
	if err != nil {
		if isNotFound(err) {
			return nil, &ErrNotFound{Key: key}
		}
		return nil, fmt.Errorf("s3 get %s: %w", key, err)
	}
	defer out.Body.Close()
	data, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, fmt.Errorf("s3 read %s: %w", key, err)
	}
	return data, nil
}

// Put uploads data to key. Skips the upload when the existing object's
// SHA-256 already matches (idempotent).
func (b *Bucket) Put(ctx context.Context, key string, data []byte) error {
	// Check if existing content matches to avoid a write.
	existing, err := b.Get(ctx, key)
	if err == nil && sha256sum(existing) == sha256sum(data) {
		return nil // identical, no-op
	}

	_, err = b.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(b.name),
		Key:    aws.String(key),
		Body:   bytes.NewReader(data),
	})
	if err != nil {
		return fmt.Errorf("s3 put %s: %w", key, err)
	}
	return nil
}

// Delete removes an object from the bucket. If the key does not exist, Delete
// returns nil (idempotent). Used by workspace delete to clean up cluster keys.
func (b *Bucket) Delete(ctx context.Context, key string) error {
	_, err := b.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(b.name),
		Key:    aws.String(key),
	})
	if err != nil && isNotFound(err) {
		return nil
	}
	return wrapErr(err, "s3 delete "+key)
}

// Exists returns true if the key is present in the bucket.
func (b *Bucket) Exists(ctx context.Context, key string) (bool, error) {
	_, err := b.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(b.name),
		Key:    aws.String(key),
	})
	if err == nil {
		return true, nil
	}
	if isNotFound(err) {
		return false, nil
	}
	return false, fmt.Errorf("s3 head %s: %w", key, err)
}

// ListClusters returns cluster names by listing S3 buckets that match the pattern
// wolfram-<cluster-name>-meta-<accountID>.
func (b *Bucket) ListClusters(ctx context.Context) ([]string, error) {
	out, err := b.client.ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil {
		return nil, fmt.Errorf("s3 list buckets: %w", err)
	}
	var names []string
	suffix := ""
	lastDash := strings.LastIndex(b.name, "-")
	if lastDash != -1 {
		suffix = b.name[lastDash:] // e.g. "-123456789012"
	}
	if suffix == "" {
		return nil, fmt.Errorf("invalid metabucket name format: %s", b.name)
	}

	for _, bucket := range out.Buckets {
		if bucket.Name == nil {
			continue
		}
		name := *bucket.Name
		if strings.HasPrefix(name, "wolfram-") && strings.HasSuffix(name, "-meta"+suffix) {
			prefixLen := len("wolfram-")
			suffixLen := len("-meta") + len(suffix)
			if len(name) > prefixLen+suffixLen {
				clusterName := name[prefixLen : len(name)-suffixLen]
				// Avoid returning "wasctl" fallback placeholder as a cluster name
				if clusterName != "wasctl" {
					names = append(names, clusterName)
				}
			}
		}
	}
	return names, nil
}

// create configures a new meta S3 bucket with required settings.
func (b *Bucket) create(ctx context.Context) error {
	input := &s3.CreateBucketInput{Bucket: aws.String(b.name)}
	// us-east-1 is the default region and must NOT include LocationConstraint.
	if b.region != "us-east-1" {
		input.CreateBucketConfiguration = &s3types.CreateBucketConfiguration{
			LocationConstraint: s3types.BucketLocationConstraint(b.region),
		}
	}
	if _, err := b.client.CreateBucket(ctx, input); err != nil {
		return fmt.Errorf("create bucket %s: %w", b.name, err)
	}

	if err := b.applyEncryption(ctx); err != nil {
		return err
	}
	if err := b.applyVersioning(ctx); err != nil {
		return err
	}
	if err := b.applyPublicAccessBlock(ctx); err != nil {
		return err
	}
	if err := b.applyOwnershipControls(ctx); err != nil {
		return err
	}
	if err := b.applyLifecycle(ctx); err != nil {
		return err
	}
	return nil
}

func (b *Bucket) applyEncryption(ctx context.Context) error {
	_, err := b.client.PutBucketEncryption(ctx, &s3.PutBucketEncryptionInput{
		Bucket: aws.String(b.name),
		ServerSideEncryptionConfiguration: &s3types.ServerSideEncryptionConfiguration{
			Rules: []s3types.ServerSideEncryptionRule{{
				ApplyServerSideEncryptionByDefault: &s3types.ServerSideEncryptionByDefault{
					SSEAlgorithm: s3types.ServerSideEncryptionAes256,
				},
			}},
		},
	})
	return wrapErr(err, "set bucket encryption")
}

func (b *Bucket) applyVersioning(ctx context.Context) error {
	_, err := b.client.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{
		Bucket: aws.String(b.name),
		VersioningConfiguration: &s3types.VersioningConfiguration{
			Status: s3types.BucketVersioningStatusEnabled,
		},
	})
	return wrapErr(err, "enable bucket versioning")
}

func (b *Bucket) applyPublicAccessBlock(ctx context.Context) error {
	t := true
	_, err := b.client.PutPublicAccessBlock(ctx, &s3.PutPublicAccessBlockInput{
		Bucket: aws.String(b.name),
		PublicAccessBlockConfiguration: &s3types.PublicAccessBlockConfiguration{
			BlockPublicAcls:       &t,
			IgnorePublicAcls:      &t,
			BlockPublicPolicy:     &t,
			RestrictPublicBuckets: &t,
		},
	})
	return wrapErr(err, "set public access block")
}

func (b *Bucket) applyOwnershipControls(ctx context.Context) error {
	_, err := b.client.PutBucketOwnershipControls(ctx, &s3.PutBucketOwnershipControlsInput{
		Bucket: aws.String(b.name),
		OwnershipControls: &s3types.OwnershipControls{
			Rules: []s3types.OwnershipControlsRule{{
				ObjectOwnership: s3types.ObjectOwnershipBucketOwnerEnforced,
			}},
		},
	})
	return wrapErr(err, "set ownership controls")
}

func (b *Bucket) applyLifecycle(ctx context.Context) error {
	days90 := int32(90)
	days7 := int32(7)
	_, err := b.client.PutBucketLifecycleConfiguration(ctx, &s3.PutBucketLifecycleConfigurationInput{
		Bucket: aws.String(b.name),
		LifecycleConfiguration: &s3types.BucketLifecycleConfiguration{
			Rules: []s3types.LifecycleRule{
				{
					ID:     aws.String("expire-old-versions"),
					Status: s3types.ExpirationStatusEnabled,
					Filter: &s3types.LifecycleRuleFilter{Prefix: aws.String("")},
					NoncurrentVersionExpiration: &s3types.NoncurrentVersionExpiration{
						NoncurrentDays: &days90,
					},
				},
				{
					ID:     aws.String("abort-multipart"),
					Status: s3types.ExpirationStatusEnabled,
					Filter: &s3types.LifecycleRuleFilter{Prefix: aws.String("")},
					AbortIncompleteMultipartUpload: &s3types.AbortIncompleteMultipartUpload{
						DaysAfterInitiation: &days7,
					},
				},
			},
		},
	})
	return wrapErr(err, "set lifecycle policy")
}

// newS3Client builds an S3 client for the given region using the customer's
// default AWS credentials (profile, env vars, IMDS — same as CLI).
func newS3Client(ctx context.Context, region string) (*s3.Client, error) {
	cfg, err := awscfg.LoadDefaultConfig(ctx, awscfg.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}
	return s3.NewFromConfig(cfg), nil
}

// isNotFound returns true for HTTP 404 errors from S3.
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	var nf *s3types.NotFound
	var nk *s3types.NoSuchKey
	var nb *s3types.NoSuchBucket
	return errors.As(err, &nf) || errors.As(err, &nk) || errors.As(err, &nb)
}

func sha256sum(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h)
}

func wrapErr(err error, msg string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", msg, err)
}

// ErrNotFound is returned by Get when the S3 key does not exist.
type ErrNotFound struct{ Key string }

func (e *ErrNotFound) Error() string { return fmt.Sprintf("s3 key not found: %s", e.Key) }

// ClusterKeyPrefix returns the S3 key prefix for a cluster's workspace.
func ClusterKeyPrefix(clusterName string) string {
	return "clusters/" + clusterName + "/"
}

// BootstrapStateKey returns the S3 key for the bootstrap terraform.tfstate.
func BootstrapStateKey(clusterName string) string {
	return ClusterKeyPrefix(clusterName) + "bootstrap-state/terraform.tfstate"
}

// BootstrapStateBackupKey returns the S3 key for the bootstrap state backup.
func BootstrapStateBackupKey(clusterName string) string {
	return ClusterKeyPrefix(clusterName) + "bootstrap-state/terraform.tfstate.backup"
}

// BackendHCLKey returns the S3 key for the stack backend.hcl.
func BackendHCLKey(clusterName string) string {
	return ClusterKeyPrefix(clusterName) + "backend.hcl"
}

// WorkspaceMetaKey returns the S3 key for workspace.json.
func WorkspaceMetaKey(clusterName string) string {
	return ClusterKeyPrefix(clusterName) + "workspace.json"
}

// MetaIndexKey returns the S3 key for the top-level meta.json.
const MetaIndexKey = "meta.json"

// noncurrentDaysVal is a workaround for the int32 pointer requirement.
func noncurrentDaysVal(n int32) *int32 {
	_ = time.Now() // suppress unused import in non-test builds
	return &n
}

// Destroy deletes the meta bucket. It first empties all object versions and delete markers,
// then calls DeleteBucket.
func (b *Bucket) Destroy(ctx context.Context) error {
	var nextKeyMarker *string
	var nextVersionIdMarker *string

	for {
		out, err := b.client.ListObjectVersions(ctx, &s3.ListObjectVersionsInput{
			Bucket:          aws.String(b.name),
			KeyMarker:       nextKeyMarker,
			VersionIdMarker: nextVersionIdMarker,
		})
		if err != nil {
			if isNotFound(err) {
				return nil // already deleted
			}
			return fmt.Errorf("list object versions: %w", err)
		}

		var objects []s3types.ObjectIdentifier
		for _, v := range out.Versions {
			objects = append(objects, s3types.ObjectIdentifier{
				Key:       v.Key,
				VersionId: v.VersionId,
			})
		}
		for _, dm := range out.DeleteMarkers {
			objects = append(objects, s3types.ObjectIdentifier{
				Key:       dm.Key,
				VersionId: dm.VersionId,
			})
		}

		if len(objects) > 0 {
			_, err = b.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
				Bucket: aws.String(b.name),
				Delete: &s3types.Delete{Objects: objects, Quiet: aws.Bool(true)},
			})
			if err != nil {
				return fmt.Errorf("delete versions/delete markers: %w", err)
			}
		}

		if !aws.ToBool(out.IsTruncated) {
			break
		}
		nextKeyMarker = out.NextKeyMarker
		nextVersionIdMarker = out.NextVersionIdMarker
	}

	_, err := b.client.DeleteBucket(ctx, &s3.DeleteBucketInput{
		Bucket: aws.String(b.name),
	})
	if err != nil && !isNotFound(err) {
		return fmt.Errorf("delete S3 bucket %s: %w", b.name, err)
	}
	return nil
}
