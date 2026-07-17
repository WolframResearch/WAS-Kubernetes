package stages_test

import (
	"testing"

	"github.com/WolframResearch/WAS-Kubernetes/internal/stages"
)

// TestAppSubstepNames verifies the three substeps in the App stage.
func TestAppSubstepNames(t *testing.T) {
	a := stages.App{}
	subs := a.Substeps()
	if len(subs) != 3 {
		t.Fatalf("expected 3 substeps, got %d: %v", len(subs), subs)
	}
	want := []string{"Open workspace", "Safety check", "Deploy WAS chart"}
	for i, w := range want {
		if subs[i] != w {
			t.Errorf("substep[%d]: got %q, want %q", i, subs[i], w)
		}
	}
}

// TestAppMetadata verifies App stage metadata fields are non-empty.
func TestAppMetadata(t *testing.T) {
	a := stages.App{}
	if a.Name() != "app" {
		t.Errorf("Name: %q", a.Name())
	}
	if a.Description() == "" {
		t.Error("Description must not be empty")
	}
	if a.EstimateText() == "" {
		t.Error("EstimateText must not be empty")
	}
	if a.Label() == "" {
		t.Error("Label must not be empty")
	}
}

// NOTE: App.Apply and App.Destroy both require an AWS meta bucket (workspace)
// and a live EKS cluster. These are tested by integration tests only.
