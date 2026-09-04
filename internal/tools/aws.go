package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// AWSUpdateKubeconfig returns the command to generate an isolated kubeconfig
// for an EKS cluster. kubeconfigPath must be a workspace/temp path — never
// ~/.kube/config.
func AWSUpdateKubeconfig(region, clusterName, kubeconfigPath string) []string {
	return []string{
		"aws", "eks", "update-kubeconfig",
		"--kubeconfig", kubeconfigPath,
		"--region", region,
		"--name", clusterName,
	}
}

// CallerIdentity holds the fields returned by aws sts get-caller-identity.
type CallerIdentity struct {
	Account string `json:"Account"`
	UserID  string `json:"UserId"`
	ARN     string `json:"Arn"`
}

// GetCallerIdentity calls aws sts get-caller-identity and returns the parsed
// result. It is a synchronous read used in preflight and bucket-name
// derivation; it does not stream output.
func GetCallerIdentity(ctx context.Context, region string) (CallerIdentity, error) {
	args := []string{"aws", "sts", "get-caller-identity", "--output", "json"}
	if region != "" {
		args = append(args, "--region", region)
	}
	out, err := exec.CommandContext(ctx, args[0], args[1:]...).Output() //nolint:gosec
	if err != nil {
		return CallerIdentity{}, fmt.Errorf("aws sts get-caller-identity: %w", err)
	}
	var id CallerIdentity
	if err := json.Unmarshal(bytes.TrimSpace(out), &id); err != nil {
		return CallerIdentity{}, fmt.Errorf("parse caller identity: %w", err)
	}
	return id, nil
}

// EKSClusterStatus returns the cluster.status field from aws eks describe-cluster
// (e.g. "ACTIVE", "CREATING"). Returns an error if the cluster does not exist.
func EKSClusterStatus(ctx context.Context, region, clusterName string) (string, error) {
	args := []string{
		"aws", "eks", "describe-cluster",
		"--name", clusterName,
		"--query", "cluster.status",
		"--output", "text",
	}
	if region != "" {
		args = append(args, "--region", region)
	}
	out, err := exec.CommandContext(ctx, args[0], args[1:]...).Output() //nolint:gosec
	if err != nil {
		return "", fmt.Errorf("aws eks describe-cluster %q: %w", clusterName, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// EKSNodegroupCount returns how many managed node groups the cluster has.
func EKSNodegroupCount(ctx context.Context, region, clusterName string) (int, error) {
	args := []string{
		"aws", "eks", "list-nodegroups",
		"--cluster-name", clusterName,
		"--query", "length(nodegroups)",
		"--output", "text",
	}
	if region != "" {
		args = append(args, "--region", region)
	}
	out, err := exec.CommandContext(ctx, args[0], args[1:]...).Output() //nolint:gosec
	if err != nil {
		return 0, fmt.Errorf("aws eks list-nodegroups %q: %w", clusterName, err)
	}
	var n int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(out)), "%d", &n); err != nil {
		return 0, fmt.Errorf("parse nodegroup count %q: %w", strings.TrimSpace(string(out)), err)
	}
	return n, nil
}

// DeleteLaunchTemplateIfExists deletes an EC2 launch template by exact name.
// Missing templates are treated as success. Used to clear orphans from older
// installs that used a fixed launch_template name.
func DeleteLaunchTemplateIfExists(ctx context.Context, region, name string) error {
	args := []string{
		"aws", "ec2", "delete-launch-template",
		"--launch-template-name", name,
		"--output", "text",
	}
	if region != "" {
		args = append(args, "--region", region)
	}
	out, err := exec.CommandContext(ctx, args[0], args[1:]...).CombinedOutput() //nolint:gosec
	if err == nil {
		return nil
	}
	msg := string(out) + err.Error()
	if strings.Contains(msg, "InvalidLaunchTemplateName.NotFoundException") ||
		strings.Contains(msg, "does not exist") {
		return nil
	}
	return fmt.Errorf("delete launch template %q: %w (%s)", name, err, strings.TrimSpace(string(out)))
}
