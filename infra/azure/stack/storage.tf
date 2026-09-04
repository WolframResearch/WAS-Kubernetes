# Blob storage for the resource-manager / nodefile-manager artifacts.
# Equivalent of infra/aws/stack/s3.tf. Terraform creates the account and
# generates a globally unique name automatically (override via
# var.blob_storage_account_name if needed).

resource "random_id" "blob_suffix" {
  byte_length = 4
}

locals {
  blob_storage_account_name = coalesce(
    var.blob_storage_account_name,
    "${substr(local.name_base, 0, 14)}bs${random_id.blob_suffix.hex}"
  )
}

resource "azurerm_storage_account" "app_data" {
  name                = local.blob_storage_account_name
  resource_group_name = azurerm_resource_group.this.name
  location            = azurerm_resource_group.this.location

  account_tier             = "Standard"
  account_replication_type = var.blob_replication_type
  account_kind             = "StorageV2"
  is_hns_enabled           = false # flat blob namespace — not Data Lake Gen2 (S-R6)

  min_tls_version                 = "TLS1_2"
  allow_nested_items_to_be_public = false
  https_traffic_only_enabled      = true

  public_network_access_enabled = true

  network_rules {
    default_action = "Allow"
    bypass         = ["AzureServices"]
  }

  blob_properties {
    versioning_enabled = true

    delete_retention_policy {
      days = 30
    }

    container_delete_retention_policy {
      days = 30
    }
  }

  tags = local.tags
}

resource "azurerm_storage_container" "resource" {
  name                  = var.resource_container_name
  storage_account_id    = azurerm_storage_account.app_data.id
  container_access_type = "private"
}

resource "azurerm_storage_container" "nodefile" {
  name                  = var.nodefile_container_name
  storage_account_id    = azurerm_storage_account.app_data.id
  container_access_type = "private"
}
