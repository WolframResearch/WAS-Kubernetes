terraform {
  backend "azurerm" {}
  # All backend configuration is supplied via -backend-config=backend.tfvars
  # so this file never needs to be edited.
  #
  # Example init:
  #   terraform -chdir=infra/azure/stack init -backend-config=backend.tfvars
  #
  # See examples/backend.tfvars.example for the required fields
  # (resource_group_name, storage_account_name, container_name, key, use_azuread_auth).
  # Run infra/azure/bootstrap first to create the storage account and container.
}
