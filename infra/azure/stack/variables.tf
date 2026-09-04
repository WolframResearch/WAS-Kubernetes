# =============================================================================
# Provider / subscription
# =============================================================================

variable "subscription_id" {
  type        = string
  description = "Azure subscription ID to deploy into."
}

variable "tenant_id" {
  type        = string
  description = "Azure AD tenant ID. Required for explicit (non-interactive) provider authentication."
}

variable "location" {
  type        = string
  default     = "eastus"
  description = "Azure region for all resources."
}

# =============================================================================
# Cluster
# =============================================================================

variable "cluster_name" {
  type        = string
  default     = "was"
  description = "AKS cluster name. Also used as a prefix/base for related resource names (resource group, storage accounts, managed identity)."
}

variable "cluster_version" {
  type        = string
  default     = "1.36"
  description = "Kubernetes version for the AKS cluster. Verify against `az aks get-versions --location <location> -o table` before applying — AKS retires old minors faster than EKS."
}

variable "api_server_authorized_ip_ranges" {
  type        = list(string)
  default     = []
  description = "CIDR ranges allowed to reach the AKS API server. Empty list = no restriction. Add your office/CI egress CIDR here to harden."
}

# =============================================================================
# Networking
# =============================================================================

variable "vnet_cidr" {
  type        = string
  default     = "10.20.0.0/16"
  description = "CIDR block for the VNet. Deliberately different from infra/aws/stack's 10.168.0.0/16 to avoid any future cross-cloud confusion if both are ever peered."
}

variable "aks_subnet_cidr" {
  type        = string
  default     = "10.20.0.0/20"
  description = "CIDR block for the AKS node subnet (4096 IPs)."
}

variable "pod_cidr" {
  type        = string
  default     = "192.168.0.0/16"
  description = "Pod CIDR for Azure CNI Overlay (network_plugin_mode = \"overlay\"). Independent of the VNet address space by design — pods never consume VNet IPs under overlay mode. Must not overlap with vnet_cidr or any peered network."
}

# =============================================================================
# Node pool
# =============================================================================

variable "node_vm_size" {
  type        = string
  default     = "Standard_D4s_v5"
  description = "VM size for the default node pool. Modern equivalent of the AWS stack's c5.2xlarge."
}

variable "node_disk_size_gb" {
  type        = number
  default     = 50
  description = "OS disk size (GB) for each worker node."
}

variable "node_min_count" {
  type        = number
  default     = 2
  description = "Minimum worker node count (cluster autoscaler floor)."
}

variable "node_desired_count" {
  type        = number
  default     = 2
  description = "Initial worker node count at cluster creation. The cluster autoscaler owns node count after that — see aks.tf's lifecycle.ignore_changes."
}

variable "node_max_count" {
  type        = number
  default     = 10
  description = "Maximum worker node count (cluster autoscaler ceiling; HPA ceiling)."
}

# =============================================================================
# Azure Files (RWX log PVCs)
# =============================================================================

variable "filesystem_storage_account_name" {
  type        = string
  default     = null
  description = "Name for the Azure Files storage account. Leave null to auto-generate a globally-unique name from cluster_name plus a random suffix — you do not need to invent one."
}

variable "filesystem_quota_gb" {
  type        = number
  default     = 100
  description = "Quota (GB) for the was-files Azure Files share."
}

variable "filesystem_tier" {
  type        = string
  default     = "Premium"
  description = "Storage account tier for Azure Files. \"Premium\" (account_kind=FileStorage) matches EFS's bursting-throughput behavior most closely but costs more; \"Standard\" (account_kind=StorageV2) is cheaper with lower baseline throughput."

  validation {
    condition     = contains(["Premium", "Standard"], var.filesystem_tier)
    error_message = "filesystem_tier must be \"Premium\" or \"Standard\"."
  }
}

# =============================================================================
# Blob storage (resource-manager artifacts)  →  Helm chart: objectStorage.*
# =============================================================================

variable "blob_storage_account_name" {
  type        = string
  default     = null
  description = "Name for the resource-manager blob storage account. Leave null to auto-generate a globally-unique name from cluster_name plus a random suffix — you do not need to invent one. Helm chart: --set objectStorage.azure.accountName=<value>"
}

variable "blob_replication_type" {
  type        = string
  default     = "LRS"
  description = "Replication type for the blob storage account. \"LRS\" (default, cheapest, single-region) or \"GRS\" (geo-redundant, for regional-failure tolerance — costs more and only protects against a regional outage, not accidental deletion)."
}

variable "resource_container_name" {
  type        = string
  default     = "resources"
  description = "Name of the blob container for resource-manager artifacts. Helm chart: --set objectStorage.resourceBucket=<value>"
}

variable "nodefile_container_name" {
  type        = string
  default     = "nodefiles"
  description = "Name of the blob container for nodefile-manager artifacts. Helm chart: --set objectStorage.nodefileBucket=<value>"
}

# =============================================================================
# Workload Identity — resource-manager ServiceAccount
# =============================================================================

variable "resource_manager_service_account_name" {
  type        = string
  default     = "was-resource-manager"
  description = "Kubernetes ServiceAccount name for the resource-manager pod. Must match the Helm chart value resourceManager.serviceAccount.name."
}

variable "resource_manager_service_account_namespace" {
  type        = string
  default     = "was"
  description = "Kubernetes namespace for the resource-manager ServiceAccount. Must match the Helm chart value namespace.name."
}

# =============================================================================
# DNS (optional)
# =============================================================================

variable "create_dns_record" {
  type        = bool
  default     = false
  description = "Create an Azure DNS A record pointing dns_record_name at ingress_ip. Requires dns_zone_name, dns_zone_resource_group, dns_record_name, and ingress_ip. Default off — most deployments use the free *.cloudapp.azure.com DNS label on the ingress-nginx load balancer instead; this resource is for customers who want a record in their own zone."
}

variable "dns_zone_name" {
  type        = string
  default     = ""
  description = "Existing Azure DNS zone name (e.g. example.com). Required when create_dns_record = true. This stack does not create the zone."
}

variable "dns_zone_resource_group" {
  type        = string
  default     = ""
  description = "Resource group containing the Azure DNS zone above. Required when create_dns_record = true."
}

variable "dns_record_name" {
  type        = string
  default     = ""
  description = "Fully-qualified DNS name to create (e.g. was.example.com). Required when create_dns_record = true."
}

variable "ingress_ip" {
  type        = string
  default     = ""
  description = "Public IP address of the ingress-nginx load balancer. Required when create_dns_record = true. Unlike AWS's ELB (which exposes a DNS hostname, hence Route53 CNAME), Azure's Load Balancer exposes a static IP, hence an A record here rather than a CNAME. Obtain after installing ingress-nginx: kubectl get svc -n ingress-nginx ingress-nginx-controller -o jsonpath='{.status.loadBalancer.ingress[0].ip}'"
}

# =============================================================================
# Tags
# =============================================================================

variable "tags" {
  type        = map(string)
  default     = {}
  description = "Additional tags to merge onto all resources. Merged with stable defaults (Application, ManagedBy, Cluster)."
}
