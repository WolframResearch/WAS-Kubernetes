package addons

import (
	"context"
	"fmt"
	"strings"
)

// NewMetricsServer returns the metrics-server add-on.
//
// AKS (and some EKS add-on profiles) already ship metrics-server under
// addon-manager ownership. In that case Helm cannot adopt the existing
// ServiceAccount/Deployment, so we detect the in-cluster install and skip.
func NewMetricsServer() *metricsServer {
	return &metricsServer{}
}

type metricsServer struct{}

func (m *metricsServer) Name() string         { return "metrics-server" }
func (m *metricsServer) Clouds() []string     { return []string{"aws", "azure"} }
func (m *metricsServer) Namespaces() []string { return []string{"kube-system"} }

func (m *metricsServer) Check(ctx context.Context, rc *RunContext) (State, error) {
	if ok, _ := metricsServerAlreadyPresent(ctx, rc); ok {
		return StateHealthy, nil
	}
	return m.component().Check(ctx, rc)
}

func (m *metricsServer) Verify(ctx context.Context, rc *RunContext) error {
	if ok, _ := metricsServerAlreadyPresent(ctx, rc); ok {
		return nil
	}
	return m.component().Verify(ctx, rc)
}

func (m *metricsServer) Uninstall(ctx context.Context, rc *RunContext) error {
	// Never uninstall a cluster-addon metrics-server — only our helm release.
	if ok, reason := metricsServerAlreadyPresent(ctx, rc); ok && strings.Contains(reason, "addon") {
		rc.Reporter.LogLine("[addons] metrics-server: cluster addon present — leaving in place")
		return nil
	}
	return m.component().Uninstall(ctx, rc)
}

func (m *metricsServer) Install(ctx context.Context, rc *RunContext) error {
	if ok, reason := metricsServerAlreadyPresent(ctx, rc); ok {
		rc.Reporter.LogLine(fmt.Sprintf("[addons] metrics-server: %s — skipping helm install", reason))
		return nil
	}
	return m.component().Install(ctx, rc)
}

func (m *metricsServer) component() *HelmComponent {
	return &HelmComponent{
		ReleaseName: "metrics-server",
		ChartRef:    "metrics-server/metrics-server",
		Version:     "~3.12.0",
		Namespace:   "kube-system",
		RepoName:    "metrics-server",
		RepoURL:     "https://kubernetes-sigs.github.io/metrics-server/",
		clouds:      []string{"aws", "azure"},
		// Derived from: helm template metrics-server/metrics-server --version ~3.12.0
		OrphanedResourceCheck: []OrphanedResource{
			{Kind: "ClusterRole", Name: "system:metrics-server"},
			{Kind: "ClusterRoleBinding", Name: "system:metrics-server"},
			{Kind: "ClusterRoleBinding", Name: "metrics-server:system:auth-delegator"},
			{Kind: "RoleBinding", Name: "metrics-server-auth-reader", Namespace: "kube-system"},
			{Kind: "ServiceAccount", Name: "metrics-server", Namespace: "kube-system"},
			{Kind: "Service", Name: "metrics-server", Namespace: "kube-system"},
			{Kind: "Deployment", Name: "metrics-server", Namespace: "kube-system"},
			{Kind: "APIService", Name: "v1beta1.metrics.k8s.io"},
		},
	}
}

// metricsServerAlreadyPresent reports whether metrics-server is usable without
// a wasctl helm release (AKS addon, prior kubectl apply, etc.).
func metricsServerAlreadyPresent(ctx context.Context, rc *RunContext) (bool, string) {
	out, err := rc.Runner.Output(ctx, append([]string{
		"kubectl", "get", "deploy", "metrics-server", "-n", "kube-system",
		"-o", "jsonpath={.metadata.labels.addonmanager\\.kubernetes\\.io/mode}",
	}, rc.kubectlFlags()...), nil)
	if err == nil {
		mode := strings.TrimSpace(string(out))
		if mode != "" {
			return true, "already present as cluster addon (addonmanager mode=" + mode + ")"
		}
	}

	// Ready deployment without helm release (e.g. leftover from a prior install path).
	ready, err := rc.Runner.Output(ctx, append([]string{
		"kubectl", "get", "deploy", "metrics-server", "-n", "kube-system",
		"-o", "jsonpath={.status.readyReplicas}",
	}, rc.kubectlFlags()...), nil)
	if err != nil {
		return false, ""
	}
	if strings.TrimSpace(string(ready)) != "" && strings.TrimSpace(string(ready)) != "0" {
		// Confirm helm release is absent so we don't skip our own healthy release path incorrectly.
		_, helmErr := rc.Runner.Output(ctx, append([]string{
			"helm", "status", "metrics-server", "--namespace", "kube-system", "-o", "json",
		}, rc.helmFlags()...), nil)
		if helmErr != nil {
			return true, "deployment already ready in kube-system (no helm release)"
		}
	}
	return false, ""
}
