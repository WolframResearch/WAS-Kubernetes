variable "cluster-name" {
  default = "WAS"
}

variable "aks_region" {
  default = "eastus"
}

variable "desired-worker-node" {
  default = "2"
}

variable "min-worker-node" {
  default = "2"
}

variable "max-worker-node" {
  default = "10"
}

variable "max_pods" {
  default = "100"
}

variable "cluster-version" {
  default = "1.24"
}

variable "disk-size" {
  default = "30"
}

variable "instance_type" {
  default = "Standard_D8s_v3"
}

variable "appId" {
  default = "XXXXXX"
}

variable "password" {
  default = "YYYYYY"
}

