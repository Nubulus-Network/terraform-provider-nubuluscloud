## 0.2.1 (17 de agosto de 2026)

NOTES:

* Wording only, in every resource and data source description, the README, this
  file and the examples. No attribute, default or behaviour changed, and no
  configuration needs editing.

  It gets a release of its own rather than riding along with the next change
  because the descriptions are what the registry renders, so until a version
  ships with them the published documentation keeps the old text.

## 0.2.0 (17 de agosto de 2026)

BREAKING CHANGES:

* provider: `token_url`, `project_id`, `dns_endpoint` and `tunnel_endpoint` are
  no longer arguments of the `provider` block. They describe a hosted service
  rather than a preference: there is one production, and no other value for
  them is useful against it. And `token_url` in particular is where the client
  id and secret are sent, so a configuration file no longer gets to redirect
  them. A configuration that sets any of the four now fails with "Unsupported
  argument"; remove the line. The four are still read from `NUBULUS_TOKEN_URL`,
  `NUBULUS_PROJECT_ID`, `NUBULUS_DNS_ENDPOINT` and `NUBULUS_TUNNEL_ENDPOINT`,
  which is what points a build at a test environment; unset, the compiled-in
  defaults apply as before. The provider block now takes `client_id` and
  `client_secret` and nothing else.

FEATURES:

* **New resource:** `nubuluscloud_tunnel`, an outbound WireGuard connection
  from a machine of yours to the platform. Nothing about it is configurable:
  the address, the key pair and the credential are all issued by the platform,
  and the only things you choose are a `name` and an `external_id` to recognise
  it by.

  Two properties of it are worth knowing before you write one:

  * `tunnel_token` and `wireguard_private_key` are issued **once**, when the
    tunnel is created, and can never be read back. They live in the state file
    and nowhere else. A tunnel brought in with `terraform import` has neither,
    and no amount of refreshing recovers them.

  * Setting `external_id` is what makes an apply repeatable. Without it, an
    apply interrupted between creating the tunnel and writing the state leaves
    one behind that nothing can find again, holding an address from the pool
    and a credential nobody ever saw, and the next apply makes another. With
    it, the next apply recognises the tunnel and, by default, **stops with an
    explanation** rather than taking it over: the provider cannot tell your own
    interrupted apply from a tunnel that is up and carrying traffic under the
    same identifier. `adopt_existing = true` takes it over and issues a new
    credential, which stops anything still running on the old one.

* **New resource:** `nubuluscloud_tunnel_route`, which sends requests for a hostname,
  optionally only those under a path, through a tunnel to an upstream reachable
  from the machine running the tunnel client.

  `type`, `hostname` and `path_prefix` replace the route when changed, because
  the API has no way to edit them. Everything else (the upstream, `strip_prefix`,
  `priority` and `enabled`) is updated in place.

  Two of them cannot be set while the route is being created, which the provider
  handles rather than passing on: a new route is always enabled, and a
  `priority` of `0` is read as "unset" and stored as 100. A create that asked
  for either issues the correcting update itself, so `enabled = false` and
  `priority = 0` mean what they say instead of producing a permanent diff.

  `hostname` is unique across the whole platform rather than per account, so a
  collision is with somebody else's route and cannot be inspected or resolved
  from your side. The error says so.

* **New data source:** `nubuluscloud_tunnels`, the tunnels of the account,
  optionally narrowed to one `external_id`. It never carries `tunnel_token` or
  the WireGuard private key: those are issued once, at creation, and no read
  returns them.

ENHANCEMENTS:

* Errors are now explained by the code the API returns rather than by the HTTP
  status where the two disagree, which is what lets a malformed request be
  reported as one whatever status it arrives with.

## 0.1.1 (14 de agosto de 2026)

BUG FIXES:

* `nubuluscloud_dns_rrset`: accept record names whose labels start with an
  underscore, as RFC 8552 reserves them: `_dmarc`, `_domainkey` (DKIM),
  `_acme-challenge` and SRV records were refused at plan time. A zone name keeps
  the stricter RFC 1123 rule.
* `nubuluscloud_dns_zone`: keep `verification_txt_host` and
  `verification_txt_value` in state after the zone has been verified. The API
  stops reporting the challenge once there is nothing left to prove, which made
  every plan, apply and destroy after a successful verification fail with
  "Invalid template interpolation value" for the very configuration the
  documentation recommends.

## 0.1.0 (14 de agosto de 2026)

FEATURES:

* **New Resource:** `nubuluscloud_dns_zone`
* **New Resource:** `nubuluscloud_dns_zone_verification`
* **New Resource:** `nubuluscloud_dns_rrset`
* **New Data Source:** `nubuluscloud_dns_zone`
* **New Data Source:** `nubuluscloud_dns_zones`
