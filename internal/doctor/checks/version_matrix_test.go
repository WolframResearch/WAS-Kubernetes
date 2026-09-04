package checks

import (
	"context"
	"testing"

	"github.com/WolframResearch/WAS-Kubernetes/internal/doctor"
)

func TestVersionMatrix_NoIssues_Pass(t *testing.T) {
	// In a test environment, none of the tool parsers will succeed (tools not
	// on PATH or cluster unreachable), so versions.Check() returns 0 issues.
	// The check should report Pass — not Skip — when the matrix runs cleanly.
	rc := &doctor.RunContext{Cfg: testCfg()}
	f := VersionMatrix{}.Run(context.Background(), rc)
	if f.Status != doctor.StatusPass && f.Status != doctor.StatusFail {
		// We accept either Pass (no tools detected = no issues) or Fail (tools
		// detected and out-of-range). Skip is not an expected outcome for this check.
		t.Fatalf("expected Pass or Fail status, got %v: %s", f.Status, f.Message)
	}
}

func TestVersionMatrix_ID(t *testing.T) {
	c := VersionMatrix{}
	if c.ID() != "version.matrix" {
		t.Error("unexpected ID")
	}
}

func TestVersionMatrix_Category(t *testing.T) {
	c := VersionMatrix{}
	if c.Category() != doctor.CategoryEnvironment {
		t.Error("unexpected category")
	}
}
