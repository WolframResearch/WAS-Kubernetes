resource "azurerm_resource_group" "this" {
  name     = "${local.cluster_name}-rg"
  location = var.location
  tags     = local.tags
}

resource "azurerm_kubernetes_cluster" "this" {
  name                = "${local.cluster_name}-aks"
  location            = azurerm_resource_group.this.location
  resource_group_name = azurerm_resource_group.this.name
  dns_prefix          = "${local.cluster_name}-k8s"
  kubernetes_version  = var.cluster_version

  # Required for Workload Identity (OIDC issuer + workload identity webhook).
  oidc_issuer_enabled       = true
  workload_identity_enabled = true

  identity {
    type = "SystemAssigned"
  }

  azure_active_directory_role_based_access_control {
    azure_rbac_enabled = true
    # The provider requires one of tenant_id / admin_group_object_ids to be
    # set explicitly even though both are individually optional in the
    # schema. Defaulting to the subscription's own tenant — Azure AD RBAC
    # then applies to every AAD principal in this tenant with an assigned
    # role, no separate admin group required.
    tenant_id = var.tenant_id
  }

  default_node_pool {
    name                 = "workernodes"
    vm_size              = var.node_vm_size
    os_disk_size_gb      = var.node_disk_size_gb
    vnet_subnet_id       = azurerm_subnet.aks.id
    auto_scaling_enabled = true
    min_count            = var.node_min_count
    max_count            = var.node_max_count
    node_count           = var.node_desired_count
  }

  network_profile {
    network_plugin      = "azure"
    network_plugin_mode = "overlay"
    network_policy      = "calico"
    pod_cidr            = var.pod_cidr
  }

  api_server_access_profile {
    authorized_ip_ranges = var.api_server_authorized_ip_ranges
  }

  tags = local.tags

  lifecycle {
    # node_count is the *initial* size; the in-cluster autoscaler (enabled
    # via auto_scaling_enabled above) owns it after that. Without this,
    # every `terraform plan` would show a diff fighting the autoscaler's
    # last-observed count.
    ignore_changes = [default_node_pool[0].node_count]
  }
}
