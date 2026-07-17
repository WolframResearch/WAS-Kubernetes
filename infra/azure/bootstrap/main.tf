provider "azurerm" {
  subscription_id = var.subscription_id
  tenant_id       = var.tenant_id
  features {}
}

locals {
  tags = merge(
    {
      ManagedBy   = "Terraform"
      Application = "WolframApplicationServer"
      Component   = "wasctl-state-backend"
    },
    var.tags
  )

  # Storage account names must be 3-24 chars, lowercase letters and digits
  # only, and globally unique across all of Azure. We sanitize and truncate
  # the base to 16 chars so the full 8-char random suffix always fits,
  # regardless of how long a customer-supplied storage_account_base is.
  storage_account_base_clean = substr(
    lower(replace(var.storage_account_base, "/[^a-z0-9]/", "")),
    0,
    16
  )
  storage_account_name = "${local.storage_account_base_clean}${random_id.tfstate_suffix.hex}"
}

resource "random_id" "tfstate_suffix" {
  byte_length = 4
}

resource "azurerm_resource_group" "tfstate" {
  name     = var.resource_group_name
  location = var.location
  tags     = local.tags
}

# Storage account for Terraform state. Equivalent of the AWS bootstrap's
# S3 bucket: versioned, encrypted, no public access, locked down to Azure
# services + the customer's own access by default.
resource "azurerm_storage_account" "tfstate" {
  name                = local.storage_account_name
  resource_group_name = azurerm_resource_group.tfstate.name
  location            = azurerm_resource_group.tfstate.location

  account_tier             = "Standard"
  account_replication_type = "LRS"
  account_kind             = "StorageV2"

  min_tls_version                 = "TLS1_2"
  allow_nested_items_to_be_public = false
  https_traffic_only_enabled      = true

  # The azurerm backend block (infra/azure/stack/backend.tf) uses
  # use_azuread_auth = true, so the shared key is not actually needed for
  # day-to-day Terraform operations. It stays enabled because disabling it
  # is a one-way door (some tooling and break-glass access still expects
  # it) — revisit as a hardening pass once AzureAD-only access is verified
  # end-to-end.
  shared_access_key_enabled = true

  public_network_access_enabled = true

  network_rules {
    default_action = "Allow"
    bypass         = ["AzureServices"]
  }

  blob_properties {
    versioning_enabled  = true
    change_feed_enabled = true

    delete_retention_policy {
      days = 90
    }

    container_delete_retention_policy {
      days = 90
    }
  }

  tags = local.tags
}

resource "azurerm_storage_container" "tfstate" {
  name                  = var.container_name
  storage_account_id    = azurerm_storage_account.tfstate.id
  container_access_type = "private"
}
