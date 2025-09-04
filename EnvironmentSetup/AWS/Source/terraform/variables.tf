variable "aws_region" {
  default = "us-east-1"
}

variable "cluster_name" {
  default = "WAS"
}

variable "cluster_version" {
  default = "1.33"
}

variable "disk_size" {
  default = "30"
}

variable "instance_type" {
  default = "c5.2xlarge"
}

variable "desired_worker_node" {
  default = "2"
}

variable "min_worker_node" {
  default = "2"
}

variable "max_worker_node" {
  default = "10"
}
