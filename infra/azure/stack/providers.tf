provider "azurerm" {
  subscription_id = var.subscription_id
  tenant_id       = var.tenant_id
  features {}
}

# Unused today — no kubernetes_* resource exists in this stack, mirroring
# infra/aws/stack/providers.tf's kubernetes provider (also pre-wired but
# currently unused). Kept for parity / future use.
#
# Uses the AKS admin/break-glass kubeconfig (client-certificate auth, always
# available unless local accounts are disabled) rather than an AAD-token
# exec plugin, so this stack has no dependency on the separate `kubelogin`
# binary. If a real kubernetes_* resource is ever added here, reconsider:
# the admin kubeconfig bypasses the azure_rbac_enabled Azure AD RBAC this
# cluster is configured with (aks.tf), which is fine for Terraform-applied
# bootstrap resources but worth a second look for anything more sensitive.
provider "kubernetes" {
  host                   = azurerm_kubernetes_cluster.this.kube_admin_config[0].host
  client_certificate     = base64decode(azurerm_kubernetes_cluster.this.kube_admin_config[0].client_certificate)
  client_key             = base64decode(azurerm_kubernetes_cluster.this.kube_admin_config[0].client_key)
  cluster_ca_certificate = base64decode(azurerm_kubernetes_cluster.this.kube_admin_config[0].cluster_ca_certificate)
}

provider "random" {}
