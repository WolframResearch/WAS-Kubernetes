package metacontainer

import "strings"

// ContainerName is the fixed blob container name within every wasctl meta storage account.
const ContainerName = "wasctl-meta"

// MetaResourceGroup returns the Azure resource group name used to hold the wasctl
// meta storage account for a cluster. Format: "wolfram-<clusterName>-meta".
func MetaResourceGroup(clusterName string) string {
	return "wolfram-" + clusterName + "-meta"
}

// StorageAccountName derives the meta storage account name for an Azure subscription
// and cluster. The account is per-subscription but namespaced by cluster name to
// avoid collisions.
// Format: "wolfram" + first 8 chars of cluster name (lowercase alphanum) + first 8 hex of sub ID = ≤24 chars.
//
// Example: cluster "mycluster", sub "12345678-abcd-efef-1234-abcdef012345" → "wolframmycluste12345678"
func StorageAccountName(subscriptionID, clusterName string) string {
	// Sanitize cluster name: lowercase, only alphanumeric.
	var sb strings.Builder
	for _, r := range strings.ToLower(clusterName) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			sb.WriteRune(r)
		}
	}
	sanitized := sb.String()
	if len(sanitized) > 8 {
		sanitized = sanitized[:8]
	}
	hex := strings.ReplaceAll(subscriptionID, "-", "")
	if len(hex) > 8 {
		hex = hex[:8]
	}
	return "wolfram" + sanitized + hex
}

// ClusterKeyPrefix returns the blob key prefix for a cluster's workspace blobs.
func ClusterKeyPrefix(clusterName string) string {
	return "clusters/" + clusterName + "/"
}

// BootstrapStateKey returns the blob key for the bootstrap terraform.tfstate.
func BootstrapStateKey(clusterName string) string {
	return ClusterKeyPrefix(clusterName) + "bootstrap-state/terraform.tfstate"
}

// BootstrapStateBackupKey returns the blob key for the bootstrap state backup.
func BootstrapStateBackupKey(clusterName string) string {
	return ClusterKeyPrefix(clusterName) + "bootstrap-state/terraform.tfstate.backup"
}

// BackendTFVarsKey returns the blob key for the stack backend.tfvars.
// Azure uses .tfvars (not .hcl as AWS does) to supply backend config to azurerm.
func BackendTFVarsKey(clusterName string) string {
	return ClusterKeyPrefix(clusterName) + "backend.tfvars"
}

// WorkspaceMetaKey returns the blob key for workspace.json.
func WorkspaceMetaKey(clusterName string) string {
	return ClusterKeyPrefix(clusterName) + "workspace.json"
}

// LockBlobKey returns the blob key used as the lease sentinel for per-cluster locking.
func LockBlobKey(clusterName string) string {
	return "locks/" + clusterName
}
