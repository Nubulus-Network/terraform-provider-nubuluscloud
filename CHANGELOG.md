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
