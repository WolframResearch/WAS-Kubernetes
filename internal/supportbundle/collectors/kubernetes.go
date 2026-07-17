package collectors

import (
	"context"
	"fmt"
)

// KubernetesCollector collects cluster-level Kubernetes information.
type KubernetesCollector struct{}

func (KubernetesCollector) Name() string { return "kubernetes" }

func (KubernetesCollector) Collect(ctx context.Context, cc *CollectContext) ([]File, error) {
	if cc.Kubeconfig == "" {
		return nil, fmt.Errorf("no cluster access (kubeconfig unavailable)")
	}

	kargs := kubectlArgs(cc)
	var files []File

	// cluster-info
	clusterInfo, err := runOutput(ctx, "kubectl", append(kargs, "cluster-info")...)
	if err != nil {
		clusterInfo = []byte(err.Error())
	}
	files = append(files, File{Path: "kubernetes/cluster_info.txt", Content: []byte(redactText(string(clusterInfo)))})

	// nodes
	nodes, err := runOutput(ctx, "kubectl", append(kargs, "get", "nodes", "-o", "json")...)
	if err != nil {
		nodes = []byte(fmt.Sprintf(`{"error": %q}`, err.Error()))
	}
	files = append(files, File{Path: "kubernetes/nodes.json", Content: nodes})

	// namespaces
	ns, err := runOutput(ctx, "kubectl", append(kargs, "get", "namespaces", "-o", "json")...)
	if err != nil {
		ns = []byte(fmt.Sprintf(`{"error": %q}`, err.Error()))
	}
	files = append(files, File{Path: "kubernetes/all_namespaces.json", Content: ns})

	// recent events (last hour)
	events, err := runOutput(ctx, "kubectl", append(kargs,
		"get", "events", "--all-namespaces", "--sort-by=.lastTimestamp", "-o", "json")...)
	if err != nil {
		events = []byte(fmt.Sprintf(`{"error": %q}`, err.Error()))
	}
	files = append(files, File{Path: "kubernetes/events_recent.json", Content: []byte(redactText(string(events)))})

	// CRDs
	crds, err := runOutput(ctx, "kubectl", append(kargs, "get", "crds", "-o", "name")...)
	if err != nil {
		crds = []byte(err.Error())
	}
	files = append(files, File{Path: "kubernetes/crds.txt", Content: crds})

	// API resources
	apiRes, err := runOutput(ctx, "kubectl", append(kargs, "api-resources", "--verbs=list", "--output=name")...)
	if err != nil {
		apiRes = []byte(err.Error())
	}
	files = append(files, File{Path: "kubernetes/api_resources.txt", Content: apiRes})

	return files, nil
}
