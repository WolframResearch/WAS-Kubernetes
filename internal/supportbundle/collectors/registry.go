package collectors

// All returns all collectors in the order they appear in the bundle.
// Collectors that skip when their preconditions aren't met (e.g., no kubeconfig)
// return an error from Collect, which the bundle builder records as "skipped".
func All() []Collector {
	return []Collector{
		SystemCollector{},
		CLIVersionsCollector{},
		AWSCollector{},
		AzureCollector{},
		WorkspaceCollector{},
		DoctorCollector{},
		HelmCollector{},
		KubernetesCollector{},
		WorkloadsCollector{},
		LogsCollector{},
		KafkaCollector{},
		StorageCollector{},
		NetworkingCollector{},
	}
}
