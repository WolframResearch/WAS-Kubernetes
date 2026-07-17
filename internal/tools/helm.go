package tools

// HelmDepUpdate returns the command to update chart dependencies.
// Always includes --kubeconfig and --kube-context.
func HelmDepUpdate(chartDir, kubeconfigPath, contextName string) []string {
	return []string{
		"helm", "--kubeconfig", kubeconfigPath, "--kube-context", contextName,
		"dependency", "update", chartDir,
	}
}

// HelmUpgradeInstall returns the helm upgrade --install command for the WAS
// chart. kubeconfigPath and contextName are required so the CLI never falls
// back to ~/.kube/config. sets is a map of --set key=value pairs.
func HelmUpgradeInstall(releaseName, chartDir, valuesFile, namespace, kubeconfigPath, contextName string, sets map[string]string) []string {
	cmd := []string{
		"helm", "--kubeconfig", kubeconfigPath, "--kube-context", contextName,
		"upgrade", "--install", releaseName, chartDir,
		"-f", valuesFile,
		"--namespace", namespace, "--create-namespace",
	}
	for k, v := range sets {
		// --set-string avoids Helm type coercion and is safer for values that
		// contain special characters (e.g. Azure storage account keys).
		cmd = append(cmd, "--set-string", k+"="+v)
	}
	return cmd
}

// HelmStatus returns the command to check a release's status.
func HelmStatus(release, namespace, kubeconfigPath, contextName string) []string {
	return []string{
		"helm", "--kubeconfig", kubeconfigPath, "--kube-context", contextName,
		"status", release, "-n", namespace,
	}
}

// HelmUninstall returns the helm uninstall command.
func HelmUninstall(release, namespace, kubeconfigPath, contextName string) []string {
	return []string{
		"helm", "--kubeconfig", kubeconfigPath, "--kube-context", contextName,
		"uninstall", release, "-n", namespace,
	}
}

// HelmList returns the helm list -A -o json command for listing all releases.
func HelmList(kubeconfigPath, contextName string) []string {
	return []string{
		"helm", "--kubeconfig", kubeconfigPath, "--kube-context", contextName,
		"list", "-A", "-o", "json",
	}
}

// HelmHistory returns the helm history command for a release.
func HelmHistory(release, namespace, kubeconfigPath, contextName string) []string {
	return []string{
		"helm", "--kubeconfig", kubeconfigPath, "--kube-context", contextName,
		"history", release, "-n", namespace, "-o", "json",
	}
}
