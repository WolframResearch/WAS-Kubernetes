# Bootstrap — Terraform Remote State Setup (Azure)

`wasctl install` (and `wasctl serve`) create this state backend automatically.
Use this directory only for manual Terraform workflows.

Run **once per Azure subscription** before `infra/azure/stack`. Creates the
resource group, storage account, and blob container for remote state. This
stack itself uses local state (chicken-and-egg).

## Prerequisites

- Azure CLI, logged in: `az login`
- Terraform ≥ 1.9.0
- Permissions to create resource groups and storage accounts in the target
  subscription
- See also [wasctl](../../../README.md)

## Usage

```bash
cd infra/azure/bootstrap

az login
az account set --subscription <subscription-id>

cp examples/terraform.tfvars.example terraform.tfvars
# fill subscription_id / tenant_id, e.g.:
#   subscription_id = "$(az account show --query id -o tsv)"
#   tenant_id       = "$(az account show --query tenantId -o tsv)"

terraform init
terraform apply
```

After `apply` succeeds, copy the printed `backend_config_hint` output into
`infra/azure/stack/backend.tfvars` (see `../stack/examples/backend.tfvars.example`).

## What it creates

| Resource | Name |
|---|---|
| Resource group | `wolfram-was-tfstate-rg` (default; override with `resource_group_name`) |
| Storage account (versioned, encrypted, private, network-locked) | `wolframwastfstate<8-char-random-suffix>` — generated automatically, never typed in by hand |
| Blob container | `tfstate` |

The storage account is provisioned with:
- `account_tier = "Standard"`, `account_replication_type = "LRS"` (single
  region, cheap, sufficient for tfstate)
- `min_tls_version = "TLS1_2"`, `allow_nested_items_to_be_public = false`
- `public_network_access_enabled = false`, with `network_rules.bypass =
  ["AzureServices"]` so Azure-internal diagnostics still work
- Blob versioning, change feed, and 90-day soft delete on both blobs and
  containers

## Authenticating the main stack's backend (no shared key needed day-to-day)

`backend_config_hint` includes `use_azuread_auth = true`. This tells
Terraform's `azurerm` backend to authenticate to the state blob using your
Azure AD identity (the same one `az login` set up) instead of the storage
account's shared key. You — or whoever runs `infra/azure/stack` — needs the
**Storage Blob Data Contributor** role on this storage account:

```bash
az role assignment create \
  --assignee <your-or-CI-principal-id> \
  --role "Storage Blob Data Contributor" \
  --scope $(terraform output -raw resource_group_name | xargs -I{} az group show --name {} --query id -o tsv)
```

(Scope it to the storage account specifically if you want it tighter than
resource-group-wide.)

The storage account's shared access key remains enabled
(`shared_access_key_enabled = true`) as a break-glass fallback — it is not
the primary auth path and is not printed or stored anywhere by this stack.

## Notes

- Do **not** run `terraform destroy` on this stack unless you intend to
  delete the state backend for every environment that points at it. There
  is no `force_destroy`-equivalent guard here (Azure storage accounts don't
  refuse deletion just because they're non-empty the way `force_destroy =
  false` does for S3), so be deliberate.
- The `terraform.tfstate` file this stack produces lives in
  `infra/azure/bootstrap/`. Keep it, or at minimum keep a copy. It is
  covered by `.gitignore` (`*.tfstate*`) — do not commit it.
- Storage account and resource group names are generated, not chosen, by
  default. This is deliberate: the less the customer has to invent and
  type correctly before running the stack, the fewer chances to collide
  with an existing globally-unique Azure name or typo a value the main
  stack later depends on.
