terraform {
  required_version = ">= 1.9.0, < 2.0.0"

  required_providers {
    azurerm = {
      source = "hashicorp/azurerm"
      # 4.79.0 is the latest stable release on the registry as of writing.
      # Pinned to the patch series so `terraform init` never silently picks
      # up a new minor with breaking schema changes; bump deliberately.
      version = "~> 4.79"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.6"
    }
  }

  # Intentionally local state. This stack creates the remote-state storage
  # account that the main stack (infra/azure/stack) uses as its backend; it
  # cannot itself live in remote state. See README.md.
}
