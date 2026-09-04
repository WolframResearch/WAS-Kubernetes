# =============================================================================
# AKS cluster
# =============================================================================

output "cluster_name" {
  description = "AKS cluster name."
  value       = azurerm_kubernetes_cluster.this.name
}

output "cluster_id" {
  description = "AKS cluster resource ID."
  value       = azurerm_kubernetes_cluster.this.id
}

output "cluster_oidc_issuer_url" {
  description = "OIDC issuer URL for the AKS cluster. Used to create additional Workload Identity federated credentials outside this stack."
  value       = azurerm_kubernetes_cluster.this.oidc_issuer_url
}

output "kubeconfig_command" {
  description = "Run this command to update your local kubeconfig."
  value       = "az aks get-credentials --admin --resource-group ${azurerm_resource_group.this.name} --name ${azurerm_kubernetes_cluster.this.name}"
}

# =============================================================================
# Azure Files  →  Helm chart: storage.className points at a StorageClass
# that wasctl wires to this account (file.csi.azure.com + account-key Secret).
# =============================================================================

output "filesystem_name" {
  description = "Azure Files share name."
  value       = azurerm_storage_share.was_files.name
}

output "filesystem_storage_account_name" {
  description = "Storage account name backing the Azure Files share."
  value       = azurerm_storage_account.filesystem.name
}

output "filesystem_storage_account_key" {
  description = "Primary access key for the Azure Files storage account. The Files CSI driver needs this to mount the share."
  value       = azurerm_storage_account.filesystem.primary_access_key
  sensitive   = true
}

output "filesystem_sku_name" {
  description = "SKU name for was-azurefile StorageClass parameters.skuName (Premium_LRS or Standard_LRS)."
  value       = var.filesystem_tier == "Premium" ? "Premium_LRS" : "Standard_LRS"
}

output "resource_group_name" {
  description = "Stack resource group name (hosts AKS, Azure Files, and blob storage accounts)."
  value       = azurerm_resource_group.this.name
}

# =============================================================================
# Blob storage  →  Helm chart: objectStorage.*
# =============================================================================

output "resource_storage_account_name" {
  description = "Blob storage account name. Helm chart: --set objectStorage.azure.accountName=<value>"
  value       = azurerm_storage_account.app_data.name
}

output "resource_storage_account_key" {
  description = "Primary access key for the blob storage account. Default wasctl Azure installs use objectStorage.auth.mode=static with this key (Contributor can listKeys; no role-assignment permission required)."
  value       = azurerm_storage_account.app_data.primary_access_key
  sensitive   = true
}

output "resource_container_name" {
  description = "Resource-info blob container name. Helm chart: --set objectStorage.resourceBucket=<value>"
  value       = azurerm_storage_container.resource.name
}

output "nodefile_storage_account_name" {
  description = "Blob storage account name (same account as resource_storage_account_name — both containers live in one storage account, see storage.tf)."
  value       = azurerm_storage_account.app_data.name
}

output "nodefile_container_name" {
  description = "Nodefiles blob container name. Helm chart: --set objectStorage.nodefileBucket=<value>"
  value       = azurerm_storage_container.nodefile.name
}

# =============================================================================
# Workload Identity  →  Helm chart: resourceManager.serviceAccount.azureClientId
# =============================================================================

output "resource_manager_identity_client_id" {
  description = "Client ID of the resource-manager user-assigned managed identity. Helm chart: --set resourceManager.serviceAccount.azureClientId=<value>"
  value       = azurerm_user_assigned_identity.resource_manager.client_id
}

output "resource_manager_identity_object_id" {
  description = "Object (principal) ID of the resource-manager user-assigned managed identity. Reference only — not consumed by the Helm chart."
  value       = azurerm_user_assigned_identity.resource_manager.principal_id
}

# =============================================================================
# Convenience
# =============================================================================

output "helm_install_command_hint" {
  description = "Suggested helm install command populated with all Terraform outputs. Replace <your-dns-name> with your ingress hostname. Default auth is static (account key). Workload Identity is optional and requires a Storage Blob Data Contributor role assignment on the UAMI (Owner/UAA)."
  value       = <<-EOT
    helm install was charts/wolfram-application-server \
      -f charts/wolfram-application-server/values-azure.yaml \
      --namespace was --create-namespace \
      --set ingress.host=<your-dns-name> \
      --set objectStorage.azure.accountName=${azurerm_storage_account.app_data.name} \
      --set objectStorage.resourceBucket=${azurerm_storage_container.resource.name} \
      --set objectStorage.nodefileBucket=${azurerm_storage_container.nodefile.name} \
      --set objectStorage.auth.mode=static \
      --set objectStorage.auth.secretKey=<from terraform output -raw resource_storage_account_key>
  EOT
}
