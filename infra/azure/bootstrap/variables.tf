variable "subscription_id" {
  type        = string
  description = "Azure subscription ID to deploy the state backend into."
}

variable "tenant_id" {
  type        = string
  description = "Azure AD tenant ID. Required for explicit (non-interactive) provider authentication."
}

variable "location" {
  type        = string
  default     = "eastus"
  description = "Azure region for the resource group and storage account."
}

variable "resource_group_name" {
  type        = string
  default     = "wolfram-was-tfstate-rg"
  description = "Name of the dedicated resource group created to hold the Terraform state backend. Kept separate from the main stack's resource group so it is never accidentally destroyed alongside the cluster."
}

variable "storage_account_base" {
  type        = string
  default     = "wolframwastfstate"
  description = "Base name for the state storage account. An 8-character random hex suffix is always appended for global uniqueness; the base is truncated to 16 characters first so the result never exceeds Azure's 24-character limit. Lowercase letters and digits only — anything else is stripped."
}

variable "container_name" {
  type        = string
  default     = "tfstate"
  description = "Name of the blob container that holds the main stack's state file."
}

variable "tags" {
  type        = map(string)
  default     = {}
  description = "Additional tags to merge onto all resources. Merged with stable defaults (ManagedBy, Application, Component)."
}
