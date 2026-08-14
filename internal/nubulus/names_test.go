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

func TestIsApex(t *testing.T) {
	if !IsApex("ejemplo.com.", "ejemplo.com") {
		t.Error("the zone name itself is the apex")
	}
	if IsApex("www.ejemplo.com.", "ejemplo.com") {
		t.Error("a subdomain is not the apex")
	}
}
