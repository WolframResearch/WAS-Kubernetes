package addons

// NewKubePrometheusStack returns the kube-prometheus-stack add-on.
// Required by prometheus-adapter (see below); must be installed first.
func NewKubePrometheusStack() *HelmComponent {
	return &HelmComponent{
		ReleaseName: "kube-prometheus-stack",
		ChartRef:    "prometheus-community/kube-prometheus-stack",
		Version:     "~65.0.0",
		Namespace:   "monitoring",
		RepoName:    "prometheus-community",
		RepoURL:     "https://prometheus-community.github.io/helm-charts",
		clouds:      []string{"aws", "azure"},
		// Derived from: helm template prometheus-community/kube-prometheus-stack --version ~65.0.0
		// Names follow {{ template "kube-prometheus-stack.fullname" . }} = "kube-prometheus-stack"
		// when release name matches chart name (no nameOverride). Extend this list by running the
		// helm template command and grepping for "kind: ClusterRole".
		OrphanedResourceCheck: []OrphanedResource{
			{Kind: "ClusterRole", Name: "kube-prometheus-stack-operator"},
			{Kind: "ClusterRole", Name: "kube-prometheus-stack-alertmanager"},
			{Kind: "ClusterRole", Name: "kube-prometheus-stack-grafana-clusterrole"},
			{Kind: "ClusterRole", Name: "kube-prometheus-stack-kube-state-metrics"},
			{Kind: "ClusterRole", Name: "kube-prometheus-stack-prometheus"},
			{Kind: "ClusterRoleBinding", Name: "kube-prometheus-stack-operator"},
			{Kind: "ClusterRoleBinding", Name: "kube-prometheus-stack-alertmanager"},
			{Kind: "ClusterRoleBinding", Name: "kube-prometheus-stack-grafana-clusterrolebinding"},
			{Kind: "ClusterRoleBinding", Name: "kube-prometheus-stack-kube-state-metrics"},
			{Kind: "ClusterRoleBinding", Name: "kube-prometheus-stack-prometheus"},
		},
	}
}

// NewPrometheusAdapter returns the prometheus-adapter add-on.
// Provides custom metrics API required by WAS HPAs; must follow
// kube-prometheus-stack in the install order.
func NewPrometheusAdapter() *HelmComponent {
	return &HelmComponent{
		ReleaseName: "prometheus-adapter",
		ChartRef:    "prometheus-community/prometheus-adapter",
		Version:     "~4.11.0",
		Namespace:   "monitoring",
		// prometheus-community repo was already added by kube-prometheus-stack;
		// --force-update in HelmComponent.Install makes this a no-op.
		RepoName: "prometheus-community",
		RepoURL:  "https://prometheus-community.github.io/helm-charts",
		Values: map[string]string{
			"prometheus.url":  "http://kube-prometheus-stack-prometheus.monitoring.svc",
			"prometheus.port": "9090",
		},
		clouds: []string{"aws", "azure"},
		// Derived from: helm template prometheus-community/prometheus-adapter --version ~4.11.0
		OrphanedResourceCheck: []OrphanedResource{
			{Kind: "ClusterRole", Name: "prometheus-adapter"},
			{Kind: "ClusterRole", Name: "prometheus-adapter:system:auth-delegator"},
			{Kind: "ClusterRole", Name: "prometheus-adapter-resource-reader"},
			{Kind: "ClusterRole", Name: "prometheus-adapter-server-resources"},
			{Kind: "ClusterRoleBinding", Name: "prometheus-adapter"},
			{Kind: "ClusterRoleBinding", Name: "prometheus-adapter:system:auth-delegator"},
			{Kind: "ClusterRoleBinding", Name: "hpa-controller-prometheus-adapter"},
			{Kind: "ClusterRoleBinding", Name: "prometheus-adapter-resource-reader"},
		},
	}
}
