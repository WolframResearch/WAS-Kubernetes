output "resource_group_name" {
  value = "${var.resource_group}"
}

output "kubernetes_cluster_name" {
  value = "${var.cluster_name}-aks"
}