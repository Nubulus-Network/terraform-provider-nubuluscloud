# terraform-provider-nubuluscloud

Terraform provider for [Nubulus Cloud](https://nubulusnetwork.es).

It manages **authoritative DNS**: zones served by the Nubulus name servers and the record
sets inside them.

```terraform
terraform {
  required_providers {
    nubuluscloud = {
      source = "nubulus-network/nubuluscloud"
    }
  }
}

provider "nubuluscloud" {} # credentials from the environment

resource "nubuluscloud_dns_zone" "main" {
  name = "ejemplo.com"
}

resource "nubuluscloud_dns_rrset" "www" {
  zone   = nubuluscloud_dns_zone.main.name
  name   = "www"
  type   = "A"
  ttl    = 300
  values = ["203.0.113.10", "203.0.113.11"]
}
```

## Authentication

The provider authenticates with an **application token**: a machine credential that belongs
to the account rather than to a person, created in the panel at `/dashboard/account/tokens`.
It hands back a `client_id` and a `client_secret`, and the secret is shown exactly once.

```sh
export NUBULUS_CLIENT_ID=...
export NUBULUS_CLIENT_SECRET=...
```

Both can also be set in the provider block, and everything else has a default:

| Attribute | Environment | Default |
|---|---|---|
| `client_id` | `NUBULUS_CLIENT_ID` | — |
| `client_secret` | `NUBULUS_CLIENT_SECRET` | — |
| `token_url` | `NUBULUS_TOKEN_URL` | `https://idp.nubulusnetwork.es/oauth/v2/token` |
| `project_id` | `NUBULUS_PROJECT_ID` | the platform's Zitadel project |
| `dns_endpoint` | `NUBULUS_DNS_ENDPOINT` | `https://dns.api.nubulusnetwork.es` |

A personal access token from the identity provider **cannot** be used instead, and it is not
a matter of permissions: a PAT is an encrypted token the services cannot parse, and it is
minted outside the token endpoint so it carries no scopes — which means no project audience
and no role claim, two of the three things every service requires.

### The role of the token is a real limit

The API derives the permission from the HTTP method, so the role chosen when the token was
created decides what it can do:

| Role | Records | Zones |
|---|---|---|
| `owner`, `admin` | yes | create, delete, verify |
| `member` | yes | **no** |
| `viewer` | no | no |

A token that only maintains records can, and should, be created as `member`.

## What it manages

| | |
|---|---|
| `nubuluscloud_dns_zone` | A zone. Created and active immediately for a domain already assigned to the account; **reserved** with a challenge for any other name. |
| `nubuluscloud_dns_zone_verification` | Waits until control of a name has been proven. Records depend on this, not on the zone. |
| `nubuluscloud_dns_rrset` | A record **set**: every record sharing a name and a type. |
| `data.nubuluscloud_dns_zone` | One zone, whether or not Terraform created it. |
| `data.nubuluscloud_dns_zones` | Every zone of the account. |

Two things about the model are worth knowing before writing any of it:

* **The unit is the record set, not the record.** Three A records on `www` are one resource
  with three `values`. This is what the DNS protocol operates on — a change to one value of a
  set rewrites the whole set — so three separate resources would race against each other.
* **A zone for a name registered elsewhere does not exist on the name servers until it is
  verified.** That ordering is a safety property and not a workflow preference: a zone
  created before control is proven answers authoritatively for a name that is not yours. It
  is why `nubuluscloud_dns_zone_verification` exists, and why record resources should depend
  on it rather than on the zone.

## Development

```sh
make build      # go build ./...
make test       # unit tests, no credentials needed
make testacc    # acceptance tests; creates real records, see below
make lint       # golangci-lint
make generate   # regenerate docs/ from the schemas and examples/
```

To run a local build against a real configuration, point Terraform at it with a
`dev_overrides` block in `~/.terraformrc`:

```hcl
provider_installation {
  dev_overrides {
    "nubulus-network/nubuluscloud" = "/home/<you>/go/bin"
  }
  direct {}
}
```

With an override in place `terraform init` is skipped — Terraform says so on every command,
and that is expected.

The acceptance tests create real zones and records. They need credentials plus a zone the
account already owns and does not mind being written into:

```sh
export NUBULUS_CLIENT_ID=... NUBULUS_CLIENT_SECRET=...
export NUBULUS_TEST_ZONE=pruebas.ejemplo.com
make testacc
```

They deliberately do not cover verifying an external zone: that needs a TXT record published
in DNS this provider does not manage, which no test can arrange.

## Known limitations

**Records are refreshed one zone at a time.** The API has no route for a single record set,
so refreshing one reads the zone and filters. The read is cached on the server, so this costs
an HTTP request rather than a zone transfer, but it is why a configuration with many record
sets makes many small requests.

**Underscore labels need a current API.** `_dmarc`, `_domainkey` (DKIM), `_acme-challenge`
and SRV records are accepted by the provider, but an older deployment of the API answers
`400 INVALID_NAME` to them. There is nothing Terraform can do about that: until the API is
updated, those records have to be managed elsewhere.

**Verifying an external zone is not covered by the acceptance tests.** It needs a TXT record
published in DNS this provider does not manage, which no test can arrange.
