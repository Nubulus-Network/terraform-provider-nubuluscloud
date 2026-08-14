# A zone claimed from the panel, so records can be managed from Terraform
# without importing the zone itself.
data "nubuluscloud_dns_zone" "propio" {
  name = "ejemplo.com"
}

resource "nubuluscloud_dns_rrset" "www" {
  zone   = data.nubuluscloud_dns_zone.propio.name
  name   = "www"
  type   = "A"
  ttl    = 300
  values = ["203.0.113.10"]
}
