package workspace

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/WolframResearch/WAS-Kubernetes/internal/metabucket"
)

// VerifyCluster runs safety checks before any cluster-mutating operation
// (install stages from kubeconfig onward, destroy). It dispatches to the
// appropriate cloud-specific implementation based on w.Meta.Cloud.
//
// AWS checks:
//  1. Current AWS account matches workspace.AWSAccountID (cross-account guard).
//  2. Cluster ARN exists: aws eks describe-cluster succeeds.
//  3. Cluster is reachable: kubectl cluster-info succeeds.
//  4. kube-system UID match (detects delete+recreate with same name).
//
// Azure checks:
//  1. Current subscription ID matches workspace.AzureSubscriptionID.
//  2. AKS cluster exists: az aks show succeeds.
//  3. Cluster is reachable: kubectl cluster-info succeeds.
//  4. kube-system UID match.
func VerifyCluster(ctx context.Context, w *Workspace, kubeconfigPath string) error {
	if w.Meta.Cloud == "azure" {
		return verifyAzureCluster(ctx, w, kubeconfigPath)
	}
	return verifyAWSCluster(ctx, w, kubeconfigPath)
}

func verifyAWSCluster(ctx context.Context, w *Workspace, kubeconfigPath string) error {
	// 1. Account match.
	liveID, err := liveAWSAccountID(ctx)
	if err != nil {
		return fmt.Errorf("verify cluster: get caller identity: %w", err)
	}
	if liveID != w.Meta.AWSAccountID {
		return fmt.Errorf(
			"this workspace was created from account %s, but you are authenticated as account %s.\n"+
				"Switch AWS credentials (aws configure / AWS_PROFILE) and retry.",
			w.Meta.AWSAccountID, liveID,
		)
	}

	contextName := w.ContextName()

	// 2. Cluster ARN exists.
	describeCmd := exec.CommandContext(ctx,
		"aws", "eks", "describe-cluster",
		"--name", w.Meta.ClusterName,
		"--region", w.Meta.AWSRegion,
		"--output", "text",
		"--query", "cluster.status",
	)
	descOut, descErr := describeCmd.Output()
	if descErr != nil {
		return fmt.Errorf(
			"cluster %q not found in region %s (account %s): %w",
			w.Meta.ClusterName, w.Meta.AWSRegion, w.Meta.AWSAccountID, descErr,
		)
	}
	if status := strings.TrimSpace(string(descOut)); status != "ACTIVE" {
		return fmt.Errorf("cluster %q status is %q, expected ACTIVE", w.Meta.ClusterName, status)
	}

	// 3. Cluster is reachable.
	if err := kubectlClusterInfo(ctx, kubeconfigPath, contextName, w.Meta.ClusterName); err != nil {
		return err
	}

	// 4. UID check.
	return verifyOrRecordUID(ctx, w, kubeconfigPath, contextName)
}

func verifyAzureCluster(ctx context.Context, w *Workspace, kubeconfigPath string) error {
	// 1. Subscription match.
	liveSubID, err := liveAzureSubscriptionID(ctx)
	if err != nil {
		return fmt.Errorf("verify cluster: az account show: %w", err)
	}
	if liveSubID != w.Meta.AzureSubscriptionID {
		return fmt.Errorf(
			"this workspace was created in subscription %s, but the active subscription is %s.\n"+
				"Switch subscriptions (az account set --subscription <id>) and retry.",
			w.Meta.AzureSubscriptionID, liveSubID,
		)
	}

	contextName := w.ContextName()

	// 2. AKS cluster exists (stack RG + …-aks name — not meta RG / logical name).
	aksName := AzureAKSName(w.Meta.ClusterName)
	rg := AzureStackResourceGroup(w.Meta.ClusterName)
	showCmd := exec.CommandContext(ctx,
		"az", "aks", "show",
		"--resource-group", rg,
		"--name", aksName,
		"--output", "tsv",
		"--query", "provisioningState",
	)
	showOut, showErr := showCmd.Output()
	if showErr != nil {
		return fmt.Errorf(
			"AKS cluster %q not found in resource group %s: %w",
			aksName, rg, showErr,
		)
	}
	if state := strings.TrimSpace(string(showOut)); state != "Succeeded" {
		return fmt.Errorf("AKS cluster %q provisioning state is %q, expected Succeeded", aksName, state)
	}

	// 3. Cluster is reachable.
	if err := kubectlClusterInfo(ctx, kubeconfigPath, contextName, w.Meta.ClusterName); err != nil {
		return err
	}

	// 4. UID check.
	return verifyOrRecordUID(ctx, w, kubeconfigPath, contextName)
}

