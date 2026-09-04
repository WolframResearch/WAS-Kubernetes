# =============================================================================
# Region
# =============================================================================

variable "aws_region" {
  type        = string
  default     = "us-east-1"
  description = "AWS region for all resources."
}

# =============================================================================
# Cluster
# =============================================================================

variable "cluster_name" {
  type        = string
  default     = "was"
  description = "EKS cluster name. Also used as a prefix for related resource names. Must be ≤ 22 chars."

  validation {
    condition     = length(var.cluster_name) <= 22
    error_message = "cluster_name must be 22 characters or fewer; longer names cause the EKS node group IAM role name_prefix to exceed the 38-char IAM limit."
  }
}

variable "cluster_version" {
  type        = string
  default     = "1.36"
  description = "Kubernetes version for the EKS cluster."
}

variable "authentication_mode" {
  type        = string
  default     = "API"
  description = "EKS authentication mode. 'API' is recommended; 'CONFIG_MAP' is legacy."

  validation {
    condition     = contains(["API", "API_AND_CONFIG_MAP", "CONFIG_MAP"], var.authentication_mode)
    error_message = "authentication_mode must be one of: API, API_AND_CONFIG_MAP, CONFIG_MAP."
  }
}

variable "enable_cluster_creator_admin_permissions" {
  type        = bool
  default     = true
  description = "Grant the IAM identity running Terraform full admin access to the cluster."
}

# =============================================================================
# Networking
# =============================================================================

variable "vpc_cidr" {
  type        = string
  default     = "10.168.0.0/16"
  description = "CIDR block for the VPC."
}

variable "public_subnet_cidrs" {
  type        = list(string)
  default     = ["10.168.0.0/18", "10.168.64.0/18"]
  description = "CIDR blocks for public subnets (one per AZ used)."
}

variable "private_subnet_cidrs" {
  type        = list(string)
  default     = ["10.168.128.0/18", "10.168.192.0/18"]
  description = "CIDR blocks for private subnets (one per AZ used). EKS nodes and EFS mount targets go here."
}

variable "single_nat_gateway" {
  type        = bool
  default     = true
  description = "Use a single NAT gateway for all private subnets. Set to false for production HA (one NAT per AZ)."
}

# =============================================================================
# Node group
# =============================================================================

variable "node_instance_types" {
  type        = list(string)
  default     = ["c5.2xlarge"]
  description = "EC2 instance types for the managed node group."
}

variable "node_disk_size_gb" {
  type        = number
  default     = 50
  description = "EBS root volume size (GB) for each worker node."
}

variable "node_min_size" {
  type        = number
  default     = 2
  description = "Minimum number of worker nodes."
}

variable "node_desired_size" {
  type        = number
  default     = 2
  description = "Desired number of worker nodes at launch."
}

variable "node_max_size" {
  type        = number
  default     = 10
  description = "Maximum number of worker nodes (HPA ceiling)."
}

# =============================================================================
# S3 buckets (application data)
# =============================================================================

variable "resource_bucket_name" {
  type        = string
  default     = null
  description = "Name for the resource-info S3 bucket. If null, a name is auto-generated using a random suffix."
}

variable "nodefile_bucket_name" {
  type        = string
  default     = null
  description = "Name for the nodefiles S3 bucket. If null, a name is auto-generated using a random suffix."
}

# =============================================================================
# IRSA — resource-manager service account
# =============================================================================

variable "resource_manager_service_account_name" {
  type        = string
  default     = "was-resource-manager"
  description = "Kubernetes ServiceAccount name for the resource-manager pod. Must match the Helm chart value resourceManager.serviceAccount.name."
}

variable "resource_manager_service_account_namespace" {
  type        = string
  default     = "was"
  description = "Kubernetes namespace for the resource-manager ServiceAccount. Must match the Helm chart value namespace.name."
}

# =============================================================================
# Route53 (optional DNS)
# =============================================================================

variable "create_dns_record" {
  type        = bool
  default     = false
  description = "Create a Route53 CNAME record pointing dns_record_name at the ingress-nginx ELB. Requires hosted_zone_id and dns_record_name."
}

variable "hosted_zone_id" {
  type        = string
  default     = ""
  description = "Route53 hosted zone ID. Required when create_dns_record = true."
}

variable "dns_record_name" {
  type        = string
  default     = ""
  description = "Fully-qualified DNS name to create (e.g. was.example.com). Required when create_dns_record = true."
}

variable "elb_dns_name" {
  type        = string
  default     = ""
  description = "DNS name of the ingress-nginx ELB. Required when create_dns_record = true. Obtain after running the bootstrap script: kubectl get svc -n ingress-nginx ingress-nginx-controller -o jsonpath='{.status.loadBalancer.ingress[0].hostname}'"
}

# =============================================================================
# Tags
# =============================================================================

variable "tags" {
  type        = map(string)
  default     = {}
  description = "Additional tags to merge onto all resources. Merged with stable defaults (Application, ManagedBy)."
}
