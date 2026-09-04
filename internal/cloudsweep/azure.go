package cloudsweep

// Azure orphan cleanup is a stub. Azure AKS clusters clean up LoadBalancer
// services synchronously; the destroy sequence still uninstalls ingress-nginx
// before terraform destroy, which is sufficient in practice.
//
// If orphan issues are observed on Azure, add an AzureSweeper here following
// the same safety-rule pattern as AWSSweeper: resource-group scope + cluster
// tag before any deletion.
