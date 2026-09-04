package checks

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/WolframResearch/WAS-Kubernetes/internal/doctor"
	"github.com/WolframResearch/WAS-Kubernetes/internal/versions"
)

// kubectlFn executes a kubectl command with the RunContext kubeconfig.
// Overridable for tests.
var kubectlFn = func(ctx context.Context, rc *doctor.RunContext, args ...string) ([]byte, error) {
	fullArgs := append([]string{
		"--kubeconfig", rc.Kubeconfig,
		"--context", rc.ContextName,
	}, args...)
	return runOutput(ctx, "kubectl", fullArgs...)
}

// clusterPrecondition is the shared precondition for all cluster checks.
func clusterPrecondition(_ context.Context, rc *doctor.RunContext) (bool, string) {
	if rc.Kubeconfig == "" {
		return true, noCluster
	}
	return false, ""
}

// ClusterReachable checks that kubectl cluster-info succeeds.
type ClusterReachable struct{ BaseCheck }

func (ClusterReachable) ID() string                { return "cluster.reachable" }
func (ClusterReachable) Name() string              { return "kubectl cluster-info succeeds" }
func (ClusterReachable) Category() doctor.Category { return doctor.CategoryCluster }
func (ClusterReachable) Description() string {
	return "Runs kubectl cluster-info to verify the cluster API server is reachable."
}
func (c ClusterReachable) Precondition(ctx context.Context, rc *doctor.RunContext) (bool, string) {
	return clusterPrecondition(ctx, rc)
}
func (c ClusterReachable) Run(ctx context.Context, rc *doctor.RunContext) doctor.Finding {
	out, err := kubectlFn(ctx, rc, "cluster-info")
	if err != nil {
		return fail(c.ID(), c.Name(), doctor.SeverityCritical,
			fmt.Sprintf("kubectl cluster-info failed: %v", err),
			"Check your kubeconfig and cluster status.\n"+
				"Run: aws eks update-kubeconfig --name <cluster> --region <region>\n"+
				"See: docs/Troubleshooting.md#cluster-unreachable")
	}
	first := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
	return pass(c.ID(), c.Name(), first)
}

// ClusterUID checks that the live cluster UID matches the workspace record.
type ClusterUID struct{ BaseCheck }

func (ClusterUID) ID() string                { return "cluster.uid_matches" }
func (ClusterUID) Name() string              { return "Recorded UID matches live cluster" }
func (ClusterUID) Category() doctor.Category { return doctor.CategoryCluster }
func (ClusterUID) Description() string {
	return "Compares the cluster UID stored in the workspace with the live cluster UID to detect accidental cross-cluster operations."
}
func (c ClusterUID) Precondition(ctx context.Context, rc *doctor.RunContext) (bool, string) {
	if rc.Kubeconfig == "" {
		return true, noCluster
	}
	if rc.Workspace == nil {
		return true, "no workspace available; skip UID comparison"
	}
	return false, ""
}
func (c ClusterUID) Run(ctx context.Context, rc *doctor.RunContext) doctor.Finding {
	out, err := kubectlFn(ctx, rc, "get", "namespace", "kube-system",
		"-o", "jsonpath={.metadata.uid}")
	if err != nil {
		return checkError(c.ID(), c.Name(), fmt.Sprintf("cannot read cluster UID: %v", err))
	}
	liveUID := strings.TrimSpace(string(out))
	stored := rc.Workspace.Meta.ClusterUID
	if stored == "" {
		return pass(c.ID(), c.Name(), "no UID stored in workspace yet; will be recorded on next install")
	}
	if liveUID != stored {
		return fail(c.ID(), c.Name(), doctor.SeverityCritical,
			fmt.Sprintf("UID mismatch: workspace has %s, cluster has %s", stored, liveUID),
			"Your workspace points to a different cluster than the one you are connected to.\n"+
				"Run `wasctl workspace info "+rc.Cfg.ClusterName.Value+"` and verify.\n"+
				"See: docs/Troubleshooting.md#uid-mismatch")
	}
	return pass(c.ID(), c.Name(), fmt.Sprintf("UID matches: %s", liveUID))
}

// ClusterK8sVersion checks the Kubernetes version is in the supported matrix.
type ClusterK8sVersion struct{ BaseCheck }

