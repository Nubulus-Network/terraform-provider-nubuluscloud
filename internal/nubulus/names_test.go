package nubulus

import "testing"

func TestQualifyName(t *testing.T) {
	const zone = "ejemplo.com"

	cases := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{"apex with at", "@", "ejemplo.com.", true},
		{"apex spelled out", "ejemplo.com", "ejemplo.com.", true},
		{"apex with dot", "ejemplo.com.", "ejemplo.com.", true},
		{"relative label", "www", "www.ejemplo.com.", true},
		{"relative subtree", "a.b", "a.b.ejemplo.com.", true},
		{"absolute without dot", "www.ejemplo.com", "www.ejemplo.com.", true},
		{"absolute with dot", "www.ejemplo.com.", "www.ejemplo.com.", true},
		{"uppercase", "WWW.Ejemplo.COM", "www.ejemplo.com.", true},
		{"wildcard relative", "*", "*.ejemplo.com.", true},
		{"wildcard absolute", "*.ejemplo.com", "*.ejemplo.com.", true},
		{"another zone", "www.otro.com.", "", false},
		{"empty", "", "", false},

		// RFC 8552 labels: mail, certificates and service discovery all live
		// under a first label that starts with an underscore, and every one of
		// these is an ordinary record for an ordinary domain.
		{"dmarc", "_dmarc", "_dmarc.ejemplo.com.", true},
		{"dkim", "sel1._domainkey", "sel1._domainkey.ejemplo.com.", true},
		{"dkim wildcard", "*._domainkey", "*._domainkey.ejemplo.com.", true},
		{"acme challenge", "_acme-challenge", "_acme-challenge.ejemplo.com.", true},
		{"srv, two underscore labels deep", "_sip._tcp", "_sip._tcp.ejemplo.com.", true},
		{"dmarc spelled out", "_dmarc.ejemplo.com.", "_dmarc.ejemplo.com.", true},

		// The underscore is a prefix RFC 8552 gives a meaning to, not a
		// character that is legal anywhere in a label.
		{"underscore inside a label", "exam_ple", "", false},
		{"a bare underscore label", "_", "", false},
		{"a label starting with a hyphen", "-www", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := QualifyName(tc.in, zone)
			if ok != tc.ok {
				t.Fatalf("QualifyName(%q) ok = %v, want %v", tc.in, ok, tc.ok)
			}
			if got != tc.want {
				t.Errorf("QualifyName(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// A name that is absolute but only *looks* like it belongs to the zone must be
// refused rather than pushed inside it. "notejemplo.com." shares a suffix with
// the zone without the label boundary, and appending the zone to it would
// invent a record in a place the configuration never asked for.
func TestQualifyNameRefusesASuffixThatIsNotALabelBoundary(t *testing.T) {
	if got, ok := QualifyName("notejemplo.com.", "ejemplo.com"); ok {
		t.Fatalf("QualifyName accepted a name outside the zone: %q", got)
	}
}

// Reverse zones are the case the label rules have to keep passing: every label
// is numeric, which RFC 952 would have rejected and RFC 1123 allows.
func TestNormalizeZoneNameAcceptsReverseZones(t *testing.T) {
	got, ok := NormalizeZoneName("2.0.192.in-addr.arpa.")
	if !ok {
		t.Fatal("NormalizeZoneName rejected a reverse zone")
	}
	if got != "2.0.192.in-addr.arpa" {
		t.Errorf("got %q", got)
	}
}

// A zone keeps the stricter rule: it is registered and delegated as a
// hostname-shaped name. The provider must not be looser than the API here, and
// the API refuses these.
func TestNormalizeZoneNameRefusesUnderscoreLabels(t *testing.T) {
	for _, in := range []string{"_dmarc.ejemplo.com", "_tcp.ejemplo.com", "exam_ple.com"} {
		if got, ok := NormalizeZoneName(in); ok {
			t.Errorf("NormalizeZoneName(%q) = %q, ok — a zone name is RFC 1123", in, got)
		}
	}
}

// A label is 63 octets, and the underscore counts towards them.
func TestQualifyNameRefusesAnOverlongLabel(t *testing.T) {
	long := "_"
	for len(long) < 64 {
		long += "a"
	}
	if got, ok := QualifyName(long, "ejemplo.com"); ok {
		t.Fatalf("QualifyName accepted a 64-octet label: %q", got)
	}
}

func TestIsApex(t *testing.T) {
	if !IsApex("ejemplo.com.", "ejemplo.com") {
		t.Error("the zone name itself is the apex")
	}
	if IsApex("www.ejemplo.com.", "ejemplo.com") {
		t.Error("a subdomain is not the apex")
	}
}
