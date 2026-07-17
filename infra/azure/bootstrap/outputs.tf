output "resource_group_name" {
  description = "Name of the resource group created for Terraform state. Copy into infra/azure/stack/examples/backend.tfvars."
  value       = azurerm_resource_group.tfstate.name
}

output "storage_account_name" {
  description = "Name of the storage account created for Terraform state. Copy into infra/azure/stack/examples/backend.tfvars."
  value       = azurerm_storage_account.tfstate.name
}

output "container_name" {
  description = "Name of the blob container created for Terraform state. Copy into infra/azure/stack/examples/backend.tfvars."
  value       = azurerm_storage_container.tfstate.name
}

output "backend_config_hint" {
  description = "Contents wasctl writes into infra/azure/stack/backend.tfvars (for reference only)."
  value       = <<-EOT
    resource_group_name  = "${azurerm_resource_group.tfstate.name}"
    storage_account_name = "${azurerm_storage_account.tfstate.name}"
    container_name       = "${azurerm_storage_container.tfstate.name}"
    key                  = "stack/terraform.tfstate"
    use_azuread_auth     = true
  EOT
}
