package nubulus

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

// A create and a read of the same tunnel answer different shapes, and this is
// the test that pins it: the identifier moves from `tunnel_id` to `id`, the
// tunnel arrives nested, and the two secrets exist only in the create.
//
// Anything that assumed one shape for both would come back from the first
// refresh with an empty id and empty credentials, and would then plan a
// replacement of a tunnel that is perfectly fine.
func TestCreateAndGetAnswerDifferentShapes(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v2/tunnels":
			// The create takes no body at all.
			body, _ := io.ReadAll(r.Body)
			if len(body) != 0 {
				t.Errorf("the create sent a body: %q", body)
			}
			writeJSON(t, w, http.StatusCreated, map[string]any{
				"tunnel_id":        "tun-1",
				"tunnel_token":     "tok-secret",
				"tunnel_subdomain": "tun-1.example.net",
				"cname_target":     "tun-1.example.net.",
				"instructions":     "Point the client at the endpoint below.",
				"wireguard_ip":     "10.0.0.7",
				"wireguard": map[string]any{
					"interface": map[string]any{
						"private_key": "private-secret",
						"address":     "10.0.0.7/32",
						"dns":         "10.0.0.1",
					},
					"peer": map[string]any{
						"public_key":           "server-public",
						"endpoint":             "gw.example.net:51820",
						"allowed_ips":          "10.0.0.0/24",
						"persistent_keepalive": 25,
					},
				},
			})

		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/tunnels/tun-1":
			writeJSON(t, w, http.StatusOK, map[string]any{
				"tunnel": map[string]any{
					"id": "tun-1", "account_id": "acct_01", "user_id": "sub-1",
					"tunnel_subdomain": "tun-1.example.net", "wireguard_ip": "10.0.0.7",
					"wireguard_public_key": "client-public",
					"status":               "active", "online_status": "online",
					"created_at": "2026-08-15T10:00:00Z", "updated_at": "2026-08-15T10:00:00Z",
				},
				"routes": []map[string]any{
					{
						"id": "rt-1", "tunnel_id": "tun-1", "type": "host",
						"hostname": "app.example.com", "path_prefix": "/",
						"upstream_host": "127.0.0.1", "upstream_port": 8080,
						"upstream_scheme": "http", "strip_prefix": false,
						"enabled": true, "priority": 100,
						"created_at": "2026-08-15T10:00:00Z", "updated_at": "2026-08-15T10:00:00Z",
					},
				},
			})

		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))

	created, err := client.Tunnel.CreateTunnel(t.Context())
	if err != nil {
		t.Fatalf("CreateTunnel: %v", err)
	}
	if created.TunnelID != "tun-1" {
		t.Errorf("TunnelID = %q, want the value of tunnel_id", created.TunnelID)
	}
	if created.TunnelToken != "tok-secret" {
		t.Errorf("TunnelToken = %q", created.TunnelToken)
	}
	if created.WireGuard.Interface.PrivateKey != "private-secret" {
		t.Errorf("the private key did not survive decoding: %+v", created.WireGuard.Interface)
	}
	if created.WireGuard.Peer.PersistentKeepalive != 25 {
		t.Errorf("keepalive = %d", created.WireGuard.Peer.PersistentKeepalive)
	}

	read, err := client.Tunnel.GetTunnel(t.Context(), "tun-1")
	if err != nil {
		t.Fatalf("GetTunnel: %v", err)
	}
	if read.Tunnel == nil || read.Tunnel.ID != "tun-1" {
		t.Fatalf("the read identifier is `id` and is nested: %+v", read.Tunnel)
	}
	if len(read.Routes) != 1 || read.Routes[0].Hostname != "app.example.com" {
		t.Fatalf("routes = %+v", read.Routes)
	}
	// The whole reason the create answer has to be kept: a read cannot give
	// these back, so nothing may refresh them from here.
	if read.Tunnel.WireGuardPublicKey == "" {
		t.Error("the public key should come back on a read")
	}
}

