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

NOTES:

* Groundwork for the tunnel resources: the provider now carries a typed client
  for the tunnel API. No resource or data source uses it yet, so nothing that
  can be written in a configuration gains anything from it.

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
