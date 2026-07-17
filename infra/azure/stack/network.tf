# Written directly rather than via the AVM network module (Azure/avm-res-
# network-virtualnetwork/azurerm) — for a single VNet + single subnet this
# is two resources either way, and writing them directly avoids taking on
# an external module dependency for something this small.

resource "azurerm_virtual_network" "this" {
  name                = "${local.cluster_name}-vnet"
  location            = azurerm_resource_group.this.location
  resource_group_name = azurerm_resource_group.this.name
  address_space       = [var.vnet_cidr]
  tags                = local.tags
}

# Single subnet for AKS nodes. Pods get their addresses from var.pod_cidr
# via Azure CNI Overlay (network_profile in aks.tf), so no separate pod
# subnet is needed — overlay mode uses a pod CIDR independent of the VNet.
resource "azurerm_subnet" "aks" {
  name                 = "${local.cluster_name}-aks-subnet"
  resource_group_name  = azurerm_resource_group.this.name
  virtual_network_name = azurerm_virtual_network.this.name
  address_prefixes     = [var.aks_subnet_cidr]

  # Required for the storage account network_rules.virtual_network_subnet_ids
  # allow-list in storage.tf / filesystem.tf to actually take effect.
  service_endpoints = ["Microsoft.Storage"]
}
