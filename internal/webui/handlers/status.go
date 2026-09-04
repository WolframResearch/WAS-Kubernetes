package handlers

import "github.com/WolframResearch/WAS-Kubernetes/internal/metabucket"

// clusterDisplayStatus derives the badge status shown in the web UI.
//
// Empty ClusterARN alone must not force "installing": workspaces can finish
// kubeconfig/app without a persisted ARN (e.g. --local infra, a failed persist
// after a successful apply). Prefer progress signals and the stored Status.
func clusterDisplayStatus(m *metabucket.Metadata) string {
	if m == nil {
		return "error"
	}
	if m.Status == "destroyed" {
		return "destroyed"
	}
	// Bootstrap-only: nothing beyond workspace creation is recorded yet.
	if m.ClusterARN == "" && m.ClusterUID == "" && m.IngressHost == "" {
		return "installing"
	}
	if m.Status == "installing" {
		// App stage records IngressHost last; treat that as complete.
		if m.IngressHost != "" {
			return "active"
		}
		return "installing"
	}
	if m.Status != "" {
		return m.Status
	}
	return "active"
}
