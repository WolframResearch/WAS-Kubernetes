package addons

import (
	"github.com/WolframResearch/WAS-Kubernetes/internal/config"
	"github.com/WolframResearch/WAS-Kubernetes/internal/runner"
)

// RunContext carries per-invocation state for every Addon. Constructed once
// by the addons stage and passed to every Check/Install/Verify call.
//
// All kubectl/helm calls use Kubeconfig + KubeContext explicitly. ~/.kube/config
// is never read or modified.
type RunContext struct {
	Cfg         *config.Config
	Kubeconfig  string // absolute path to isolated kubeconfig
	KubeContext string // cluster ARN (AWS) or AKS context name

	// AWS-only (empty on Azure)
	EFSFilesystemID string // terraform output efs_filesystem_id
	EFSCSIRoleARN   string // terraform output efs_csi_driver_role_arn
	EBSCSIRoleARN   string // terraform output ebs_csi_driver_role_arn

	// Azure-only (empty on AWS)
	AzureStorageAccount string // unused; kept for API compatibility of AddonContext
	AzureIngressFQDN    string // {dns-label}.{location}.cloudapp.azure.com after ingress-nginx install

	// Azure Files CSI (was-azurefile StorageClass) — from infra/azure/stack filesystem.tf
	AzureFilesystemAccount string // terraform output filesystem_storage_account_name
	AzureFilesystemKey     string // terraform output filesystem_storage_account_key (sensitive)
	AzureFilesystemRG      string // stack RG hosting the account (<cluster>-rg)
	AzureFilesystemSKU     string // Premium_LRS (default) or Standard_LRS

	Runner   runner.Runner   // mockable; used for all helm/kubectl calls
	Reporter runner.Reporter // progress output (TUI or plain)
}

// helmFlags returns the flags every helm invocation must carry.
func (rc *RunContext) helmFlags() []string {
	return []string{"--kubeconfig", rc.Kubeconfig, "--kube-context", rc.KubeContext}
}

// kubectlFlags returns the flags every kubectl invocation must carry.
func (rc *RunContext) kubectlFlags() []string {
	return []string{"--kubeconfig", rc.Kubeconfig, "--context", rc.KubeContext}
}
