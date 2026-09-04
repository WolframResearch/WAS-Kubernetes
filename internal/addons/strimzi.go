package addons

import "strings"

// NewStrimzi returns the Strimzi Kafka operator add-on.
//
// WatchedNS and the watchNamespaces Helm value are derived from the same
// source (watchedNS slice) so they cannot drift.
//
// All namespaces in WatchedNS (plus the release namespace strimzi-system) are
// pre-created by HelmComponent.Install before the operator starts, satisfying
// Strimzi's requirement that watched namespaces exist at startup.
func NewStrimzi() *HelmComponent {
	watchedNS := []string{"was", "kafka"}
	return &HelmComponent{
		ReleaseName: "strimzi-kafka-operator",
		ChartRef:    "strimzi/strimzi-kafka-operator",
		// >= 1.1.0 required for Kubernetes 1.35+ compatibility.
		// See docs/Troubleshooting.md#14-strimzi-cluster-operator-crashloopbackoff-on-k8s-135
		Version:   "~1.1.0",
		Namespace: "strimzi-system",
		WatchedNS: watchedNS,
		RepoName:  "strimzi",
		RepoURL:   "https://strimzi.io/charts/",
		Values: map[string]string{
			// Derived from WatchedNS so value and namespace list stay in sync.
			"watchNamespaces": "{" + strings.Join(watchedNS, ",") + "}",
		},
		clouds: []string{"aws", "azure"},
		// Extend timeout to 15m to handle slow image downloads or resource allocation.
		Timeout: "15m",
		// Disable atomic rollback so failed pods stay in the namespace for troubleshooting.
		DisableAtomic: true,
		// Derived from: helm template strimzi/strimzi-kafka-operator --version ~1.1.0
		// grep -E '^kind:|^  name:' | paste - -
		OrphanedResourceCheck: []OrphanedResource{
			// Cluster-scoped RBAC
			{Kind: "ClusterRoleBinding", Name: "strimzi-cluster-operator"},
			{Kind: "ClusterRoleBinding", Name: "strimzi-cluster-operator-kafka-broker-delegation"},
			{Kind: "ClusterRoleBinding", Name: "strimzi-cluster-operator-kafka-client-delegation"},
			{Kind: "ClusterRoleBinding", Name: "strimzi-cluster-operator-watched"},
			{Kind: "ClusterRoleBinding", Name: "strimzi-cluster-operator-global"},
			{Kind: "ClusterRole", Name: "strimzi-cluster-operator-namespaced"},
			{Kind: "ClusterRole", Name: "strimzi-cluster-operator-global"},
			{Kind: "ClusterRole", Name: "strimzi-kafka-broker"},
			{Kind: "ClusterRole", Name: "strimzi-kafka-client"},
			{Kind: "ClusterRole", Name: "strimzi-entity-operator"},
			{Kind: "ClusterRole", Name: "strimzi-kafka-broker-init"},
			{Kind: "ClusterRole", Name: "strimzi-cluster-operator-leader-election"},
			// Namespaced RBAC — checked in strimzi-system, was, and kafka
			{Kind: "RoleBinding", Name: "strimzi-cluster-operator", Namespace: "@watched"},
			{Kind: "RoleBinding", Name: "strimzi-cluster-operator-watched", Namespace: "@watched"},
			{Kind: "RoleBinding", Name: "strimzi-cluster-operator-entity-operator-delegation", Namespace: "@watched"},
			{Kind: "RoleBinding", Name: "strimzi-cluster-operator-leader-election", Namespace: "@watched"},
			// Operator workload
			{Kind: "ServiceAccount", Name: "strimzi-cluster-operator", Namespace: "strimzi-system"},
			{Kind: "Deployment", Name: "strimzi-cluster-operator", Namespace: "strimzi-system"},
		},
	}
}
