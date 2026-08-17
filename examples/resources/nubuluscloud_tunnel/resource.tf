# Set external_id. Without it, an apply interrupted between creating the tunnel
# and writing the state leaves one behind that nothing can find again — holding
# an address from the pool and a credential nobody ever saw — and the next apply
# makes another. With it, the next apply recognises the tunnel.
resource "nubuluscloud_tunnel" "produccion" {
  name        = "produccion"
  external_id = "produccion-eu"
}

# What the tunnel client needs. Both are issued once, at creation, and can never
# be read back: they live in the state file and nowhere else.
output "tunnel_token" {
  value     = nubuluscloud_tunnel.produccion.tunnel_token
  sensitive = true
}

output "wireguard" {
  value = {
    private_key = nubuluscloud_tunnel.produccion.wireguard_private_key
    address     = nubuluscloud_tunnel.produccion.wireguard_address
    dns         = nubuluscloud_tunnel.produccion.wireguard_dns
    peer = {
      public_key  = nubuluscloud_tunnel.produccion.peer_public_key
      endpoint    = nubuluscloud_tunnel.produccion.peer_endpoint
      allowed_ips = nubuluscloud_tunnel.produccion.peer_allowed_ips
    }
  }
  sensitive = true
}

# Point your own hostname here with a CNAME, and route it with
# nubuluscloud_tunnel_route.
output "cname_target" {
  value = nubuluscloud_tunnel.produccion.cname_target
}

# Unattended recovery, for a pipeline that has to converge on its own.
#
# With adopt_existing, an apply that finds the external_id already taken takes
# that tunnel over and issues it a NEW credential — which stops anything still
# running on the old one within seconds. Leave it off unless the identifier is
# unambiguously yours: the provider cannot tell your own interrupted apply from
# a tunnel that is up and carrying traffic.
resource "nubuluscloud_tunnel" "ci" {
  name           = "ci"
  external_id    = "ci-runner-1"
  adopt_existing = true
}
