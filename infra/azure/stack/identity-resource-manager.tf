# Workload Identity for the resource-manager pod — the Azure equivalent of
# AWS IRSA (infra/aws/stack/iam-resource-manager.tf).
#
# Default wasctl Azure installs use objectStorage.auth.mode=static (storage
# account key) so Contributor-only principals can complete infra without
# Microsoft.Authorization/roleAssignments/write.
#
# The UAMI + federated credential below are still created so operators who
# have Owner/UAA (or an out-of-band Storage Blob Data Contributor assignment
# on this identity) can opt into workloadIdentity later without re-running
# the identity portion of the stack.

resource "azurerm_user_assigned_identity" "resource_manager" {
  name                = "${local.cluster_name}-rm-id"
  resource_group_name = azurerm_resource_group.this.name
  location            = azurerm_resource_group.this.location
  tags                = local.tags
}

resource "azurerm_federated_identity_credential" "resource_manager" {
  name = "${local.cluster_name}-rm-fed"

  # user_assigned_identity_id is the modern attribute; parent_id and
  # resource_group_name are deprecated on this resource as of the pinned
  # azurerm provider version (confirmed against the 4.79.0 schema).
  user_assigned_identity_id = azurerm_user_assigned_identity.resource_manager.id

  audience = ["api://AzureADTokenExchange"]
  issuer   = azurerm_kubernetes_cluster.this.oidc_issuer_url
  subject  = "system:serviceaccount:${var.resource_manager_service_account_namespace}:${var.resource_manager_service_account_name}"
}
