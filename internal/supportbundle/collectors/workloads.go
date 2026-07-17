package collectors

import (
	"context"
	"fmt"
)

const wasNamespace = "was"

// WorkloadsCollector collects Kubernetes workload objects from the WAS namespace.
type WorkloadsCollector struct{}

func (WorkloadsCollector) Name() string { return "workloads" }

func (WorkloadsCollector) Collect(ctx context.Context, cc *CollectContext) ([]File, error) {
	if cc.Kubeconfig == "" {
		return nil, fmt.Errorf("no cluster access (kubeconfig unavailable)")
	}

	kargs := kubectlArgs(cc)
	ns := []string{"-n", wasNamespace}
	var files []File

	resources := []struct {
		kind string
		path string
	}{
		{"deployments", "workloads/deployments.json"},
		{"pods", "workloads/pods.json"},
		{"services", "workloads/services.json"},
		{"ingresses", "workloads/ingresses.json"},
		{"horizontalpodautoscalers", "workloads/hpas.json"},
		{"persistentvolumeclaims", "workloads/pvcs.json"},
	}

	for _, r := range resources {
		out, err := runOutput(ctx, "kubectl",
			append(kargs, append([]string{"get", r.kind, "-o", "json"}, ns...)...)...)
		if err != nil {
			out = []byte(fmt.Sprintf(`{"error": %q}`, err.Error()))
		}
		files = append(files, File{Path: r.path, Content: out})
	}

	return files, nil
}
