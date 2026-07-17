output "state_bucket_name" {
  description = "Name of the S3 bucket created for Terraform state. Copy this into backend.hcl."
  value       = aws_s3_bucket.tfstate.bucket
}

output "lock_table_name" {
  description = "Name of the DynamoDB table for state locking. Copy this into backend.hcl."
  value       = aws_dynamodb_table.tfstate_lock.name
}

output "backend_hcl_hint" {
  description = "Contents wasctl writes into infra/aws/stack/backend.hcl (for reference only)"
  value       = <<-EOT
    bucket       = "${aws_s3_bucket.tfstate.bucket}"
    key          = "stack/terraform.tfstate"
    region       = "${var.aws_region}"
    use_lockfile = true
    encrypt      = true
  EOT
}
