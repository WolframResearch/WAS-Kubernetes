package stages

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/WolframResearch/WAS-Kubernetes/internal/config"
	"github.com/WolframResearch/WAS-Kubernetes/internal/runner"
	"github.com/WolframResearch/WAS-Kubernetes/internal/workspace"
)

// Kubeconfig generates an isolated kubeconfig for the cluster and
// verifies connectivity. It never writes to ~/.kube/config.
// AWS: aws eks update-kubeconfig
// Azure: az aks get-credentials --admin (local cluster-admin cert)
type Kubeconfig struct{}

func (Kubeconfig) Name() string         { return "kubeconfig" }
func (Kubeconfig) Description() string  { return "kubeconfig configured" }
func (Kubeconfig) EstimateText() string { return "~10 sec" }
func (Kubeconfig) Label() string        { return "Configuring kubeconfig" }
func (Kubeconfig) DestroyLabel() string { return "Removing kubeconfig" }
func (Kubeconfig) Substeps() []string {
	return []string{"Open workspace", "Generate kubeconfig", "Verify cluster connectivity", "Record cluster UID"}
}

// Check verifies that the workspace has a clusterUID (meaning kubeconfig has
// been run at least once and the cluster was reachable).
func (Kubeconfig) Check(ctx context.Context, cfg *config.Config) error {
	accountID, err := cloudAccountID(ctx, cfg)
	if err != nil {
		return fmt.Errorf("kubeconfig check: %w", err)
	}
	w, err := workspace.OpenForCloud(ctx, cfg, accountID, cfg.ClusterName.Value)
	if err != nil {
		return fmt.Errorf("kubeconfig not configured: %w", err)
	}
	if w.Meta.ClusterUID == "" {
		return fmt.Errorf("kubeconfig not configured: clusterUID not recorded")
	}
	return nil
}

func (Kubeconfig) Apply(ctx context.Context, cfg *config.Config, r runner.Runner, rep runner.Reporter) error {
	accountID, err := cloudAccountID(ctx, cfg)
	if err != nil {
		return fmt.Errorf("derive account ID: %w", err)
	}

	rep.SubstepStart("Open workspace")
	w, err := workspace.OpenForCloud(ctx, cfg, accountID, cfg.ClusterName.Value)
	if err != nil {
		rep.SubstepFail(err)
		return err
	}
	if err := w.CheckCloudMatch(cfg.Cloud); err != nil {
		rep.SubstepFail(err)
		return err
	}
	if err := w.Lock(ctx); err != nil {
		rep.SubstepFail(err)
		return err
	}
	defer w.Unlock(ctx)
	if err := w.MaterializeTempDir(); err != nil {
		rep.SubstepFail(err)
		return err
	}
	defer func() {
		if persistErr := w.PersistMeta(ctx); persistErr != nil {
			rep.LogLine("[!] persist meta failed: " + persistErr.Error())
		}
		w.Close()
	}()
	rep.SubstepDone()

	rep.SubstepStart("Generate kubeconfig")
	if cfg.Cloud == "azure" {
		aks := workspace.AzureAKSName(cfg.ClusterName.Value)
		rep.LogLine("-> az aks get-credentials --admin --resource-group " +
			workspace.AzureStackResourceGroup(cfg.ClusterName.Value) +
			" --name " + aks + " --file <tempfile>")
	} else {
		rep.LogLine("-> aws eks update-kubeconfig --kubeconfig <tempfile> --name " + cfg.ClusterName.Value)
	}
	kubeconfigPath, err := w.Kubeconfig(ctx)
	if err != nil {
		rep.SubstepFail(err)
		return err
	}
	rep.LogLine("[✓] Kubeconfig written to isolated temp file (not ~/.kube/config)")
	rep.SubstepDone()

	contextName := w.ContextName()

	rep.SubstepStart("Verify cluster connectivity")
	if !cfg.DryRun {
		cmd := []string{"kubectl", "--kubeconfig", kubeconfigPath, "--context", contextName, "cluster-info"}
		if err := runCmd(ctx, cfg, r, rep, cmd); err != nil {
			errMsg := strings.ToLower(err.Error())
			if cfg.Cloud == "azure" && (strings.Contains(errMsg, "forbidden") ||
				strings.Contains(errMsg, "local accounts") ||
				strings.Contains(errMsg, "unauthorized")) {
				rep.SubstepFail(fmt.Errorf("AKS admin credentials failed: %w", err))
				return fmt.Errorf(
					"kubectl cluster-info failed with Azure admin kubeconfig\n"+
						"wasctl uses: az aks get-credentials --admin\n"+
						"  1. Confirm local accounts are enabled on the cluster\n"+
						"  2. Confirm your Azure identity can call get-credentials --admin (Azure Kubernetes Service Cluster Admin Role)\n"+
						"  3. Re-run the kubeconfig stage\n%w",
					err,
				)
			}
			rep.SubstepFail(fmt.Errorf("cluster may still be initialising: %w", err))
			return fmt.Errorf("kubectl cluster-info failed — cluster may still be initialising")
		}
	}
	rep.SubstepDone()

	rep.SubstepStart("Record cluster UID")
	if !cfg.DryRun && w.Meta.ClusterUID == "" {
		uid, err := readKubeSysUID(ctx, kubeconfigPath, contextName)
		if err != nil {
			rep.SubstepFail(err)
			return fmt.Errorf("record cluster UID: %w", err)
		}
		w.Meta.ClusterUID = uid
		rep.LogLine(fmt.Sprintf("[✓] Recorded cluster UID: %s", uid))
	} else if w.Meta.ClusterUID != "" {
		rep.LogLine(fmt.Sprintf("[✓] Cluster UID already recorded: %s", w.Meta.ClusterUID))
	}
	if w.Meta.ClusterARN == "" {
		ensureClusterARN(cfg, w, accountID)
		if w.Meta.ClusterARN != "" {
			rep.LogLine("[✓] Backfilled cluster ARN in workspace")
		}
	}
	rep.SubstepDone()
	return nil
}

func (Kubeconfig) Destroy(_ context.Context, _ *config.Config, _ runner.Runner, _ runner.Reporter) error {
	return nil
}

// readKubeSysUID reads the kube-system namespace UID via the isolated kubeconfig.
func readKubeSysUID(ctx context.Context, kubeconfigPath, contextName string) (string, error) {
	out, err := exec.CommandContext(ctx,
		"kubectl", "--kubeconfig", kubeconfigPath, "--context", contextName,
		"get", "ns", "kube-system", "-o", "jsonpath={.metadata.uid}",
	).Output()
	if err != nil {
		return "", fmt.Errorf("kubectl get ns kube-system: %w", err)
	}
	uid := strings.TrimSpace(string(out))
	if uid == "" {
		return "", fmt.Errorf("kube-system namespace UID is empty")
	}
	return uid, nil
}
