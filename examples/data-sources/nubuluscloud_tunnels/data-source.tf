# Every tunnel of the account.
data "nubuluscloud_tunnels" "todos" {}

output "resumen" {
  value = [
    for t in data.nubuluscloud_tunnels.todos.tunnels : {
      id     = t.id
      name   = t.name
      estado = t.online_status
      rutas  = t.route_count
    }
  ]
}

# Or just the one carrying an identifier of yours. A tunnel that is not there is
# an empty list rather than an error, so this is also how to ask whether it
# exists at all.
data "nubuluscloud_tunnels" "produccion" {
  external_id = "produccion-eu"
}
