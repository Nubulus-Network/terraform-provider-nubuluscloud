package nubulus

import (
	"regexp"
	"strings"
)

// ─────────────────────────────────────────────────────────────────────────────
// Names, spelled the way the API spells them.
//
// The rules here are a copy of the ones the API enforces, and the copy is on
// purpose: the provider has to be able to say "that is not a name" during
// `terraform plan`, when it has not talked to anything yet. What it must NOT do
// is enforce anything the service does not: a provider stricter than the API
// refuses configurations that would have worked, while a looser one only moves
// the error later.
// ─────────────────────────────────────────────────────────────────────────────

// labelRe matches a single RFC 1123 label, lowercase.
//
// Numeric labels pass, which is not an accident: the RFC 952 rule that a label
// must start with a letter would reject every reverse zone there is, and
// reverse zones are ordinary zones.
var labelRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// recordLabelRe is labelRe plus the leading underscore RFC 8552 reserves for
// service discovery: `_dmarc`, `_acme-challenge`, the `_domainkey` of every DKIM
// key, the `_sip._tcp` of every SRV.
//
// It applies to the name of a record and never to the name of a zone. A zone is
// registered and delegated as a hostname-shaped name, where RFC 1123 is the
// right rule; a record name is any name that can appear in the DNS. The
// underscore is only ever the first character of a label. RFC 8552 defines
// these as a prefix on an otherwise ordinary name, not as a character that
// became legal everywhere.
var recordLabelRe = regexp.MustCompile(`^_?[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// maxLabelLen is the DNS limit on one label, in octets. labelRe encodes it on
// its own; recordLabelRe would allow 64 with the underscore in front, so the
// bound is checked separately.
const maxLabelLen = 63

// NormalizeZoneName lowercases and trims a zone name and reports whether it is
// a plausible one. The result carries no trailing dot, which is how zone names
// are spelled everywhere except inside an RRset.
func NormalizeZoneName(raw string) (string, bool) {
	name := strings.ToLower(strings.TrimSpace(raw))
	name = strings.TrimSuffix(name, ".")
	if name == "" || len(name) > 253 {
		return "", false
	}
	if strings.ContainsAny(name, "/:@ ") {
		return "", false
	}
	labels := strings.Split(name, ".")
	if len(labels) < 2 {
		return "", false
	}
	for _, l := range labels {
		if len(l) > maxLabelLen || !labelRe.MatchString(l) {
			return "", false
		}
	}
	return name, true
}

// validRecordName reports whether every label of an already-qualified owner name
// is one a record may carry. It is NormalizeZoneName's loop with recordLabelRe
// in place of labelRe, and it exists so that the difference between what a zone
// may be called and what a record may be called is exactly one regexp.
func validRecordName(name string) bool {
	trimmed := strings.TrimSuffix(name, ".")
	if trimmed == "" || len(trimmed) > 253 {
		return false
	}
	if strings.ContainsAny(trimmed, "/:@ ") {
		return false
	}
	labels := strings.Split(trimmed, ".")
	if len(labels) < 2 {
		return false
	}
	for _, l := range labels {
		if len(l) > maxLabelLen || !recordLabelRe.MatchString(l) {
			return false
		}
	}
	return true
}

// NormalizeOwnerName lowercases an owner name and gives it the trailing dot.
//
// The dot is kept because it is what the DNS means. A name without it is
// relative to an origin the reader has to guess, and a wrong guess silently
// produces a name with the zone appended to it twice.
func NormalizeOwnerName(raw string) string {
	name := strings.ToLower(strings.TrimSpace(raw))
	if name == "" {
		return ""
	}
	if !strings.HasSuffix(name, ".") {
		name += "."
	}
	return name
}

// QualifyName resolves the name of a record against its zone, and it is the one
// piece of convenience the provider adds on top of the API.
//
// The API takes fully-qualified owner names only and refuses anything outside
// the zone. Writing every record as "www.example.com." in HCL, next to a zone
// that already says "example.com", is repetition that invites typos, so all
// four of these mean the same record:
//
//	"@"                    → example.com.
//	"www"                  → www.example.com.
//	"www.example.com"      → www.example.com.
//	"www.example.com."     → www.example.com.
//
// A wildcard is a legitimate label and passes through both forms ("*" and
// "*.example.com"). ok is false only for a name that cannot be made to sit
// inside the zone at all, which is the one case worth an error at plan time.
func QualifyName(name, zone string) (string, bool) {
	zoneName, ok := NormalizeZoneName(zone)
	if !ok {
		return "", false
	}
	apex := zoneName + "."

	raw := strings.ToLower(strings.TrimSpace(name))
	if raw == "" {
		return "", false
	}
	if raw == "@" {
		return apex, true
	}

	qualified := raw
	if !strings.HasSuffix(qualified, ".") {
		// Relative unless it already spells the zone out. Both spellings are
		// common in zone files and in other providers, so both are accepted.
		if qualified == zoneName || strings.HasSuffix(qualified, "."+zoneName) {
			qualified += "."
		} else {
			qualified = qualified + "." + apex
		}
	}

	if qualified != apex && !strings.HasSuffix(qualified, "."+apex) {
		return "", false
	}
	if len(qualified) > 254 {
		return "", false
	}

	// The labels are validated with the wildcard taken off first: "*" is not an
	// RFC 1123 label and is nonetheless exactly what a wildcard record is. And
	// they are validated as a *record* name, which is why `_dmarc` and
	// `*._domainkey` pass here and would not pass as a zone name.
	check := strings.TrimPrefix(qualified, "*.")
	if !validRecordName(check) {
		return "", false
	}

	return qualified, true
}

// IsApex reports whether a qualified owner name is the zone apex, the place
// where the SOA and the NS record set live, and therefore where several rules
// change.
func IsApex(qualifiedName, zone string) bool {
	zoneName, ok := NormalizeZoneName(zone)
	if !ok {
		return false
	}
	return NormalizeOwnerName(qualifiedName) == zoneName+"."
}

// ManagedAtApex are the record sets the platform owns at the apex. A customer who
// deletes the NS set of their own zone takes it off the internet, and the
// request to put it back travels over the DNS they just broke.
//
// NS *below* the apex is a delegation of a subzone, which is an ordinary thing
// to want and is not managed by us.
var ManagedAtApex = map[string]struct{}{
	"SOA": {},
	"NS":  {},
}

// ForbiddenTypes are never writable through the API by anybody. The DNSSEC ones
// belong to the name server when signing is turned on, and a hand-written RRSIG
// can only be wrong; DNAME rewrites a whole subtree; the rest are not records
// at all.
var ForbiddenTypes = map[string]struct{}{
	"DNSKEY": {}, "RRSIG": {}, "NSEC": {}, "NSEC3": {}, "NSEC3PARAM": {},
	"CDS": {}, "CDNSKEY": {}, "DNAME": {},
	"AXFR": {}, "IXFR": {}, "ANY": {}, "OPT": {}, "TSIG": {}, "TKEY": {},
}

// The bounds the API enforces on a record set. A TTL of 0 on a popular name is
// a denial of service against our own name servers written by somebody who did
// not mean it; a week is the customary ceiling, past which a mistake is
// unfixable for a week.
const (
	MinTTL         = 60
	MaxTTL         = 604800
	MaxRRsetValues = 100
)
