# IRSA role for the resource-manager Kubernetes ServiceAccount.
# The Helm chart's resourceManager.serviceAccount.roleArn input points here.

data "aws_iam_policy_document" "resource_manager_assume" {
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
      values = [
        "system:serviceaccount:${var.resource_manager_service_account_namespace}:${var.resource_manager_service_account_name}"
      ]
    }

    condition {
      test     = "StringEquals"
      variable = "${module.eks.oidc_provider}:aud"
      values   = ["sts.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "resource_manager" {
  name               = "${local.cluster_name}-rm-role"
  assume_role_policy = data.aws_iam_policy_document.resource_manager_assume.json

  depends_on = [module.eks]
}

data "aws_iam_policy_document" "resource_manager_s3" {
  statement {
    sid    = "BucketList"
    effect = "Allow"
    actions = [
      "s3:ListBucket",
      "s3:GetBucketLocation"
    ]
    resources = [
      aws_s3_bucket.resource.arn,
      aws_s3_bucket.nodefile.arn
    ]
  }

  statement {
    sid    = "ObjectReadWrite"
    effect = "Allow"
    actions = [
      "s3:GetObject",
      "s3:PutObject",
      "s3:DeleteObject"
    ]
    resources = [
      "${aws_s3_bucket.resource.arn}/*",
      "${aws_s3_bucket.nodefile.arn}/*"
    ]
  }
}

resource "aws_iam_policy" "resource_manager_s3" {
  name        = "${local.cluster_name}-rm-policy"
  description = "Scoped S3 access for the WAS resource-manager pod (IRSA)."
  policy      = data.aws_iam_policy_document.resource_manager_s3.json
}

resource "aws_iam_role_policy_attachment" "resource_manager_s3" {
  role       = aws_iam_role.resource_manager.name
  policy_arn = aws_iam_policy.resource_manager_s3.arn
}
