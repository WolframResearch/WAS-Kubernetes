package addons

import "slices"

// AddonsFor returns the ordered list of addons for the given cloud ("aws" or
// "azure"). The order is significant: each addon may depend on earlier ones
// (e.g. was-efs-storageclass requires aws-efs-csi-driver).
func AddonsFor(cloud string) []Addon {
	all := []Addon{
		NewIngressNginx(),
		// AWS: CSI drivers must precede the StorageClasses that use them.
		NewEFSCSIDriver(),
		&EFSStorageClass{},
		NewEBSCSIDriver(),
		&AWSKafkaStorageClass{},
		// Azure: CSI is built into AKS; only the StorageClass is needed.
		&AzureFileStorageClass{},
		&AzureKafkaStorageClass{},
		// Strimzi watches namespaces that are pre-created by HelmComponent.Install.
		NewStrimzi(),
		NewMetricsServer(),
		// Prometheus stack must precede the adapter that scrapes it.
		NewKubePrometheusStack(),
		NewPrometheusAdapter(),
		NewCertManager(),
	}

	var out []Addon
	for _, a := range all {
		if slices.Contains(a.Clouds(), cloud) {
			out = append(out, a)
		}
	}
	return out
}

// NamesFor returns addon names for cloud in install order.
func NamesFor(cloud string) []string {
	list := AddonsFor(cloud)
	names := make([]string, len(list))
	for i, a := range list {
		names[i] = a.Name()
	}
	return names
}
