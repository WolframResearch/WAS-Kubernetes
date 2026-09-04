package collectors

import (
	"context"
	"fmt"
	"strings"
)

// AWSCollector collects AWS account and EKS cluster metadata.
type AWSCollector struct{}

func (AWSCollector) Name() string { return "aws" }

func (AWSCollector) Collect(ctx context.Context, cc *CollectContext) ([]File, error) {
	if cc.Cfg == nil {
		return nil, fmt.Errorf("no config available")
	}

	region := cc.Cfg.Region.Value
	if region == "" {
		region = cc.Cfg.MetaRegion.Value
	}

	var files []File

	// Caller identity.
	callerID, err := runOutput(ctx, "aws", "sts", "get-caller-identity", "--output", "json", "--region", region)
	if err != nil {
		callerID = []byte(fmt.Sprintf(`{"error": %q}`, err.Error()))
	}
	files = append(files, File{Path: "aws/caller_identity.json", Content: []byte(redactText(string(callerID)))})

	// EKS cluster describe.
	clusterName := cc.Cfg.ClusterName.Value
	eksCluster, err := runOutput(ctx, "aws", "eks", "describe-cluster",
		"--name", clusterName, "--region", region, "--output", "json")
	if err != nil {
		eksCluster = []byte(fmt.Sprintf(`{"error": %q}`, err.Error()))
	}
	files = append(files, File{Path: "aws/eks_cluster.json", Content: []byte(redactText(string(eksCluster)))})

	// Meta bucket status.
	if cc.Cfg.StateBucket.Value != "" {
		bucketStatus, err := runOutput(ctx, "aws", "s3api", "head-bucket",
			"--bucket", cc.Cfg.StateBucket.Value, "--region", region)
		if err != nil {
			bucketStatus = []byte(err.Error())
		}
		files = append(files, File{Path: "aws/meta_bucket_status.json",
			Content: []byte(strings.TrimSpace(string(bucketStatus)))})
	}

	// CloudTrail recent events (optional).
	if !cc.NoCloudTrail {
		trail, err := runOutput(ctx, "aws", "cloudtrail", "lookup-events",
			"--lookup-attributes", "AttributeKey=Username,AttributeValue=wasctl",
			"--start-time", "24hours",
			"--region", region, "--output", "json")
		if err != nil {
			trail = []byte(fmt.Sprintf(`{"error": %q}`, err.Error()))
		}
		files = append(files, File{Path: "aws/cloudtrail_recent.json", Content: []byte(redactText(string(trail)))})
	}

	return files, nil
}
