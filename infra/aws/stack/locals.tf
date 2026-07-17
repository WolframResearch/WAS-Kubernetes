locals {
  cluster_name = var.cluster_name

  tags = merge(
    {
      Application = "WolframApplicationServer"
      ManagedBy   = "Terraform"
      Cluster     = local.cluster_name
    },
    var.tags
  )
}
