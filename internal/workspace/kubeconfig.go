package workspace

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Kubeconfig generates an isolated kubeconfig for the cluster.
//
// For AWS, it runs:
//
//	aws eks update-kubeconfig --kubeconfig <tempfile> --name <cluster> --region <region>
//
// For Azure, it runs:
//
//	az aks get-credentials --admin --resource-group <rg> --name <cluster> \
//	  --file <tempfile> --overwrite-existing
//
// --admin fetches the local cluster-admin client certificate (break-glass),
// which bypasses Azure AD / Entra ID Kubernetes RBAC. The context is named
// "<aksName>-admin". wasctl needs full cluster access for install and destroy
// (Helm secrets, PVCs, add-ons). Local accounts remain enabled on the wasctl
// AKS stack (default).
//
// The kubeconfig lives in the workspace's temp directory and is removed when
// Close() is called. It is NEVER written to ~/.kube/config.
//
// Returns the absolute path to the kubeconfig file.
func (w *Workspace) Kubeconfig(ctx context.Context) (string, error) {
	if w.tempDir == "" {
		return "", fmt.Errorf("workspace not materialised; call Materialize before Kubeconfig")
	}

	kubeconfigPath := filepath.Join(w.tempDir, "kubeconfig")
	if err := guardNotHomeKube(kubeconfigPath); err != nil {
		panic(err) // defensive: should never happen; panic to catch regressions
	}

	// Cache: reuse non-empty kubeconfig. For Azure, reject stale AAD/user
	// kubeconfigs (exec/kubelogin) so we always operate as cluster admin.
	if info, err := os.Stat(kubeconfigPath); err == nil && info.Size() > 0 {
		if w.Meta.Cloud == "azure" {
			if data, readErr := os.ReadFile(kubeconfigPath); readErr == nil {
				s := string(data)
				if strings.Contains(s, "kubelogin") || strings.Contains(s, "exec:") {
					_ = os.Remove(kubeconfigPath)
				} else {
					return kubeconfigPath, nil
				}
			}
		} else {
			return kubeconfigPath, nil
		}
	}

	if w.Meta.Cloud == "azure" {
		return w.kubeconfigAzure(ctx, kubeconfigPath)
	}
	return w.kubeconfigAWS(ctx, kubeconfigPath)
}

func (w *Workspace) kubeconfigAWS(ctx context.Context, kubeconfigPath string) (string, error) {
	clusterName := w.Meta.ClusterName
	region := w.Meta.AWSRegion

	cmd := exec.CommandContext(ctx,
		"aws", "eks", "update-kubeconfig",
		"--kubeconfig", kubeconfigPath,
		"--name", clusterName,
		"--region", region,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf(
			"aws eks update-kubeconfig for cluster %s: %w\n%s",
			clusterName, err, strings.TrimSpace(string(out)),
		)
	}
	if err := os.Chmod(kubeconfigPath, 0600); err != nil {
		return "", fmt.Errorf("chmod kubeconfig: %w", err)
	}
	return kubeconfigPath, nil
}

func (w *Workspace) kubeconfigAzure(ctx context.Context, kubeconfigPath string) (string, error) {
	// Logical wasctl cluster name (e.g. "wasctl") ≠ AKS ARM name ("wasctl-aks").
	// Bootstrap historically stored the meta RG (wolfram-*-meta) in AzureResourceGroup;
	// AKS lives in the stack RG ("wasctl-rg"). Always derive from Terraform naming.
	logical := w.Meta.ClusterName
	aksName := AzureAKSName(logical)
	rg := AzureStackResourceGroup(logical)
	if w.Meta.AzureResourceGroup != "" && w.Meta.AzureResourceGroup != rg {
		// Correct stale meta-RG (or other) values so later stages persist the right RG.
		w.Meta.AzureResourceGroup = rg
	} else if w.Meta.AzureResourceGroup == "" {
		w.Meta.AzureResourceGroup = rg
	}

	// --admin: local cluster-admin cert (bypasses Azure AD Kubernetes RBAC).
	// Context is always named "<aksName>-admin" (az ignores --context with --admin
	// when aks-preview is installed; see ContextName).
	cmd := exec.CommandContext(ctx,
		"az", "aks", "get-credentials",
		"--admin",
		"--resource-group", rg,
		"--name", aksName,
		"--file", kubeconfigPath,
		"--overwrite-existing",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(out))
		hint := ""
		if strings.Contains(strings.ToLower(msg+err.Error()), "local accounts") ||
			strings.Contains(strings.ToLower(msg+err.Error()), "disabled") {
			hint = "\nHint: AKS local accounts appear disabled; wasctl requires --admin credentials.\n" +
				"Re-enable with: az aks update -g " + rg + " -n " + aksName + " --enable-local-accounts"
		}
		return "", fmt.Errorf(
			"az aks get-credentials --admin for cluster %s (rg=%s): %w\n%s%s",
			aksName, rg, err, msg, hint,
		)
	}
	if err := os.Chmod(kubeconfigPath, 0600); err != nil {
		return "", fmt.Errorf("chmod kubeconfig: %w", err)
	}
	return kubeconfigPath, nil
}

// ContextName returns the kubectl context name for this cluster.
//
// AWS: EKS update-kubeconfig creates a context named after the cluster ARN.
// Azure: az aks get-credentials --admin creates "<aksName>-admin".
func (w *Workspace) ContextName() string {
	if w.Meta.Cloud == "azure" {
		return AzureAKSName(w.Meta.ClusterName) + "-admin"
	}
	return ClusterARN(w.Meta.AWSRegion, w.Meta.AWSAccountID, w.Meta.ClusterName)
}

// guardNotHomeKube returns an error if path resolves under ~/.kube.
// wasctl must never point customer kubeconfig at the home kube directory.
func guardNotHomeKube(path string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil // can't check; allow
	}
	kubeDir := filepath.Join(home, ".kube")
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil
	}
	if strings.HasPrefix(absPath, kubeDir+string(filepath.Separator)) || absPath == kubeDir {
		return fmt.Errorf("BUG: kubeconfig path %q resolves to ~/.kube — customer config must never be written", absPath)
	}
	return nil
}
