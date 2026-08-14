# Three addresses on one name are ONE resource with three values, never three
# resources: the DNS operates on record sets, and modelling them separately
# would have each apply race against the other two.
resource "nubuluscloud_dns_rrset" "www" {
  zone   = nubuluscloud_dns_zone.propio.name
  name   = "www"
  type   = "A"
  ttl    = 300
  values = ["203.0.113.10", "203.0.113.11", "203.0.113.12"]
}

# "@" is the zone apex.
resource "nubuluscloud_dns_rrset" "correo" {
  zone   = nubuluscloud_dns_zone.propio.name
  name   = "@"
  type   = "MX"
  ttl    = 3600
  values = ["10 mail.ejemplo.com."]
}

resource "nubuluscloud_dns_rrset" "spf" {
  zone   = nubuluscloud_dns_zone.propio.name
  name   = "@"
  type   = "TXT"
  ttl    = 3600
  values = ["\"v=spf1 mx -all\""]
}
