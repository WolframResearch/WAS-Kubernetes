# WAS — AWS Infrastructure Stack

`wasctl install` / `wasctl serve` apply this stack automatically. This README
is for manual Terraform when you need to provision EKS, EFS, S3, and IRSA
without wasctl.

## What it creates

| Resource | Details |
|---|---|
| VPC | 2 public subnets, 2 private subnets, NAT gateway, DNS enabled |
| EKS cluster | Managed node group, IRSA enabled, API auth mode |
| EFS filesystem | Encrypted, bursting throughput, 30-day IA lifecycle |
| EFS mount targets | One per private subnet |
| EFS CSI IAM role | IRSA role for `kube-system/efs-csi-controller-sa` |
| EBS CSI IAM role | IRSA role for `kube-system/ebs-csi-controller-sa` (Kafka / block volumes) |
| Resource-info S3 bucket | Versioned, encrypted, public access blocked |
| Nodefiles S3 bucket | Versioned, encrypted, public access blocked |
| Resource-manager IAM role | IRSA role scoped to the two app buckets only |
| Route53 CNAME (optional) | Gated on `create_dns_record = true` |

## Prerequisites

- Terraform ≥ 1.10.0 (S3 backend `use_lockfile`)
- AWS CLI configured with permissions to create EKS, VPC, IAM, EFS, S3
- Bootstrap stack applied — see `../bootstrap/README.md`
- `backend.hcl` file created from `examples/backend.hcl.example`

## Workflow

### 1. Run the bootstrap stack (once per account)

```bash
make tf-bootstrap ENV=prod
```

Copy the printed `backend_hcl_hint` into `infra/aws/stack/examples/backend-prod.hcl`
(see `examples/backend.hcl.example` — uses `use_lockfile = true`, not DynamoDB).

### 2. Initialise the main stack

```bash
make tf-init ENV=prod
```

### 3. Review and apply

```bash
make tf-plan  ENV=prod
make tf-apply ENV=prod
```

Apply takes ~20 minutes (EKS cluster creation dominates).

### 4. Update kubeconfig and install cluster add-ons

```bash
$(terraform -chdir=infra/aws/stack output -raw kubeconfig_command)

# Recommended: wasctl install addons
# Or enable chart subcharts / install operators yourself before helm-install
```

### 5. Install the Helm chart

```bash
make helm-install INGRESS_HOST=was.example.com
```

Or print the command and run it manually:

```bash
make print-helm-command
```

## Output reference

| Output | Used by |
|---|---|
| `cluster_name` | kubeconfig, reference |
| `cluster_endpoint` | reference |
| `cluster_oidc_provider_arn` | additional IRSA roles outside this stack |
| `cluster_certificate_authority_data` | reference (sensitive) |
| `kubeconfig_command` | paste into shell |
| `efs_filesystem_id` | wasctl / EFS StorageClass (`was-efs`) |
| `efs_csi_driver_role_arn` | wasctl addons / EFS CSI IRSA |
| `ebs_csi_driver_role_arn` | wasctl addons / EBS CSI IRSA (Kafka gp3) |
| `resource_bucket_name` | `helm install --set objectStorage.resourceBucket=` |
| `nodefile_bucket_name` | `helm install --set objectStorage.nodefileBucket=` |
| `resource_manager_role_arn` | `helm install --set resourceManager.serviceAccount.roleArn=` |
| `helm_install_command_hint` | copy-paste deploy command |

## Teardown order

Destroy in reverse order to avoid dependency errors:

```bash
# 1. Uninstall the chart
helm uninstall was --namespace was

# 2. Remove PVCs (not deleted by helm uninstall)
kubectl delete pvc awes-logs resources-logs endpoint-logs -n was
kubectl delete pvc -l strimzi.io/cluster=kafka-persistent -n kafka

# 3. Destroy the infrastructure
make tf-destroy ENV=prod
```

## Troubleshooting

**`Error: Backend configuration changed`** — run `terraform -chdir=infra/aws/stack init -reconfigure -backend-config=examples/backend-prod.hcl`.

**EKS nodes not joining** — check the worker node IAM role has the standard EKS managed node policies attached (the EKS module handles this automatically).

**EFS mount targets timing out** — EFS mount targets can take 2–5 minutes after `apply`. The bootstrap script waits for the EFS CSI driver to be ready before proceeding.

**`NoCredentialProviders` in resource-manager logs** — verify `resourceManager.serviceAccount.roleArn` matches `terraform output -raw resource_manager_role_arn`, and that the ServiceAccount namespace/name matches `var.resource_manager_service_account_namespace` / `var.resource_manager_service_account_name`.
