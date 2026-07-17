package stages

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/WolframResearch/WAS-Kubernetes/internal/config"
	"github.com/WolframResearch/WAS-Kubernetes/internal/runner"
	"github.com/WolframResearch/WAS-Kubernetes/internal/tools"
	"github.com/WolframResearch/WAS-Kubernetes/internal/workspace"
)

// prepareStackTF initializes a Terraform stack working directory so
// `terraform output` can read remote state. Infra applies into a temp
// materialization (or --local repo path); later stages must re-init against
// the same backend before reading outputs.
//
// Non-local: materializes the stack into w (sets w.tempDir for kubeconfig).
// Local: uses the repo infra path and ensures w has a tempDir for kubeconfig.
func prepareStackTF(ctx context.Context, cfg *config.Config, w *workspace.Workspace, r runner.Runner, rep runner.Reporter) (string, error) {
	if cfg.Local {
		stackDir := localStackDir(cfg)
		if err := initStackTF(ctx, cfg, r, rep, stackDir, localBackendPath(cfg)); err != nil {
			return "", err
		}
		// Kubeconfig needs a temp dir; keep an existing one if present.
		if err := w.MaterializeTempDir(); err != nil {
			return "", fmt.Errorf("temp dir for kubeconfig: %w", err)
		}
		return stackDir, nil
	}

	mat, err := w.Materialize(ctx, stackFS(cfg), workspace.Stack)
	if err != nil {
		return "", fmt.Errorf("materialize stack for terraform outputs: %w", err)
	}
	backendPath := filepath.Join(mat.StackDir, backendFileName(cfg))
	if err := initStackTF(ctx, cfg, r, rep, mat.StackDir, backendPath); err != nil {
		return "", err
	}
	return mat.StackDir, nil
}

func stackFS(cfg *config.Config) fs.FS {
	if cfg.Cloud == "azure" {
		return azureStackAssets(cfg)
	}
	return stackAssets(cfg)
}

func initStackTF(ctx context.Context, cfg *config.Config, r runner.Runner, rep runner.Reporter, stackDir, backendPath string) error {
	if cfg.Cloud == "azure" && !cfg.DryRun {
		storageAccount, err := parseBackendTFVarsStorageAccount(backendPath)
		if err != nil {
			return err
		}
		// Soft wait: infra already created the account; avoid formal substeps so
		// callers (addons/app) keep their own progress UI intact.
		if err := waitForAzureStateBlobDNS(ctx, storageAccount, rep); err != nil {
			return err
		}
	}
	rep.LogLine("[info] initializing Terraform to read stack outputs…")
	if err := runCmd(ctx, cfg, r, rep, tools.TerraformInit(stackDir, backendPath)); err != nil {
		return fmt.Errorf("terraform init for stack outputs: %w", err)
	}
	return nil
}

func localBackendPath(cfg *config.Config) string {
	if cfg.Cloud == "azure" {
		return filepath.Join(cfg.RepoRoot, azureLocalStackDir, "backend.tfvars")
	}
	return filepath.Join(cfg.RepoRoot, stackDir, "backend.hcl")
}

func backendFileName(cfg *config.Config) string {
	if cfg.Cloud == "azure" {
		return "backend.tfvars"
	}
	return "backend.hcl"
}

func requireTFOutput(ctx context.Context, stackDir, key string) (string, error) {
	val, err := tools.TerraformOutput(ctx, stackDir, key)
	if err != nil {
		return "", fmt.Errorf("terraform output %s unavailable (is stage 'infra' complete?): %w", key, err)
	}
	if val == "" {
		return "", fmt.Errorf("terraform output %s is empty (is stage 'infra' complete?)", key)
	}
	return val, nil
}
