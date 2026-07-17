# =============================================================================
# VPC
# =============================================================================

output "vpc_id" {
  description = "VPC ID. Used by wasctl destroy to scope the orphan resource sweep."
  value       = module.vpc.vpc_id
}

# =============================================================================
# EKS cluster
# =============================================================================

output "cluster_name" {
  description = "EKS cluster name."
  value       = module.eks.cluster_name
}

output "cluster_endpoint" {
  description = "EKS API server endpoint."
  value       = module.eks.cluster_endpoint
}

output "cluster_oidc_provider_arn" {
  description = "ARN of the EKS OIDC provider. Used to create additional IRSA roles outside this stack."
  value       = module.eks.oidc_provider_arn
}

output "cluster_certificate_authority_data" {
  description = "Base64-encoded cluster CA certificate."
  value       = module.eks.cluster_certificate_authority_data
  sensitive   = true
}

output "kubeconfig_command" {
  description = "Run this command to update your local kubeconfig."
  value       = "aws eks update-kubeconfig --region ${var.aws_region} --name ${module.eks.cluster_name}"
}

# =============================================================================
# EFS
# =============================================================================

output "efs_filesystem_id" {
  description = "EFS filesystem ID. Pass to the bootstrap script via --efs-id to configure the was-efs StorageClass."
  value       = aws_efs_file_system.was.id
}

output "efs_csi_driver_role_arn" {
  description = "IAM role ARN for the EFS CSI controller ServiceAccount. Pass to the bootstrap script via --efs-csi-role-arn."
  value       = aws_iam_role.efs_csi_controller.arn
}

output "ebs_csi_driver_role_arn" {
  description = "IAM role ARN for the EBS CSI controller ServiceAccount (Kafka / block volumes)."
  value       = aws_iam_role.ebs_csi_controller.arn
}

# =============================================================================
# S3 buckets  →  Helm chart: objectStorage.*
# =============================================================================

output "resource_bucket_name" {
  description = "Name of the resource-info S3 bucket. Helm chart: --set objectStorage.resourceBucket=<value>"
  value       = aws_s3_bucket.resource.bucket
}

output "nodefile_bucket_name" {
  description = "Name of the nodefiles S3 bucket. Helm chart: --set objectStorage.nodefileBucket=<value>"
  value       = aws_s3_bucket.nodefile.bucket
}

# =============================================================================
# IRSA  →  Helm chart: resourceManager.serviceAccount.roleArn
# =============================================================================

output "resource_manager_role_arn" {
  description = "IAM role ARN for the resource-manager pod (IRSA). Helm chart: --set resourceManager.serviceAccount.roleArn=<value>"
  value       = aws_iam_role.resource_manager.arn
}

# =============================================================================
# Convenience
# =============================================================================

output "helm_install_command_hint" {
  description = "Suggested helm install command populated with all Terraform outputs. Replace <your-dns-name> with your ingress hostname."
  value       = <<-EOT
    helm install was charts/wolfram-application-server \
      -f charts/wolfram-application-server/values-aws.yaml \
      --namespace was --create-namespace \
      --set ingress.host=<your-dns-name> \
      --set objectStorage.region=${var.aws_region} \
      --set objectStorage.resourceBucket=${aws_s3_bucket.resource.bucket} \
      --set objectStorage.nodefileBucket=${aws_s3_bucket.nodefile.bucket} \
      --set resourceManager.serviceAccount.roleArn=${aws_iam_role.resource_manager.arn}
  EOT
}
