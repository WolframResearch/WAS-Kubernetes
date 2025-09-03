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
	    version = ">= 4.38.0, < 5.0.0"
    }
  }
  required_version = ">= 1.13.1, < 1.14"
}

provider "azurerm" {
  features {}
  subscription_id = "${var.subscription_id}"
  tenant_id       = "${var.tenant_id}"

}

resource "azurerm_kubernetes_cluster" "default" {
  name                = "${var.cluster_name}-aks"
  location            = "${var.region}"
  resource_group_name = "${var.resource_group}"
  dns_prefix          = "${var.cluster_name}-k8s"
  kubernetes_version  = "${var.cluster_version}"

  default_node_pool {
    name                = "workernodes"
    vm_size             = "${var.instance_type}"
    os_disk_size_gb     = "${var.disk_size}"
    auto_scaling_enabled = true
	  max_pods            = "${var.max_pods}"
    min_count           = "${var.min_worker_node}"
    max_count           = "${var.max_worker_node}"
  }

  service_principal {
    client_id     = "${var.appId}"
    client_secret = "${var.password}"
  }
  
  tags = {
    environment = "Wolfram Application Server"
  }
  
}
