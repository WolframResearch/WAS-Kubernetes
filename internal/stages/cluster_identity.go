package stages

import (
	"github.com/WolframResearch/WAS-Kubernetes/internal/config"
	"github.com/WolframResearch/WAS-Kubernetes/internal/workspace"
)

// ensureClusterARN fills Meta.ClusterARN when missing so workspace identity
// heals after --local infra or a failed persist following a successful apply.
func ensureClusterARN(cfg *config.Config, w *workspace.Workspace, accountID string) {
	if w == nil || w.Meta == nil || w.Meta.ClusterARN != "" || accountID == "" {
		return
	}
	if cfg.Cloud == "azure" {
		rg := workspace.AzureStackResourceGroup(cfg.ClusterName.Value)
		aks := workspace.AzureAKSName(cfg.ClusterName.Value)
		w.Meta.ClusterARN = workspace.ClusterResourceID(accountID, rg, aks)
		return
	}
	region := cfg.Region.Value
	if region == "" {
		region = w.Meta.AWSRegion
	}
	w.Meta.ClusterARN = workspace.ClusterARN(region, accountID, cfg.ClusterName.Value)
}
