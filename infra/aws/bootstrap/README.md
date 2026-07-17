# Bootstrap — Terraform Remote State Setup (AWS)

`wasctl install` (and `wasctl serve`) create this state backend automatically.
Use this directory only for manual Terraform workflows.

Run **once per AWS account** before the main stack. Creates the S3 bucket and
DynamoDB lock table for remote state. This stack itself uses local state — that
is intentional.

## Prerequisites

- AWS CLI configured (`aws configure` or environment variables)
- Terraform ≥ 1.10.0 (stack backend uses S3 `use_lockfile`)
- See also [wasctl](../../../README.md)

## Usage

```bash
cd infra/aws/bootstrap

# Create a tfvars file (never commit this file)
cp examples/terraform.tfvars.example terraform.tfvars
# edit state_bucket_name — must be globally unique, e.g.:
#   wolfram-was-tfstate-prod-$(aws sts get-caller-identity --query Account --output text)

terraform init
terraform apply
```

After `apply` succeeds, copy the `backend_hcl_hint` output into
`infra/aws/stack/backend.hcl` or `infra/aws/stack/examples/backend-<ENV>.hcl`
(see `../stack/examples/backend.hcl.example`).

## What it creates

| Resource | Name |
|---|---|
| S3 bucket (versioned, encrypted, private) | as per `state_bucket_name` |
| DynamoDB table (PAY_PER_REQUEST) | as per `lock_table_name` (legacy; stack backend prefers S3 `use_lockfile`) |

## Notes

- Do **not** run `terraform destroy` unless you intend to delete the state
  bucket for all environments. The bucket has `force_destroy = false` so
  Terraform will refuse to delete it while it contains objects.
- The `terraform.tfstate` file this stack produces lives in the
  `infra/aws/bootstrap/` directory. Keep it, or at minimum keep a copy.
  It is covered by `.gitignore` — do not commit it.
