# Tunnels are imported by id.
#
# The import is INCOMPLETE and cannot be otherwise: tunnel_token and
# wireguard_private_key are issued once, at creation, and the API will not hand
# them out again. An imported tunnel carries neither, and the only way to get a
# working credential for it is to issue a new one — which stops whatever is
# using the old.
#
# So import a tunnel when whatever runs it already has its credential.
terraform import nubuluscloud_tunnel.produccion 8f3c1d20-0c4a-4b1e-9a77-2f1b6c5d4e30
