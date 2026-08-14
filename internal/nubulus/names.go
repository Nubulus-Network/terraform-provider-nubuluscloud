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
// is enforce anything the service does not — a provider stricter than the API
// refuses configurations that would have worked, while a looser one only moves
// the error later.
// ─────────────────────────────────────────────────────────────────────────────

// labelRe matches a single RFC 1123 label, lowercase.
//
// Numeric labels pass, which is not an accident: the RFC 952 rule that a label
// must start with a letter would reject every reverse zone there is, and
// reverse zones are ordinary zones.
var labelRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

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
		if !labelRe.MatchString(l) {
			return "", false
		}
	}
	return name, true
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
	// RFC 1123 label and is nonetheless exactly what a wildcard record is.
	check := strings.TrimPrefix(qualified, "*.")
	if _, ok := NormalizeZoneName(strings.TrimSuffix(check, ".")); !ok {
		return "", false
	}

	return qualified, true
}

// IsApex reports whether a qualified owner name is the zone apex — the place
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
