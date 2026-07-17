package stages_test

import (
	"testing"

	"github.com/WolframResearch/WAS-Kubernetes/internal/stages"
)

func TestAllReturnsSevenStages(t *testing.T) {
	all := stages.All()
	if len(all) != 7 {
		t.Fatalf("expected 7 stages, got %d", len(all))
	}
	want := []string{"preflight", "bootstrap", "backend", "infra", "kubeconfig", "addons", "app"}
	for i, s := range all {
		if s.Name() != want[i] {
			t.Errorf("stage[%d]: got %q, want %q", i, s.Name(), want[i])
		}
	}
}

func TestByName(t *testing.T) {
	s, err := stages.ByName("infra")
	if err != nil {
		t.Fatalf("ByName: %v", err)
	}
	if s.Name() != "infra" {
		t.Errorf("got %q", s.Name())
	}
}

func TestByNameUnknown(t *testing.T) {
	_, err := stages.ByName("nonexistent")
	if err == nil {
		t.Error("expected error for unknown stage")
	}
}

func TestSelect(t *testing.T) {
	got, err := stages.Select([]string{"app", "addons", "app"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name() != "addons" || got[1].Name() != "app" {
		t.Fatalf("got %v", namesOf(got))
	}
}

func TestSelectUnknown(t *testing.T) {
	_, err := stages.Select([]string{"addons", "nope"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestFrom(t *testing.T) {
	got, err := stages.From("addons")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name() != "addons" || got[1].Name() != "app" {
		t.Fatalf("got %v", namesOf(got))
	}
}

func namesOf(list []stages.Stage) []string {
	out := make([]string, len(list))
	for i, s := range list {
		out[i] = s.Name()
	}
	return out
}

func TestEachStageHasRequiredFields(t *testing.T) {
	for _, s := range stages.All() {
		if s.Name() == "" {
			t.Errorf("stage has empty Name()")
		}
		if s.Description() == "" {
			t.Errorf("stage %s has empty Description()", s.Name())
		}
		if s.EstimateText() == "" {
			t.Errorf("stage %s has empty EstimateText()", s.Name())
		}
		if s.Label() == "" {
			t.Errorf("stage %s has empty Label()", s.Name())
		}
		if len(s.Substeps()) == 0 {
			t.Errorf("stage %s has no Substeps()", s.Name())
		}
	}
}
