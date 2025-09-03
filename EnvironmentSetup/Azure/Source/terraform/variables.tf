variable "cluster_name" {
  default = "WAS"
}

variable "min_worker_node" {
  default = "2"
}

variable "max_worker_node" {
  default = "10"
}

variable "max_pods" {
  default = "100"
}

variable "cluster_version" {
  default = "1.33"
}

variable "disk_size" {
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

variable "resource_group" {
  default = "ZZZZZZ"
}

variable "region" {
  default = "TTTTTT"
}

variable "subscription_id" {
  default = "UUUUUU"
}

variable "tenant_id" {
  default = "VVVVVV"
}