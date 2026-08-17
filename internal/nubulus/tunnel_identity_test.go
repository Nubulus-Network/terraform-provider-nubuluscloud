package nubulus

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

// A create carrying an external id that already exists answers 200 with the
// existing tunnel and NO credentials, where a real create answers 201 with
// them. Everything a caller does about that hangs off telling the two apart,
// so this pins the distinction rather than the status code alone.
func TestCreateAdoptsAnExistingExternalID(t *testing.T) {
	var got CreateTunnelInput

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v2/tunnels" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("the body was not the input: %v (%s)", err, body)
		}

		// What the API answers on an adoption: identity, no secrets, 200.
		writeJSON(t, w, http.StatusOK, map[string]any{
			"tunnel_id":        "tun-existing",
			"tunnel_token":     "",
			"tunnel_subdomain": "tun-existing.example.net",
			"cname_target":     "tun-existing.example.net",
			"wireguard_ip":     "10.0.0.9",
			"instructions":     "este túnel ya existía",
			"adopted":          true,
		})
	}))

	out, err := client.Tunnel.CreateTunnel(t.Context(), CreateTunnelInput{
		Name:       "production",
		ExternalID: "cr-uid-1",
	})
	if err != nil {
		t.Fatalf("CreateTunnel: %v", err)
	}

	if got.ExternalID != "cr-uid-1" || got.Name != "production" {
		t.Errorf("the input did not reach the request: %+v", got)
	}
	if !out.Adopted {
		t.Fatal("Adopted was not decoded; a caller cannot tell this from a real create")
	}
	if out.TunnelID != "tun-existing" {
		t.Errorf("TunnelID = %q", out.TunnelID)
	}
	// The half that makes the difference matter: an adopted tunnel cannot be
	// run with what came back.
	if out.TunnelToken != "" || out.WireGuard.Interface.PrivateKey != "" {
		t.Error("an adoption must not carry credentials")
	}
}

// A zero input has to produce the request the API accepted before either field
// existed. `omitempty` on both is what does it, and it is easy to lose: drop
// the tag and every create starts sending an empty external id, which is a
// different thing from sending none.
func TestCreateWithNoIdentitySendsAnEmptyObject(t *testing.T) {
	var raw string

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		raw = string(body)
		writeJSON(t, w, http.StatusCreated, map[string]any{
			"tunnel_id": "tun-1", "tunnel_token": "tok", "adopted": false,
		})
	}))

	if _, err := client.Tunnel.CreateTunnel(t.Context(), CreateTunnelInput{}); err != nil {
		t.Fatalf("CreateTunnel: %v", err)
	}

	if raw != "{}" {
		t.Errorf("body = %s, want {}; an empty identity must not be sent as empty strings", raw)
	}
}

// Rotating is the only way to get a credential for a tunnel that already
// exists, so it is the only way an adopted tunnel becomes usable.
func TestRotateToken(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v2/tunnels/tun-1/rotate-token" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			return
		}
		writeJSON(t, w, http.StatusOK, map[string]any{
			"tunnel_id": "tun-1", "tunnel_token": "tok-new",
		})
	}))

	out, err := client.Tunnel.RotateToken(t.Context(), "tun-1")
	if err != nil {
		t.Fatalf("RotateToken: %v", err)
	}
	if out.TunnelID != "tun-1" || out.TunnelToken != "tok-new" {
		t.Errorf("result = %+v", out)
	}
}

// The lookup answers an empty list rather than a 404, so "there is none" must
// come back as no tunnel and NO error. Treating it as a failure would turn the
// ordinary case, asking before creating, into a broken apply.
func TestFindTunnelByExternalID(t *testing.T) {
	for _, tc := range []struct {
		name      string
		data      []map[string]any
		wantFound bool
	}{
		{
			name: "found",
			data: []map[string]any{{
				"id": "tun-1", "account_id": "acct_01", "external_id": "cr-uid-1",
				"tunnel_subdomain": "tun-1.example.net", "status": "active",
				"route_count": 3,
			}},
			wantFound: true,
		},
		{name: "not found", data: []map[string]any{}, wantFound: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var query string

			client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				query = r.URL.Query().Get("external_id")
				writeJSON(t, w, http.StatusOK, map[string]any{
					"data": tc.data, "limit": 20, "offset": 0,
				})
			}))

			found, err := client.Tunnel.FindTunnelByExternalID(t.Context(), "cr-uid-1")
			if err != nil {
				t.Fatalf("FindTunnelByExternalID: %v", err)
			}
			if query != "cr-uid-1" {
				t.Errorf("the filter did not reach the query string: %q", query)
			}

			switch {
			case tc.wantFound && found == nil:
				t.Fatal("the tunnel was not returned")
			case !tc.wantFound && found != nil:
				t.Fatalf("nothing matched but a tunnel came back: %+v", found)
			}

			if tc.wantFound {
				if found.ID != "tun-1" {
					t.Errorf("ID = %q", found.ID)
				}
				if found.ExternalID != "cr-uid-1" {
					t.Errorf("ExternalID = %q, want it decoded from the listing", found.ExternalID)
				}
				if found.RouteCount != 3 {
					t.Errorf("RouteCount = %d", found.RouteCount)
				}
			}
		})
	}
}
