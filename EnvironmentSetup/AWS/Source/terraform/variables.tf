variable "aws_region" {
  default = "us-east-1"
}

variable "cluster-name" {
  default = "WAS"
}

variable "cluster-version" {
  default = "1.25"
}

variable "disk-size" {
  default = "30"
}

variable "instance_type" {
  default = "c5.2xlarge"
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
