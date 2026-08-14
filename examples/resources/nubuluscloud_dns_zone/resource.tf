# A domain already assigned to the account is created and active immediately.
resource "nubuluscloud_dns_zone" "propio" {
  name = "ejemplo.com"
}

# Delegate the domain to these at the registrar. Until that is done the zone is
# served but nobody is asking it anything.
output "nameservers" {
  value = nubuluscloud_dns_zone.propio.nameservers
}

# A name registered somewhere else is *reserved* instead: nothing exists on the
# name servers until control of it has been proven. Publish this record wherever
# the domain resolves today, then see nubuluscloud_dns_zone_verification.
output "challenge" {
  value = {
    name  = nubuluscloud_dns_zone.propio.verification_txt_host
    value = nubuluscloud_dns_zone.propio.verification_txt_value
  }
}
