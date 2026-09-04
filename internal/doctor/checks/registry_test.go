package checks

import (
	"testing"

	"github.com/WolframResearch/WAS-Kubernetes/internal/doctor"
)

func TestAll_ReturnsChecks(t *testing.T) {
	all := All()
	if len(all) < 30 {
		t.Errorf("expected at least 30 checks, got %d", len(all))
	}
}

func TestAll_UniqueIDs(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range All() {
		if seen[c.ID()] {
			t.Errorf("duplicate check ID: %s", c.ID())
		}
		seen[c.ID()] = true
	}
}

func TestByCategory_Environment(t *testing.T) {
	env := ByCategory(doctor.CategoryEnvironment)
	if len(env) == 0 {
		t.Error("expected environment checks")
	}
	for _, c := range env {
		if c.Category() != doctor.CategoryEnvironment {
			t.Errorf("ByCategory returned wrong category for %s", c.ID())
		}
	}
}

func TestByCategory_Cluster(t *testing.T) {
	cluster := ByCategory(doctor.CategoryCluster)
	if len(cluster) == 0 {
		t.Error("expected cluster checks")
	}
}

func TestByCategory_Application(t *testing.T) {
	app := ByCategory(doctor.CategoryApplication)
	if len(app) == 0 {
		t.Error("expected application checks")
	}
}

func TestByID_Found(t *testing.T) {
	c := ByID("aws.credentials")
	if c == nil {
		t.Fatal("expected to find aws.credentials")
	}
	if c.ID() != "aws.credentials" {
		t.Errorf("wrong ID: %s", c.ID())
	}
}

func TestByID_NotFound(t *testing.T) {
	c := ByID("nonexistent.check")
	if c != nil {
		t.Error("expected nil for unknown check ID")
	}
}

func TestAllChecks_HaveNames(t *testing.T) {
	for _, c := range All() {
		if c.Name() == "" {
			t.Errorf("check %s has empty Name()", c.ID())
		}
		if c.Description() == "" {
			t.Errorf("check %s has empty Description()", c.ID())
		}
	}
}

func TestAllChecks_SafeToFixFalse(t *testing.T) {
	// No checks currently enable automatic repair.
	for _, c := range All() {
		if c.SafeToFix() {
			t.Errorf("unexpected SafeToFix=true for %s", c.ID())
		}
	}
}