func (ClusterK8sVersion) ID() string                { return "cluster.k8s_version" }
func (ClusterK8sVersion) Name() string              { return "Kubernetes version in supported matrix" }
func (ClusterK8sVersion) Category() doctor.Category { return doctor.CategoryCluster }
func (ClusterK8sVersion) Description() string {
	return "Verifies the cluster Kubernetes version is supported."
}
func (c ClusterK8sVersion) Precondition(ctx context.Context, rc *doctor.RunContext) (bool, string) {
	return clusterPrecondition(ctx, rc)
}

func (c ClusterK8sVersion) Run(ctx context.Context, rc *doctor.RunContext) doctor.Finding {
	out, err := kubectlFn(ctx, rc, "version", "--output", "json")
	if err != nil {
		return checkError(c.ID(), c.Name(), fmt.Sprintf("kubectl version failed: %v", err))
	}
	var kv struct {
		ServerVersion struct {
			GitVersion string `json:"gitVersion"`
		} `json:"serverVersion"`
	}
	if err := json.Unmarshal(out, &kv); err != nil {
		return checkError(c.ID(), c.Name(), fmt.Sprintf("parse kubectl version: %v", err))
	}
	ver := strings.TrimPrefix(kv.ServerVersion.GitVersion, "v")
	prefixes := versions.SupportedK8sVersionPrefixes()
	for _, prefix := range prefixes {
		if strings.HasPrefix(ver, prefix) {
			return pass(c.ID(), c.Name(), "Kubernetes "+kv.ServerVersion.GitVersion)
		}
	}
	lo, hi := "?", "?"
	if len(prefixes) > 0 {
		lo = strings.TrimSuffix(prefixes[0], ".")
		hi = strings.TrimSuffix(prefixes[len(prefixes)-1], ".")
	}
	return fail(c.ID(), c.Name(), doctor.SeverityProblem,
		fmt.Sprintf("Kubernetes %s is not in the supported range (%s–%s)",
			kv.ServerVersion.GitVersion, lo, hi),
		"Upgrade Kubernetes to a supported version.\n"+
			"See: docs/Wasctl-Architecture.md#compatibility-matrix")
}

// ClusterNodesReady checks all nodes are in Ready state.
type ClusterNodesReady struct{ BaseCheck }

