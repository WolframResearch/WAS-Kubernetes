locals {
  cluster_name = var.cluster_name

  # Sanitized form of cluster_name usable inside Azure storage account names
  # (lowercase letters and digits only — Azure storage accounts allow
  # nothing else). Each storage account in storage.tf / filesystem.tf
  # truncates this further to leave room for a short service tag plus an
  # 8-character random suffix within Azure's 24-character name limit.
  name_base = lower(replace(local.cluster_name, "/[^a-zA-Z0-9]/", ""))

  tags = merge(
    {
      Application = "WolframApplicationServer"
      ManagedBy   = "Terraform"
      Cluster     = local.cluster_name
    },
    var.tags
  )
}