// The listing is paged, and reading only its first page would show a prefix of
// the account's tunnels as though it were all of them.
func TestListTunnelsFollowsThePages(t *testing.T) {
	const total = 250
	requests := 0

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/api/v2/tunnels" || r.Method != http.MethodGet {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}

		limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
		if err != nil || limit <= 0 {
			t.Fatalf("limit = %q", r.URL.Query().Get("limit"))
		}
		offset, err := strconv.Atoi(r.URL.Query().Get("offset"))
		if err != nil || offset < 0 {
			t.Fatalf("offset = %q", r.URL.Query().Get("offset"))
		}

		// The server caps the page below what was asked for, which a real one
		// is free to do: the walk has to advance by what came back, not by what
		// it requested.
		const serverCap = 60
		if limit > serverCap {
			limit = serverCap
		}

		data := make([]map[string]any, 0, limit)
		for i := offset; i < offset+limit && i < total; i++ {
			data = append(data, map[string]any{
				"id": "tun-" + strconv.Itoa(i), "account_id": "acct_01",
				"status": "active", "route_count": i,
				"created_at": "2026-08-15T10:00:00Z", "updated_at": "2026-08-15T10:00:00Z",
			})
		}
		writeJSON(t, w, http.StatusOK, map[string]any{
			"data": data, "limit": limit, "offset": offset,
		})
	}))

	tunnels, err := client.Tunnel.ListTunnels(t.Context())
	if err != nil {
		t.Fatalf("ListTunnels: %v", err)
	}
	if len(tunnels) != total {
		t.Fatalf("got %d tunnels, want %d: the pagination stopped early", len(tunnels), total)
	}
	if tunnels[0].ID != "tun-0" || tunnels[total-1].ID != "tun-"+strconv.Itoa(total-1) {
		t.Errorf("the pages came back out of order: first=%q last=%q", tunnels[0].ID, tunnels[total-1].ID)
	}
	// route_count sits alongside the embedded tunnel and has to survive it.
	if tunnels[3].RouteCount != 3 {
		t.Errorf("RouteCount = %d, want 3", tunnels[3].RouteCount)
	}
	if requests < 2 {
		t.Errorf("requests = %d: a single page cannot have held them all", requests)
	}
}

// An empty account is one request and no tunnels, not an error.
func TestListTunnelsOnAnEmptyAccount(t *testing.T) {
	requests := 0
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		writeJSON(t, w, http.StatusOK, map[string]any{"data": []any{}, "limit": 100, "offset": 0})
	}))

	tunnels, err := client.Tunnel.ListTunnels(t.Context())
	if err != nil {
		t.Fatalf("ListTunnels: %v", err)
	}
	if len(tunnels) != 0 {
		t.Errorf("tunnels = %d", len(tunnels))
	}
	if requests != 1 {
		t.Errorf("requests = %d, want 1", requests)
	}
}

// A create sends no `enabled`, because the API has none to send: a route is
// born enabled and only an update can turn it off. Anything wanting a disabled
// route creates it and then updates it.
func TestCreateRouteSendsWhatTheAPIAccepts(t *testing.T) {
	var got map[string]any

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v2/tunnels/tun-1/routes" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decoding the request body: %v", err)
		}
		writeJSON(t, w, http.StatusCreated, map[string]any{
			"id": "rt-1", "tunnel_id": "tun-1", "type": "path",
			"hostname": "app.example.com", "path_prefix": "/api",
			"upstream_host": "127.0.0.1", "upstream_port": 8080,
			"upstream_scheme": "https", "strip_prefix": true,
			"enabled": true, "priority": 50,
			"created_at": "2026-08-15T10:00:00Z", "updated_at": "2026-08-15T10:00:00Z",
		})
	}))

	route, err := client.Tunnel.CreateRoute(t.Context(), "tun-1", CreateRouteInput{
		Type: "path", Hostname: "app.example.com", PathPrefix: "/api",
		UpstreamHost: "127.0.0.1", UpstreamPort: 8080, UpstreamScheme: "https",
		StripPrefix: true, Priority: 50,
	})
	if err != nil {
		t.Fatalf("CreateRoute: %v", err)
	}
	if _, ok := got["enabled"]; ok {
		t.Error("the create sent `enabled`, which the API does not accept there")
	}
	if got["priority"] != float64(50) {
		t.Errorf("priority = %v", got["priority"])
	}
	if route.ID != "rt-1" || !route.Enabled {
		t.Errorf("route = %+v", route)
	}
}