func (ClusterNodesReady) ID() string                { return "cluster.nodes_ready" }
func (ClusterNodesReady) Name() string              { return "All nodes Ready" }
func (ClusterNodesReady) Category() doctor.Category { return doctor.CategoryCluster }
func (ClusterNodesReady) Description() string {
	return "Checks that all cluster nodes are in Ready state."
}
func (c ClusterNodesReady) Precondition(ctx context.Context, rc *doctor.RunContext) (bool, string) {
	return clusterPrecondition(ctx, rc)
}
func (c ClusterNodesReady) Run(ctx context.Context, rc *doctor.RunContext) doctor.Finding {
	out, err := kubectlFn(ctx, rc, "get", "nodes", "-o", "json")
	if err != nil {
		return checkError(c.ID(), c.Name(), fmt.Sprintf("kubectl get nodes: %v", err))
	}
	var nodes struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Status struct {
				Conditions []struct {
					Type   string `json:"type"`
					Status string `json:"status"`
				} `json:"conditions"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(out, &nodes); err != nil {
		return checkError(c.ID(), c.Name(), fmt.Sprintf("parse nodes: %v", err))
	}
	var notReady []string
	for _, n := range nodes.Items {
		for _, cond := range n.Status.Conditions {
			if cond.Type == "Ready" && cond.Status != "True" {
				notReady = append(notReady, n.Metadata.Name)
			}
		}
	}
	if len(notReady) > 0 {
		return fail(c.ID(), c.Name(), doctor.SeverityProblem,
			fmt.Sprintf("%d node(s) not Ready: %s", len(notReady), strings.Join(notReady, ", ")),
			"Check node events: kubectl describe node <node-name>\n"+
				"See: docs/Troubleshooting.md#nodes-not-ready")
	}
	return pass(c.ID(), c.Name(), fmt.Sprintf("all %d node(s) Ready", len(nodes.Items)))
}

// ClusterSystemPods checks kube-system pods are all Running/Succeeded.
type ClusterSystemPods struct{ BaseCheck }

func (ClusterSystemPods) ID() string                { return "cluster.system_pods" }
func (ClusterSystemPods) Name() string              { return "All kube-system pods Running" }
func (ClusterSystemPods) Category() doctor.Category { return doctor.CategoryCluster }
func (ClusterSystemPods) Description() string {
	return "Checks that pods in kube-system are in Running or Succeeded phase."
}
func (c ClusterSystemPods) Precondition(ctx context.Context, rc *doctor.RunContext) (bool, string) {
	return clusterPrecondition(ctx, rc)
}
func (c ClusterSystemPods) Run(ctx context.Context, rc *doctor.RunContext) doctor.Finding {
	out, err := kubectlFn(ctx, rc, "get", "pods", "-n", "kube-system", "-o", "json")
	if err != nil {
		return checkError(c.ID(), c.Name(), fmt.Sprintf("kubectl get pods -n kube-system: %v", err))
	}
	var pods struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Status struct {
				Phase string `json:"phase"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(out, &pods); err != nil {
		return checkError(c.ID(), c.Name(), fmt.Sprintf("parse pods: %v", err))
	}
	var unhealthy []string
	for _, p := range pods.Items {
		if p.Status.Phase != "Running" && p.Status.Phase != "Succeeded" {
			unhealthy = append(unhealthy, p.Metadata.Name+"("+p.Status.Phase+")")
		}
	}
	if len(unhealthy) > 0 {
		return fail(c.ID(), c.Name(), doctor.SeverityProblem,
			fmt.Sprintf("%d pod(s) not healthy in kube-system: %s", len(unhealthy), strings.Join(unhealthy, ", ")),
			"Check pod events: kubectl describe pod -n kube-system <name>")
	}
	return pass(c.ID(), c.Name(), fmt.Sprintf("all %d kube-system pod(s) healthy", len(pods.Items)))
}

// ClusterMetricsServer checks metrics-server is installed and responding.
type ClusterMetricsServer struct{ BaseCheck }

func (ClusterMetricsServer) ID() string                { return "cluster.metrics_server" }
func (ClusterMetricsServer) Name() string              { return "metrics-server installed and responding" }
func (ClusterMetricsServer) Category() doctor.Category { return doctor.CategoryCluster }
func (ClusterMetricsServer) Description() string {
	return "Checks that metrics-server is present (required for HPA)."
}
func (c ClusterMetricsServer) Precondition(ctx context.Context, rc *doctor.RunContext) (bool, string) {
	return clusterPrecondition(ctx, rc)
}
func (c ClusterMetricsServer) Run(ctx context.Context, rc *doctor.RunContext) doctor.Finding {
	_, err := kubectlFn(ctx, rc, "get", "deployment", "metrics-server", "-n", "kube-system")
	if err != nil {
		return pass(c.ID(), c.Name(), "metrics-server is not installed (optional add-on)")
	}
	return pass(c.ID(), c.Name(), "metrics-server deployment found in kube-system")
}

// ClusterIRSA checks EKS OIDC provider is linked correctly.
type ClusterIRSA struct{ BaseCheck }

func (ClusterIRSA) ID() string                { return "cluster.irsa" }
func (ClusterIRSA) Name() string              { return "EKS OIDC provider linked correctly" }
func (ClusterIRSA) Category() doctor.Category { return doctor.CategoryCluster }
func (ClusterIRSA) Description() string {
	return "Checks that an OIDC provider exists for this EKS cluster (required for IRSA)."
}
func (c ClusterIRSA) Precondition(ctx context.Context, rc *doctor.RunContext) (bool, string) {
	if rc.Cfg.Cloud != "aws" {
		return true, "IRSA check only applies to AWS EKS clusters"
	}
	return clusterPrecondition(ctx, rc)
}

var awsEKSDescribeFn = func(ctx context.Context, clusterName, region string) ([]byte, error) {
	return runOutput(ctx, "aws", "eks", "describe-cluster",
		"--name", clusterName,
		"--region", region,
		"--query", "cluster.identity.oidc.issuer",
		"--output", "text")
}

func (c ClusterIRSA) Run(ctx context.Context, rc *doctor.RunContext) doctor.Finding {
	out, err := awsEKSDescribeFn(ctx, rc.Cfg.ClusterName.Value, rc.Cfg.Region.Value)
	if err != nil {
		return checkError(c.ID(), c.Name(), fmt.Sprintf("aws eks describe-cluster: %v", err))
	}
	issuer := strings.TrimSpace(string(out))
	if issuer == "" || issuer == "None" {
		return fail(c.ID(), c.Name(), doctor.SeverityProblem,
			"no OIDC issuer configured for this EKS cluster",
			"Enable the OIDC provider:\n"+
				"  eksctl utils associate-iam-oidc-provider --cluster <name> --approve\n"+
				"See: docs/Troubleshooting.md#irsa-oidc")
	}
	return pass(c.ID(), c.Name(), fmt.Sprintf("OIDC issuer: %s", issuer))
}

// ClusterWorkloadIdentity checks AKS workload identity when WAS needs it.
// Azure default installs use static storage-account keys for Resource Manager,
// so the WI webhook is optional. The check fails only when WAS is configured
// for workload identity and no webhook is present.
type ClusterWorkloadIdentity struct{ BaseCheck }

func (ClusterWorkloadIdentity) ID() string                { return "cluster.workload_identity" }
func (ClusterWorkloadIdentity) Name() string              { return "AKS: workload identity enabled" }
func (ClusterWorkloadIdentity) Category() doctor.Category { return doctor.CategoryCluster }
func (ClusterWorkloadIdentity) Description() string {
	return "Checks Azure Workload Identity when Resource Manager is configured to use it. Skipped as a hard requirement when static storage credentials are in use."
}
func (c ClusterWorkloadIdentity) Precondition(_ context.Context, rc *doctor.RunContext) (bool, string) {
	if rc.Cfg.Cloud != "azure" {
		return true, "Workload Identity check only applies to Azure AKS clusters"
	}
	if rc.Kubeconfig == "" {
		return true, noCluster
	}
	return false, ""
}
func (c ClusterWorkloadIdentity) Run(ctx context.Context, rc *doctor.RunContext) doctor.Finding {
	wiInUse := azureWorkloadIdentityInUse(ctx, rc)
	webhookOK := azureWorkloadIdentityWebhookPresent(ctx, rc)

	if webhookOK {
		return pass(c.ID(), c.Name(), "Azure Workload Identity webhook is present")
	}
	if !wiInUse {
		return pass(c.ID(), c.Name(),
			"Workload Identity webhook not found; not required while Resource Manager uses static storage credentials (Azure chart default)")
	}
	return fail(c.ID(), c.Name(), doctor.SeverityProblem,
		"Resource Manager is configured for Workload Identity but the webhook was not found",
		"Enable workload identity on the AKS cluster:\n"+
			"  az aks update -n <cluster> -g <rg> --enable-workload-identity --enable-oidc-issuer\n"+
			"Or switch objectStorage.auth.mode back to static if you are not using WI.")
}

// azureWorkloadIdentityInUse reports whether WAS resource-manager is labeled/annotated for WI.
func azureWorkloadIdentityInUse(ctx context.Context, rc *doctor.RunContext) bool {
	// Annotation on the ServiceAccount (client-id).
	out, err := kubectlFn(ctx, rc, "get", "sa", "resource-manager", "-n", "was",
		"-o", `jsonpath={.metadata.annotations.azure\.workload\.identity/client-id}`)
	if err == nil && strings.TrimSpace(string(out)) != "" {
		return true
	}
	// Pods explicitly requesting WI injection.
	out, err = kubectlFn(ctx, rc, "get", "pods", "-n", "was",
		"-l", "azure.workload.identity/use=true",
		"-o", "name")
	if err == nil && strings.TrimSpace(string(out)) != "" {
		return true
	}
	return false
}

// azureWorkloadIdentityWebhookPresent looks for the AKS WI mutating webhook in
// either the dedicated namespace or kube-system (placement varies by AKS version).
func azureWorkloadIdentityWebhookPresent(ctx context.Context, rc *doctor.RunContext) bool {
	candidates := [][]string{
		{"get", "deployment", "-n", "azure-workload-identity-system", "azure-wi-webhook-controller-manager"},
		{"get", "deployment", "-n", "kube-system", "azure-wi-webhook-controller-manager"},
		{"get", "mutatingwebhookconfiguration", "azure-wi-webhook-mutating-webhook-configuration"},
	}
	for _, args := range candidates {
		if _, err := kubectlFn(ctx, rc, args...); err == nil {
			return true
		}
	}
	return false
}
