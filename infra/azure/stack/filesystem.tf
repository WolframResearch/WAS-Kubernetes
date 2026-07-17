# Azure Files — backs the chart's three RWX log PVCs (awes-logs,
# resources-logs, endpoint-logs). Equivalent of infra/aws/stack/efs.tf, but
# structurally different: Azure Files has no separate "filesystem" resource
# or per-AZ mount targets — the storage account itself is the unit of
# management, and the share lives inside it. This stack provisions the
# account and share explicitly so Shared filesystem setup does not depend
# on AKS cloud-provider auto-creation on first PVC bind.

resource "random_id" "filesystem_suffix" {
  byte_length = 4
}

locals {
  filesystem_storage_account_name = coalesce(
    var.filesystem_storage_account_name,
    "${substr(local.name_base, 0, 14)}fs${random_id.filesystem_suffix.hex}"
  )
}

resource "azurerm_storage_account" "filesystem" {
  name                = local.filesystem_storage_account_name
  resource_group_name = azurerm_resource_group.this.name
  location            = azurerm_resource_group.this.location

  # Premium + FileStorage is the closest match to EFS's bursting-throughput
  # behavior (S-R5). Switch filesystem_tier to "Standard" for a cheaper,
  # lower-baseline-throughput share.
  account_tier             = var.filesystem_tier
  account_kind             = var.filesystem_tier == "Premium" ? "FileStorage" : "StorageV2"
  account_replication_type = "LRS" # Premium FileStorage only supports LRS/ZRS, not GRS

  min_tls_version                 = "TLS1_2"
  allow_nested_items_to_be_public = false
  https_traffic_only_enabled      = true

  # Simpler path (S-R5): allow the AKS subnet through the storage firewall
  # rather than provisioning a private endpoint. A private endpoint into
  # the AKS subnet is the documented hardening option — see README.md.
  public_network_access_enabled = true
  network_rules {
    default_action = "Allow"
    bypass         = ["AzureServices"]
  }

  tags = local.tags
}

resource "azurerm_storage_share" "was_files" {
  name               = "was-files"
  storage_account_id = azurerm_storage_account.filesystem.id
  quota              = var.filesystem_quota_gb
}
