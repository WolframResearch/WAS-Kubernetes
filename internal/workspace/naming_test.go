package workspace

import (
	"testing"

	"github.com/WolframResearch/WAS-Kubernetes/internal/metabucket"
)

func TestAzureAKSNaming(t *testing.T) {
	if got := AzureStackResourceGroup("wasctl"); got != "wasctl-rg" {
		t.Fatalf("stack rg: got %q", got)
	}
	if got := AzureAKSName("wasctl"); got != "wasctl-aks" {
		t.Fatalf("aks name: got %q", got)
	}
	if got := AzureNodeResourceGroup("wasctl", "eastus"); got != "MC_wasctl-rg_wasctl-aks_eastus" {
		t.Fatalf("node rg: got %q", got)
	}
	id := ClusterResourceID("sub", "wasctl-rg", "wasctl-aks")
	want := "/subscriptions/sub/resourceGroups/wasctl-rg/providers/Microsoft.ContainerService/managedClusters/wasctl-aks"
	if id != want {
		t.Fatalf("resource id: got %q", id)
	}
}

func TestContextNameAzureUsesAKSAdmin(t *testing.T) {
	w := &Workspace{Meta: &metabucket.Metadata{
		Cloud:       "azure",
		ClusterName: "wasctl",
		// Stale meta RG must not affect kubectl context name.
		AzureResourceGroup: "wolfram-wasctl-meta",
	}}
	if got := w.ContextName(); got != "wasctl-aks-admin" {
		t.Fatalf("ContextName: got %q, want wasctl-aks-admin", got)
	}
}
