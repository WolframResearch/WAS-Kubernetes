package stages

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/WolframResearch/WAS-Kubernetes/internal/runner"
)

// emptyS3Bucket deletes all object versions and delete markers in the S3 bucket.
func emptyS3Bucket(ctx context.Context, region, bucketName string, rep runner.Reporter) error {
	cfg, err := awscfg.LoadDefaultConfig(ctx, awscfg.WithRegion(region))
	if err != nil {
		return fmt.Errorf("load AWS config: %w", err)
	}
	client := s3.NewFromConfig(cfg)

	rep.LogLine(fmt.Sprintf("[cleanup] emptying S3 bucket %s in region %s...", bucketName, region))

	// Check if bucket exists first
	_, err = client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(bucketName),
	})
	if err != nil {
		var nf *s3types.NotFound
		if errors.As(err, &nf) || strings.Contains(err.Error(), "NotFound") {
			rep.LogLine(fmt.Sprintf("[cleanup] bucket %s already deleted or does not exist", bucketName))
			return nil
		}
		// If access denied or other error, log but don't fail destroy stage completely
		rep.LogLine(fmt.Sprintf("[cleanup] warning: check bucket %s: %v", bucketName, err))
		return nil
	}

	paginator := s3.NewListObjectVersionsPaginator(client, &s3.ListObjectVersionsInput{
		Bucket: aws.String(bucketName),
	})

	deletedCount := 0
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			if strings.Contains(err.Error(), "NoSuchBucket") {
				return nil
			}
			return fmt.Errorf("list object versions: %w", err)
		}

		var objectsToDelete []s3types.ObjectIdentifier
		for _, v := range page.Versions {
			objectsToDelete = append(objectsToDelete, s3types.ObjectIdentifier{
				Key:       v.Key,
				VersionId: v.VersionId,
			})
		}
		for _, dm := range page.DeleteMarkers {
			objectsToDelete = append(objectsToDelete, s3types.ObjectIdentifier{
				Key:       dm.Key,
				VersionId: dm.VersionId,
			})
		}

		if len(objectsToDelete) > 0 {
			_, err = client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
				Bucket: aws.String(bucketName),
				Delete: &s3types.Delete{Objects: objectsToDelete},
			})
			if err != nil {
				return fmt.Errorf("delete objects: %w", err)
			}
			deletedCount += len(objectsToDelete)
		}
	}
	if deletedCount > 0 {
		rep.LogLine(fmt.Sprintf("[cleanup] deleted %d object version(s)/delete marker(s) from %s", deletedCount, bucketName))
	} else {
		rep.LogLine(fmt.Sprintf("[cleanup] bucket %s is already empty", bucketName))
	}
	return nil
}
