terraform {
  backend "s3" {
    bucket         = "terraform-tfstate-${var.cluster_name}"
    key            = "global/s3/terraform.tfstate"
    region         = "${var.aws_region}"
    dynamodb_table = "${var.dynamodb_table}"
    encrypt        = true
  }
  required_version = ">= 1.13.1, < 1.14"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.10"
    }
  }
}

provider "aws" {
  region = var.aws_region
}

provider "kubernetes" {
  host                   = module.eks.cluster_endpoint
  cluster_ca_certificate = base64decode(module.eks.cluster_certificate_authority_data)

  exec {
    api_version = "client.authentication.k8s.io/v1beta1"
    command     = "aws"
    args        = ["eks", "get-token", "--cluster-name", module.eks.cluster_name]
  }
}

data "aws_availability_zones" "available" {}

module "vpc" {
  source  = "terraform-aws-modules/vpc/aws"
  version = "~> 6.0"

  name = "${var.cluster_name}-vpc"
  cidr = "10.168.0.0/16"

  azs             = data.aws_availability_zones.available.names
  private_subnets = ["10.168.128.0/18", "10.168.192.0/18"]
  public_subnets  = ["10.168.0.0/18", "10.168.64.0/18"]

  enable_nat_gateway   = true
  single_nat_gateway   = true
  enable_dns_support   = true
  enable_dns_hostnames = true

  public_subnet_tags = {
    "kubernetes.io/cluster/${var.cluster_name}" = "shared"
    "kubernetes.io/role/elb"                    = "1"
  }
  private_subnet_tags = {
    "kubernetes.io/cluster/${var.cluster_name}" = "shared"
    "kubernetes.io/role/internal-elb"           = "1"
  }

  tags = {
    Terraform   = "true"
    Environment = var.cluster_name
  }
}

module "eks" {
  source  = "terraform-aws-modules/eks/aws"
  version = "~> 21.1"

  name               = var.cluster_name
  kubernetes_version = var.cluster_version

  vpc_id     = module.vpc.vpc_id
  subnet_ids = module.vpc.private_subnets

  endpoint_public_access       = true
  endpoint_private_access      = true
  endpoint_public_access_cidrs = ["0.0.0.0/0"]

  authentication_mode = "API"
  enable_cluster_creator_admin_permissions = true
  enable_irsa = true

  addons = {
    vpc-cni                = { before_compute = true, most_recent = true }
    coredns                = { most_recent = true }
    kube-proxy             = { most_recent = true }
  }

  eks_managed_node_groups = {
    eks = {
      name           = "${var.cluster_name}-worker-nodes"
      desired_size   = var.desired_worker_node
      min_size       = var.min_worker_node
      max_size       = var.max_worker_node
      disk_size      = var.disk_size
      instance_types = [var.instance_type]
      ami_type       = "AL2023_x86_64_STANDARD"

      iam_role_additional_policies = {
        workers = aws_iam_policy.worker_policy.arn
      }
    }
  }

  tags = {
    Environment = "Wolfram Application Server"
  }
}

data "aws_iam_policy_document" "efs_csi_assume" {
  statement {
    actions = ["sts:AssumeRoleWithWebIdentity"]
    effect  = "Allow"
    principals {
      type        = "Federated"
      identifiers = [module.eks.oidc_provider_arn]
    }
    condition {
      test     = "StringEquals"
      variable = "${module.eks.oidc_provider}:sub"
      values   = ["system:serviceaccount:kube-system:efs-csi-controller-sa"]
    }
    condition {
      test     = "StringEquals"
      variable = "${module.eks.oidc_provider}:aud"
      values   = ["sts.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "efs_csi_controller" {
  name               = "${var.cluster_name}-efs-csi-controller"
  assume_role_policy = data.aws_iam_policy_document.efs_csi_assume.json

  # ensure the module creates the IAM OIDC provider first
  depends_on = [module.eks]
}

resource "aws_iam_role_policy_attachment" "efs_csi_managed" {
  role       = aws_iam_role.efs_csi_controller.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonEFSCSIDriverPolicy"

  depends_on = [aws_iam_role.efs_csi_controller]
}

output "efs_csi_role_arn" {
  value = aws_iam_role.efs_csi_controller.arn
}

resource "aws_iam_policy" "worker_policy" {
  name        = "node-workers-policy-${var.cluster_name}"
  description = "Node Workers IAM policies"
  policy      = file("${path.module}/iam-policy.json")
}