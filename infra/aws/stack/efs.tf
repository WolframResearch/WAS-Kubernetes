# Security group — allows NFS (TCP 2049) from within the VPC only.
resource "aws_security_group" "efs" {
  name        = "${local.cluster_name}-efs-sg"
  description = "Allow NFS traffic from within the VPC to EFS mount targets"
  vpc_id      = module.vpc.vpc_id

  ingress {
    description = "NFS from VPC"
    from_port   = 2049
    to_port     = 2049
    protocol    = "tcp"
    cidr_blocks = [var.vpc_cidr]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

# EFS filesystem — encrypted, bursting throughput, with Intelligent Tiering
# to move infrequently accessed files after 30 days.
resource "aws_efs_file_system" "was" {
  creation_token   = "${local.cluster_name}-efs"
  encrypted        = true
  throughput_mode  = "bursting"

  lifecycle_policy {
    transition_to_ia = "AFTER_30_DAYS"
  }
}

# Mount targets — one per private subnet so each AZ can access EFS.
# Keys are CIDR blocks (known at plan time) to avoid the for_each-with-unknown-
# values error that occurs when subnet IDs are used directly as set members.
resource "aws_efs_mount_target" "was" {
  for_each = { for i, cidr in var.private_subnet_cidrs : cidr => module.vpc.private_subnets[i] }

  file_system_id  = aws_efs_file_system.was.id
  subnet_id       = each.value
  security_groups = [aws_security_group.efs.id]
}

# IRSA role for the EFS CSI driver controller ServiceAccount.
data "aws_iam_policy_document" "efs_csi_assume" {
  statement {
    effect  = "Allow"
    actions = ["sts:AssumeRoleWithWebIdentity"]

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
  name               = "${local.cluster_name}-efs-csi-role"
  assume_role_policy = data.aws_iam_policy_document.efs_csi_assume.json

  depends_on = [module.eks]
}

resource "aws_iam_role_policy_attachment" "efs_csi_managed" {
  role       = aws_iam_role.efs_csi_controller.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonEFSCSIDriverPolicy"
}
