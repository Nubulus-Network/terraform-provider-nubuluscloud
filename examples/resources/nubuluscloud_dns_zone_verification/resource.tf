resource "nubuluscloud_dns_zone" "externo" {
  name = "ejemplo.org"
}

# The challenge record goes wherever the domain resolves TODAY — at the current
# provider, not here: the zone does not exist on our name servers yet.
resource "otro_proveedor_registro_dns" "reto" {
  name    = nubuluscloud_dns_zone.externo.verification_txt_host
  type    = "TXT"
  records = [nubuluscloud_dns_zone.externo.verification_txt_value]
}

resource "nubuluscloud_dns_zone_verification" "externo" {
  zone       = nubuluscloud_dns_zone.externo.name
  depends_on = [otro_proveedor_registro_dns.reto]

  # The default is 90m, which is generous on purpose: the usual reason an
  # attempt fails is the parent zone still serving its cached NXDOMAIN.
  timeout = "90m"
}

# Records can only be written once the zone is verified.
resource "nubuluscloud_dns_rrset" "www" {
  zone   = nubuluscloud_dns_zone.externo.name
  name   = "www"
  type   = "A"
  ttl    = 300
  values = ["203.0.113.10"]

  depends_on = [nubuluscloud_dns_zone_verification.externo]
}