// The update is all pointers so that a field nobody touched is left alone. If
// an omitted field were serialized as its zero value, an update of the port
// would blank the upstream host.
func TestUpdateRouteOmitsWhatWasNotSet(t *testing.T) {
	var got map[string]any

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/v2/tunnels/tun-1/routes/rt-1" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decoding the request body: %v", err)
		}
		writeJSON(t, w, http.StatusOK, map[string]any{
			"id": "rt-1", "tunnel_id": "tun-1", "type": "host",
			"hostname": "app.example.com", "path_prefix": "/",
			"upstream_host": "127.0.0.1", "upstream_port": 9090,
			"upstream_scheme": "http", "strip_prefix": false,
			"enabled": false, "priority": 0,
			"created_at": "2026-08-15T10:00:00Z", "updated_at": "2026-08-15T11:00:00Z",
		})
	}))

	port := 9090
	disabled := false
	// Zero, and meant as zero: the update takes it because it arrives as a
	// pointer, while a create would have read the same 0 as "unset".
	priority := 0

	route, err := client.Tunnel.UpdateRoute(t.Context(), "tun-1", "rt-1", UpdateRouteInput{
		UpstreamPort: &port, Enabled: &disabled, Priority: &priority,
	})
	if err != nil {
		t.Fatalf("UpdateRoute: %v", err)
	}

	for _, absent := range []string{"upstream_host", "upstream_scheme", "strip_prefix"} {
		if _, ok := got[absent]; ok {
			t.Errorf("%s was sent although nobody set it", absent)
		}
	}
	if got["upstream_port"] != float64(9090) {
		t.Errorf("upstream_port = %v", got["upstream_port"])
	}
	if got["enabled"] != false {
		t.Errorf("enabled = %v, and false must be sent rather than omitted", got["enabled"])
	}
	if got["priority"] != float64(0) {
		t.Errorf("priority = %v, and an explicit 0 must survive", got["priority"])
	}
	// The fields the API cannot update have no way of being sent.
	for _, never := range []string{"type", "hostname", "path_prefix"} {
		if _, ok := got[never]; ok {
			t.Errorf("%s was sent, and the API does not update it", never)
		}
	}
	if route.UpstreamPort != 9090 || route.Enabled {
		t.Errorf("route = %+v", route)
	}
}

// The route listing is its own envelope, and it is not the paged one.
func TestListRoutes(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/tunnels/tun-1/routes" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		writeJSON(t, w, http.StatusOK, map[string]any{
			"routes": []map[string]any{
				{"id": "rt-1", "tunnel_id": "tun-1", "hostname": "a.example.com", "priority": 10},
				{"id": "rt-2", "tunnel_id": "tun-1", "hostname": "b.example.com", "priority": 20},
			},
			"total": 2,
		})
	}))

	routes, err := client.Tunnel.ListRoutes(t.Context(), "tun-1")
	if err != nil {
		t.Fatalf("ListRoutes: %v", err)
	}
	if len(routes) != 2 || routes[1].Hostname != "b.example.com" {
		t.Fatalf("routes = %+v", routes)
	}
}

