variable "aws_region" {
  type        = string
  default     = "us-east-1"
  description = "AWS region for the state bucket and lock table."
}

variable "state_bucket_name" {
  type        = string
  description = "Name of the S3 bucket that will hold Terraform state. Must be globally unique. Recommended pattern: wolfram-was-tfstate-<env>-<account_id>."

  validation {
    condition     = can(regex("^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$", var.state_bucket_name))
    error_message = "state_bucket_name must be 3–63 lowercase alphanumeric characters or hyphens, starting and ending with a letter or digit."
  }
}

variable "lock_table_name" {
  type        = string
  default     = "wolfram-was-tfstate-lock"
  description = "Name of the DynamoDB table used for Terraform state locking."
}
