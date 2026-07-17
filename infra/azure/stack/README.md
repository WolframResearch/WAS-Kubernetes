# WAS — Azure Infrastructure Stack

`wasctl install` / `wasctl serve` apply this stack automatically. This README
is for manual Terraform (AKS, Azure Files, Blob containers, Workload Identity).

## What it creates

| Resource | Details |
|---|---|
| Resource group | Holds everything below — one `terraform destroy` tears it all down |
| VNet + AKS subnet | `10.20.0.0/16` / `10.20.0.0/20` by default; pods use a separate overlay CIDR, not VNet IPs |
| AKS cluster | `SystemAssigned` identity, OIDC issuer + Workload Identity enabled, Azure AD RBAC, Azure CNI Overlay + Calico network policy |
| Azure Files storage account + share | Premium FileStorage by default (closest match to EFS's bursting throughput); backs the chart's 3 RWX log PVCs |
| Blob storage account + 2 containers | `resources` and `nodefiles` containers for resource-manager / nodefile-manager artifacts |
| Resource-manager managed identity | User-assigned identity + federated credential (Workload Identity) + per-container `Storage Blob Data Contributor` role assignments |
| Azure DNS A record (optional) | Gated on `create_dns_record = true`; most deployments skip this — see below |

Every storage account name is **auto-generated** with a random suffix by
default. You do not need to invent a globally unique name before applying
this stack.

## Prerequisites

- Terraform ≥ 1.9.0
- Azure CLI logged in with permissions to create AKS, VNets, storage
  accounts, managed identities, and role assignments in the target
  subscription
- Bootstrap stack applied — see `../bootstrap/README.md`
- `backend.tfvars` file created from `examples/backend.tfvars.example`

## Workflow

### 1. Run the bootstrap stack (once per subscription)

```bash
cd ../bootstrap && terraform init && terraform apply
```

Copy the printed `backend_config_hint` into
`infra/azure/stack/backend.tfvars` (see `examples/backend.tfvars.example`).

### 2. Initialise the main stack

```bash
cd infra/azure/stack
cp examples/terraform.tfvars.example terraform.tfvars   # edit values
terraform init -backend-config=backend.tfvars
```

### 3. Review and apply

```bash
terraform plan
terraform apply
```

Apply takes roughly 10-15 minutes (AKS cluster creation dominates).

### 4. Update kubeconfig and install cluster add-ons

```bash
$(terraform output -raw kubeconfig_command)
```

Then install the chart's prerequisites — see
`charts/wolfram-application-server/README.md` (Prerequisites). Notably for Azure:
- The Azure Files CSI driver ships built into AKS — no separate install
  (unlike AWS's EFS CSI driver, which needs an IRSA role + helm install).
- With wasctl, the addons and app stages create the `was-azurefile` StorageClass
  and install the WAS chart; prefer `./wasctl install addons` / `./wasctl install app`
  over applying StorageClasses by hand.

### 5. Install the Helm chart

```bash
terraform output helm_install_command_hint
# copy, fill in --set ingress.host=..., and run
```

## Blob auth model (default: static account key)

Default `wasctl` Azure installs use `objectStorage.auth.mode=static` and the
`resource_storage_account_key` Terraform output. That path only needs
**Contributor** (`listKeys`) — no `Microsoft.Authorization/roleAssignments/write`.

## Optional Workload Identity

`identity-resource-manager.tf` still creates a user-assigned managed identity
and a federated identity credential (`subject` =
`system:serviceaccount:was:was-resource-manager`, issuer =
`cluster_oidc_issuer_url`). That does **not** create role assignments.

To opt into Workload Identity later:

1. Assign `Storage Blob Data Contributor` on the `resources` / `nodefiles`
   containers to the UAMI (requires Owner/UAA, or an admin does it out-of-band).
2. Deploy the chart with `objectStorage.auth.mode=workloadIdentity` and
   `resourceManager.serviceAccount.azureClientId` set to
   `resource_manager_identity_client_id`.

**Do not** wire `azure.workload.identity/client-id` to
`aadpodidbinding` — that's the deprecated AAD Pod Identity v1 model, not
Workload Identity. This stack and the chart only support the latter.

## Provider / module choices

- **VNet**: written directly (`network.tf`), not via the
  `Azure/avm-res-network-virtualnetwork/azurerm` AVM module. For a single
  VNet + single subnet, a module adds indirection without saving much —
  see the comment in `network.tf`.
- **AKS**: written directly via `azurerm_kubernetes_cluster`, not the
  `Azure/aks/azurerm` module, for the same reason `infra/aws/stack` prefers
  the direct EKS-module-but-not-wrapping-it approach: the resource is
  well-documented and a wrapping module tends to lag behind new provider
  attributes (Workload Identity, Azure CNI Overlay, and Azure AD RBAC are
  all relatively recent and best accessed directly).
- **azurerm provider**: pinned to `~> 4.79`, the latest stable release on
  the public registry as of writing (verified live against
  `registry.terraform.io` — not guessed). Bump deliberately; this is a
  fast-moving provider.

## Output reference

| Output | Used by |
|---|---|
| `cluster_name` / `cluster_id` | reference |
| `cluster_oidc_issuer_url` | additional Workload Identity federated credentials outside this stack |
| `kubeconfig_command` | paste into shell |
| `filesystem_name` / `filesystem_storage_account_name` | `was-azurefile` StorageClass parameters (wasctl addons / chart) |
| `filesystem_storage_account_key` | Azure Files CSI driver secret (sensitive) |
| `resource_storage_account_name` | `helm install --set objectStorage.azure.accountName=` |
| `resource_container_name` | `helm install --set objectStorage.resourceBucket=` |
| `nodefile_container_name` | `helm install --set objectStorage.nodefileBucket=` |
| `resource_manager_identity_client_id` | `helm install --set resourceManager.serviceAccount.azureClientId=` |
| `helm_install_command_hint` | copy-paste deploy command |

## Cost notes (rough monthly, single small environment, East US pricing order-of-magnitude)

- AKS control plane: free (Free tier) — Standard tier (~$73/mo) buys an
  SLA and is worth it for production
- Node pool (2x `Standard_D4s_v5`, ~730 hrs/mo): roughly $280-320/mo at
  pay-as-you-go rates, before any reserved-instance discount
- Azure Files Premium, 100GB provisioned: roughly $20-25/mo (Premium is
  provisioned-capacity priced, not pay-per-use)
- Blob storage account (LRS, light usage): a few dollars/mo
- Outbound data transfer / Load Balancer: variable, typically small at this
  scale

These are ballpark figures for planning, not a quote — check the Azure
Pricing Calculator with your actual region and usage.

## Teardown order

```bash
# 1. Uninstall the chart
helm uninstall was --namespace was

# 2. Remove PVCs (not deleted by helm uninstall)
kubectl delete pvc awes-logs resources-logs endpoint-logs -n was
kubectl delete pvc -l strimzi.io/cluster=kafka-persistent -n kafka

# 3. Destroy the infrastructure
terraform destroy
```

## Troubleshooting

**`Error: Backend configuration changed`** — run `terraform init
-reconfigure -backend-config=backend.tfvars`.

**AKS nodes not joining / pods stuck `Pending`** — check
`kubectl get nodes`; the default node pool needs a few minutes after
`apply` completes to fully register. If it never joins, check the AKS
subnet's `service_endpoints` includes `Microsoft.Storage` (network.tf) and
that `api_server_authorized_ip_ranges` (if set) includes your access path.

**resource-manager can't reach blob storage / `403`** — verify
`resourceManager.serviceAccount.azureClientId` matches `terraform output
-raw resource_manager_identity_client_id`, that the ServiceAccount
namespace/name match `var.resource_manager_service_account_namespace` /
`var.resource_manager_service_account_name`, and that the federated
credential's `subject` (in the Azure Portal, under the managed identity's
"Federated credentials" blade) reads exactly
`system:serviceaccount:was:was-resource-manager`.

**PVCs stuck `Pending`** — confirm `was-azurefile` StorageClass exists
(`kubectl get sc`). This Terraform stack does not create StorageClasses;
install them via `./wasctl install addons` (or the chart/docs prerequisites).
