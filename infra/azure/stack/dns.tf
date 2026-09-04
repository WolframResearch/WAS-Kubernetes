# Optional A record in a customer-owned Azure DNS zone. Gated off by
# default — see var.create_dns_record's description. Most deployments use a
# free *.cloudapp.azure.com DNS label on the ingress-nginx load balancer
# and need no Terraform DNS resource. This exists for parity with the AWS
# stack's optional Route53 record, for customers who want their own domain.
#
# Same chicken-and-egg as the AWS stack's route53.tf: the ingress IP only
# exists after the chart's ingress-nginx is up, so this can only be applied
# in a second pass (or left to external-dns / a manual record).

resource "azurerm_dns_a_record" "was" {
  count = var.create_dns_record ? 1 : 0

  name                = var.dns_record_name
  zone_name           = var.dns_zone_name
  resource_group_name = var.dns_zone_resource_group
  ttl                 = 300
  records             = [var.ingress_ip]
}
