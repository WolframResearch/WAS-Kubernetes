# Stable random suffix for auto-generated bucket names.
resource "random_id" "bucket_suffix" {
  byte_length = 4
}

locals {
  resource_bucket_name = coalesce(var.resource_bucket_name, "${local.cluster_name}-resources-${random_id.bucket_suffix.hex}")
  nodefile_bucket_name = coalesce(var.nodefile_bucket_name, "${local.cluster_name}-nodefiles-${random_id.bucket_suffix.hex}")
}

# ─── Resource-info bucket ────────────────────────────────────────────────────

resource "aws_s3_bucket" "resource" {
  bucket = local.resource_bucket_name
}

resource "aws_s3_bucket_server_side_encryption_configuration" "resource" {
  bucket = aws_s3_bucket.resource.id
  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

resource "aws_s3_bucket_versioning" "resource" {
  bucket = aws_s3_bucket.resource.id
  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_public_access_block" "resource" {
  bucket                  = aws_s3_bucket.resource.id
  block_public_acls       = true
  ignore_public_acls      = true
  block_public_policy     = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_ownership_controls" "resource" {
  bucket = aws_s3_bucket.resource.id
  rule {
    object_ownership = "BucketOwnerEnforced"
  }
}

resource "aws_s3_bucket_lifecycle_configuration" "resource" {
  bucket = aws_s3_bucket.resource.id

  rule {
    id     = "abort-incomplete-multipart"
    status = "Enabled"
    abort_incomplete_multipart_upload {
      days_after_initiation = 7
    }
  }

  rule {
    id     = "expire-noncurrent-versions"
    status = "Enabled"
    noncurrent_version_expiration {
      noncurrent_days = 90
    }
  }
}

# ─── Nodefiles bucket ─────────────────────────────────────────────────────────

resource "aws_s3_bucket" "nodefile" {
  bucket = local.nodefile_bucket_name
}

resource "aws_s3_bucket_server_side_encryption_configuration" "nodefile" {
  bucket = aws_s3_bucket.nodefile.id
  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

resource "aws_s3_bucket_versioning" "nodefile" {
  bucket = aws_s3_bucket.nodefile.id
  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_public_access_block" "nodefile" {
  bucket                  = aws_s3_bucket.nodefile.id
  block_public_acls       = true
  ignore_public_acls      = true
  block_public_policy     = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_ownership_controls" "nodefile" {
  bucket = aws_s3_bucket.nodefile.id
  rule {
    object_ownership = "BucketOwnerEnforced"
  }
}

resource "aws_s3_bucket_lifecycle_configuration" "nodefile" {
  bucket = aws_s3_bucket.nodefile.id

  rule {
    id     = "abort-incomplete-multipart"
    status = "Enabled"
    abort_incomplete_multipart_upload {
      days_after_initiation = 7
    }
  }

  rule {
    id     = "expire-noncurrent-versions"
    status = "Enabled"
    noncurrent_version_expiration {
      noncurrent_days = 90
    }
  }
}
