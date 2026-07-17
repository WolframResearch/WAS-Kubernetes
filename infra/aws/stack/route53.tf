# Optional Route53 CNAME record.
# Set create_dns_record = true and provide hosted_zone_id, dns_record_name,
# and elb_dns_name (the ingress-nginx ELB hostname) to use this.

resource "aws_route53_record" "was" {
  count = var.create_dns_record ? 1 : 0

  zone_id = var.hosted_zone_id
  name    = var.dns_record_name
  type    = "CNAME"
  ttl     = 300
  records = [var.elb_dns_name]
}
