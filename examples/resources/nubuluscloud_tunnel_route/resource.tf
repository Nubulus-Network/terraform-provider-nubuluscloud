resource "nubuluscloud_tunnel" "produccion" {
  name        = "produccion"
  external_id = "produccion-eu"
}

# Everything for this hostname goes through the tunnel to a service listening on
# the machine the tunnel client runs on.
#
# Point the hostname at the tunnel with a CNAME to nubuluscloud_tunnel.produccion.cname_target,
# or nothing arrives here at all.
resource "nubuluscloud_tunnel_route" "web" {
  tunnel_id = nubuluscloud_tunnel.produccion.id

  type     = "host"
  hostname = "app.ejemplo.com"

  upstream_host = "127.0.0.1"
  upstream_port = 8080
}

# Only the requests under /api, sent to a different upstream. strip_prefix
# removes /api before the request arrives, so the upstream sees /v1/... rather
# than /api/v1/...
#
# A lower priority wins, so this is matched before the host route above.
resource "nubuluscloud_tunnel_route" "api" {
  tunnel_id = nubuluscloud_tunnel.produccion.id

  type        = "path"
  hostname    = "app.ejemplo.com"
  path_prefix = "/api"

  upstream_host = "127.0.0.1"
  upstream_port = 9000
  strip_prefix  = true
  priority      = 50
}

# Kept in place but not serving. The hostname stays reserved.
resource "nubuluscloud_tunnel_route" "mantenimiento" {
  tunnel_id = nubuluscloud_tunnel.produccion.id

  type     = "host"
  hostname = "viejo.ejemplo.com"

  upstream_host = "127.0.0.1"
  upstream_port = 8081
  enabled       = false
}
