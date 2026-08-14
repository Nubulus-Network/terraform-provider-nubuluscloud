## 0.1.1 (Unreleased)

BUG FIXES:

* `nubuluscloud_dns_rrset`: accept record names whose labels start with an
  underscore, as RFC 8552 reserves them — `_dmarc`, `_domainkey` (DKIM),
  `_acme-challenge` and SRV records were refused at plan time. A zone name keeps
  the stricter RFC 1123 rule.

## 0.1.0 (14 de agosto de 2026)

FEATURES:

* **New Resource:** `nubuluscloud_dns_zone`
* **New Resource:** `nubuluscloud_dns_zone_verification`
* **New Resource:** `nubuluscloud_dns_rrset`
* **New Data Source:** `nubuluscloud_dns_zone`
* **New Data Source:** `nubuluscloud_dns_zones`
