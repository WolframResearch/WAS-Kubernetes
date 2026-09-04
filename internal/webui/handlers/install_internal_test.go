package handlers

import "testing"

// TestInstallConfigInheritsLocalFlag verifies that buildConfig propagates the
// --local flag and repo root into the resulting Config. This guards against
// the regression where InstallConfirm built configs with Local=false even when
// wasctl was started with --local, causing the addons stage to fail.
func TestInstallConfigInheritsLocalFlag(t *testing.T) {
	form := installFormData{
		Cloud:       "aws",
		ClusterName: "was-dev",
		Region:      "us-east-1",
		IngressHost: "was.example.com",
		K8sVersion:  "1.30",
		NodeType:    "m5.xlarge",
		NodeMin:     "2",
		NodeDesired: "3",
		NodeMax:     "5",
	}

	cfg := buildConfig(form, "us-east-1", true, "/home/dev/WAS-Kubernetes")

	if !cfg.Local {
		t.Error("cfg.Local: got false, want true")
	}
	if cfg.RepoRoot != "/home/dev/WAS-Kubernetes" {
		t.Errorf("cfg.RepoRoot: got %q, want %q", cfg.RepoRoot, "/home/dev/WAS-Kubernetes")
	}
}

func TestInstallConfigLocalFalseByDefault(t *testing.T) {
	form := installFormData{
		Cloud:       "aws",
		ClusterName: "was-prod",
		Region:      "us-east-1",
		IngressHost: "was.example.com",
	}

	cfg := buildConfig(form, "us-east-1", false, "")

	if cfg.Local {
		t.Error("cfg.Local: got true, want false (non-local serve should not set Local)")
	}
	if cfg.RepoRoot != "" {
		t.Errorf("cfg.RepoRoot: got %q, want empty string", cfg.RepoRoot)
	}
}
