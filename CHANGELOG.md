## Unreleased

BREAKING CHANGES:

* provider: `token_url`, `project_id`, `dns_endpoint` and `tunnel_endpoint` are
  no longer arguments of the `provider` block. They describe a hosted service
  rather than a preference — there is one production, and no other value for
  them is useful against it — and `token_url` in particular is where the client
  id and secret are sent, so a configuration file no longer gets to redirect
  them. A configuration that sets any of the four now fails with "Unsupported
  argument"; remove the line. The four are still read from `NUBULUS_TOKEN_URL`,
  `NUBULUS_PROJECT_ID`, `NUBULUS_DNS_ENDPOINT` and `NUBULUS_TUNNEL_ENDPOINT`,
  which is what points a build at a test environment; unset, the compiled-in
  defaults apply as before. The provider block now takes `client_id` and
  `client_secret` and nothing else.

FEATURES:

* **New resource:** `nubuluscloud_tunnel` — an outbound WireGuard connection
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
    one behind that nothing can find again — holding an address from the pool
    and a credential nobody ever saw — and the next apply makes another. With
    it, the next apply recognises the tunnel and, by default, **stops with an
    explanation** rather than taking it over: the provider cannot tell your own
    interrupted apply from a tunnel that is up and carrying traffic under the
    same identifier. `adopt_existing = true` takes it over and issues a new
    credential, which stops anything still running on the old one.

ENHANCEMENTS:

* Errors are now explained by the code the API returns rather than by the HTTP
  status where the two disagree, which is what lets a malformed request be
  reported as one whatever status it arrives with.

## 0.1.1 (14 de agosto de 2026)

BUG FIXES:

* `nubuluscloud_dns_rrset`: accept record names whose labels start with an
  underscore, as RFC 8552 reserves them — `_dmarc`, `_domainkey` (DKIM),
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
