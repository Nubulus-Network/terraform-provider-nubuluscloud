terraform {
  required_providers {
    nubuluscloud = {
      source = "nubulus-network/nubuluscloud"
    }
  }
}

# Both halves come from an application token, created in the panel at
# /dashboard/account/tokens. The secret is shown once, when the token is
# created, so it is read from the environment rather than written here:
#
#   export NUBULUS_CLIENT_ID=...
#   export NUBULUS_CLIENT_SECRET=...
provider "nubuluscloud" {}
