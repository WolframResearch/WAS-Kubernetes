package stages

import (
	"testing"

	"github.com/WolframResearch/WAS-Kubernetes/internal/config"
	"github.com/WolframResearch/WAS-Kubernetes/internal/metabucket"
	"github.com/WolframResearch/WAS-Kubernetes/internal/workspace"
)

func TestEnsureClusterARN_AWS(t *testing.T) {
	cfg := &config.Config{
		Cloud:       "aws",
		ClusterName: config.Field[string]{Value: "demo"},
		Region:      config.Field[string]{Value: "us-east-1"},
	}
	w := &workspace.Workspace{Meta: &metabucket.Metadata{AWSRegion: "us-east-1"}}
	ensureClusterARN(cfg, w, "123456789012")
	want := "arn:aws:eks:us-east-1:123456789012:cluster/demo"
	if w.Meta.ClusterARN != want {
		t.Fatalf("got %q want %q", w.Meta.ClusterARN, want)
	}
	// idempotent
	ensureClusterARN(cfg, w, "999")
	if w.Meta.ClusterARN != want {
		t.Fatalf("should not overwrite existing ARN")
	}
}

func TestEnsureClusterARN_Azure(t *testing.T) {
	cfg := &config.Config{
		Cloud:       "azure",
		ClusterName: config.Field[string]{Value: "demo"},
	}
	w := &workspace.Workspace{Meta: &metabucket.Metadata{}}
	ensureClusterARN(cfg, w, "sub-1")
	want := workspace.ClusterResourceID("sub-1", "demo-rg", "demo-aks")
	if w.Meta.ClusterARN != want {
		t.Fatalf("got %q want %q", w.Meta.ClusterARN, want)
	}
}
