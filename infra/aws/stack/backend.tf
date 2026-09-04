terraform {
  backend "s3" {}
  # All backend configuration is supplied via -backend-config=backend.hcl
  # so this file never needs to be edited.
  #
  # Example init:
  #   terraform -chdir=infra/aws/stack init -backend-config=examples/backend-prod.hcl
  #
  # See examples/backend.hcl.example for the required fields
  # (bucket, key, region, use_lockfile, encrypt).
  # Run infra/aws/bootstrap first to create the state bucket.
  # Requires Terraform >= 1.10 for use_lockfile.
}
