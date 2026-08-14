data "nubuluscloud_dns_zones" "todas" {}

output "zonas_sin_verificar" {
  value = [
    for z in data.nubuluscloud_dns_zones.todas.zones : z.name
    if z.status == "pending_verification"
  ]
}
