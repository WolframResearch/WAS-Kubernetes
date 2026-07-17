// Package cloud abstracts the cloud-specific settings consumed by wasctl stages.
// ForName returns AWSCloud or AzureCloud; unknown names default to AWS.
package cloud

import "github.com/WolframResearch/WAS-Kubernetes/internal/config"

// Cloud is implemented by each supported cloud provider.
type Cloud interface {
	// Name returns the short identifier ("aws", "azure").
	Name() string
	// StorageClassName is the Kubernetes StorageClass used for PVCs.
	StorageClassName() string
	// HelmValuesFile returns the cloud-specific values file path relative to
	// the repo root.
	HelmValuesFile() string
	// HelmExtraSets returns additional helm --set key=value pairs specific to
	// this cloud.
	HelmExtraSets(cfg *config.Config) map[string]string
}

// AWSCloud is the production AWS implementation.
type AWSCloud struct{}

func (AWSCloud) Name() string             { return "aws" }
func (AWSCloud) StorageClassName() string { return "was-efs" }
func (AWSCloud) HelmValuesFile() string {
	return "charts/wolfram-application-server/values-aws.yaml"
}
func (AWSCloud) HelmExtraSets(cfg *config.Config) map[string]string {
	return map[string]string{
		"ingress.host": cfg.IngressHost.Value,
	}
}

// ForName returns the Cloud implementation for the given name.
// Returns AWSCloud for unrecognized names.
func ForName(name string) Cloud {
	if name == "azure" {
		return AzureCloud{}
	}
	return AWSCloud{}
}