// Both deletes answer 204 with no body, which must not be read as a decoding
// failure.
func TestDeletesAcceptNoContent(t *testing.T) {
	var paths []string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		paths = append(paths, r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	}))

	if err := client.Tunnel.DeleteRoute(t.Context(), "tun-1", "rt-1"); err != nil {
		t.Fatalf("DeleteRoute: %v", err)
	}
	if err := client.Tunnel.DeleteTunnel(t.Context(), "tun-1"); err != nil {
		t.Fatalf("DeleteTunnel: %v", err)
	}

	want := []string{"/api/v2/tunnels/tun-1/routes/rt-1", "/api/v2/tunnels/tun-1"}
	for i := range want {
		if paths[i] != want[i] {
			t.Errorf("path %d = %q, want %q", i, paths[i], want[i])
		}
	}
}

// The refusals are recognised by the code in the body and NOT by the status.
// The statuses here are the ones the API sends today, and several of them are
// wrong: every one of these is the caller's request or the caller's state, and
// a 500 reads as "the platform broke, try later".
func TestTunnelRefusalsAreRecognisedByCodeNotStatus(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		code    string
		wantsIn string
	}{
		{"malformed request", http.StatusInternalServerError, CodeInvalidInput, "malformed"},
		{"hostname taken elsewhere", http.StatusInternalServerError, CodeHostnameConflict, "only be routed by one account"},
		{"too many tunnels", http.StatusInternalServerError, CodeQuotaExceeded, "lifts on its own"},
		{"tunnel not active", http.StatusInternalServerError, CodeTunnelInactive, "not active"},
		// The same codes once the statuses are corrected. The answer must not
		// change, which is the whole reason for deciding on the code.
		{"malformed request, corrected", http.StatusBadRequest, CodeInvalidInput, "malformed"},
		{"hostname taken, corrected", http.StatusConflict, CodeHostnameConflict, "only be routed by one account"},
		{"too many tunnels, corrected", http.StatusTooManyRequests, CodeQuotaExceeded, "lifts on its own"},
		{"tunnel not active, corrected", http.StatusConflict, CodeTunnelInactive, "not active"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeJSON(t, w, tt.status, map[string]string{
					"error": tt.code, "message": "the service's own words",
				})
			}))

			_, err := client.Tunnel.CreateRoute(t.Context(), "tun-1", CreateRouteInput{})
			if err == nil {
				t.Fatal("expected an error")
			}
			if CodeOf(err) != tt.code {
				t.Fatalf("code = %q, want %q", CodeOf(err), tt.code)
			}
			_, detail := Explain("create the route", err)
			if !strings.Contains(detail, tt.wantsIn) {
				t.Errorf("the explanation did not follow the code: %q", detail)
			}
			// The service's own message is never thrown away.
			if !strings.Contains(detail, "the service's own words") {
				t.Errorf("the original message was lost: %q", detail)
			}
		})
	}
}

// A 409 with no code at all still gets the record-set explanation, which is the
// case the code-first ordering must not have broken.
func TestAConflictWithoutACodeKeepsTheOldExplanation(t *testing.T) {
	err := &APIError{Status: http.StatusConflict, Message: "conflict"}
	_, detail := Explain("write the record", err)
	if !strings.Contains(detail, "between being read and being written") {
		t.Errorf("detail = %q", detail)
	}
}

// The tunnel client gets its own base URL, and an unset one falls back to the
// default rather than to the DNS endpoint or to nothing.
func TestTheTunnelEndpointIsSeparateAndHasADefault(t *testing.T) {
	configured, err := New(t.Context(), Config{
		DNSEndpoint:    "https://dns.example",
		TunnelEndpoint: "https://tunnel.example/",
		HTTPClient:     http.DefaultClient,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// The trailing slash is trimmed; every path built from here starts with one.
	if configured.Tunnel.base != "https://tunnel.example" {
		t.Errorf("base = %q", configured.Tunnel.base)
	}
	if configured.DNS.base != "https://dns.example" {
		t.Errorf("the two clients must not share a base: %q", configured.DNS.base)
	}

	defaulted, err := New(t.Context(), Config{HTTPClient: http.DefaultClient})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if defaulted.Tunnel.base != DefaultTunnelEndpoint {
		t.Errorf("base = %q, want the default", defaulted.Tunnel.base)
	}
}
