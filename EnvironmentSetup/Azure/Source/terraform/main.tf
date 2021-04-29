terraform {
  backend "azurerm" {
    resource_group_name  = "XXXXXX"
    storage_account_name = "YYYYYY"
    container_name       = "tfstate"
    key                  = "terraform.tfstate"
  }
  required_providers {
    azurerm = {
      source  = "hashicorp/azurerm"
	  version = "~> 2.0"
    }
  }
  required_version = "~> 0.14"
}

provider "azurerm" {
  features {}
}

resource "azurerm_resource_group" "default" {
  name     = "${var.cluster-name}-rg"
  location = var.aks_region

  tags = {
    environment = "Wolfram Application Server"
  }
}

resource "azurerm_kubernetes_cluster" "default" {
  name                = "${var.cluster-name}-aks"
  location            = azurerm_resource_group.default.location
  resource_group_name = azurerm_resource_group.default.name
  dns_prefix          = "${var.cluster-name}-k8s"
  kubernetes_version  = var.cluster-version

  default_node_pool {
    name                = "workernodes"
    node_count          = var.desired-worker-node
    vm_size             = var.instance_type
    os_disk_size_gb     = var.disk-size
	max_pods            = var.max_pods
	enable_auto_scaling = true
    min_count           = var.min-worker-node
    max_count           = var.max-worker-node
  }

  service_principal {
    client_id     = var.appId
    client_secret = var.password
  }

  role_based_access_control {
    enabled = true
  }
  
  tags = {
    environment = "Wolfram Application Server"
  }
  
}
