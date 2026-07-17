package cloud

import "github.com/WolframResearch/WAS-Kubernetes/internal/config"

// AzureCloud is the Azure implementation of Cloud.
type AzureCloud struct{}

func (AzureCloud) Name() string             { return "azure" }
func (AzureCloud) StorageClassName() string { return "azurefile" }
func (AzureCloud) HelmValuesFile() string {
	return "charts/wolfram-application-server/values-azure.yaml"
}

// HelmExtraSets returns --set overrides for helm upgrade --install on Azure.
// ingress.host comes from config. Object storage account name and managed-identity
// client ID are applied later from workspace metadata (Terraform outputs written
// by the infra stage).
func (AzureCloud) HelmExtraSets(cfg *config.Config) map[string]string {
	if cfg == nil {
		return nil
	}
	return map[string]string{
		"ingress.host": cfg.IngressHost.Value,
	}
}