// kubectlClusterInfo runs kubectl cluster-info as a reachability check.
func kubectlClusterInfo(ctx context.Context, kubeconfigPath, contextName, clusterName string) error {
	cmd := exec.CommandContext(ctx,
		"kubectl", "--kubeconfig", kubeconfigPath, "--context", contextName,
		"cluster-info",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf(
			"kubectl cluster-info failed for %s: %w\n%s",
			clusterName, err, strings.TrimSpace(string(out)),
		)
	}
	return nil
}

// verifyOrRecordUID checks the kube-system namespace UID against the workspace
// record, or records it on first run.
func verifyOrRecordUID(ctx context.Context, w *Workspace, kubeconfigPath, contextName string) error {
	liveUID, err := kubeSysUID(ctx, kubeconfigPath, contextName)
	if err != nil {
		return fmt.Errorf("get kube-system UID: %w", err)
	}
	if w.Meta.ClusterUID == "" {
		w.Meta.ClusterUID = liveUID
		return nil
	}
	if liveUID != w.Meta.ClusterUID {
		return fmt.Errorf(
			"cluster %q has kube-system UID %s, but workspace records UID %s.\n"+
				"Someone may have deleted and recreated this cluster.\n"+
				"Refusing to proceed to avoid operating on the wrong cluster.\n"+
				"If you are certain this is the same cluster, run:\n"+
				"  wasctl workspace reset-uid %s",
			w.Meta.ClusterName, liveUID, w.Meta.ClusterUID, w.Meta.ClusterName,
		)
	}
	return nil
}

// liveAWSAccountID returns the caller's AWS account ID.
func liveAWSAccountID(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx,
		"aws", "sts", "get-caller-identity",
		"--output", "text", "--query", "Account",
	).Output()
	if err != nil {
		return "", fmt.Errorf("aws sts get-caller-identity: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// liveAzureSubscriptionID returns the active Azure subscription ID.
func liveAzureSubscriptionID(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx,
		"az", "account", "show",
		"--output", "tsv", "--query", "id",
	).Output()
	if err != nil {
		return "", fmt.Errorf("az account show: %w\nHint: run 'az login' to authenticate", err)
	}
	id := strings.TrimSpace(string(out))
	if id == "" {
		return "", fmt.Errorf("az account show returned empty subscription ID")
	}
	return id, nil
}

// kubeSysUID returns the UID of the kube-system namespace.
func kubeSysUID(ctx context.Context, kubeconfigPath, contextName string) (string, error) {
	out, err := exec.CommandContext(ctx,
		"kubectl", "--kubeconfig", kubeconfigPath, "--context", contextName,
		"get", "ns", "kube-system",
		"-o", "jsonpath={.metadata.uid}",
	).Output()
	if err != nil {
		return "", err
	}
	uid := strings.TrimSpace(string(out))
	if uid == "" {
		return "", fmt.Errorf("kube-system namespace UID is empty")
	}
	return uid, nil
}

// ErrWorkspaceNotFound re-exported for callers that don't import metabucket.
type ErrWorkspaceNotFound = metabucket.ErrWorkspaceNotFound
