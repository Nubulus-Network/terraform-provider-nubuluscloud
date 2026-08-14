package nubulus

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestClient points a client at a test server with no OAuth in the way.
func newTestClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := New(context.Background(), Config{
		DNSEndpoint: server.URL,
		HTTPClient:  server.Client(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return client
}

func writeJSON(t *testing.T, w http.ResponseWriter, status int, body any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		t.Fatalf("encoding the test answer: %v", err)
	}
}

// A zone claimed for an external name comes back pending, with the challenge,
// and that is a SUCCESS. Treating it as a failure is the mistake this test
// exists to stop: the answer carries everything needed to publish the proof.
func TestCreateZoneReturnsThePendingClaim(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/zones" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		writeJSON(t, w, http.StatusCreated, map[string]any{
			"zone": map[string]any{
				"id": "zone_01", "name": "ejemplo.com", "source": "external",
				"status": "pending_verification", "account_id": "acct_01",
				"created_at": "2026-08-14T10:00:00Z",
			},
			"verification": map[string]any{
				"zone": "ejemplo.com", "status": "pending_verification", "source": "external",
				"required":         true,
				"txt_record_host":  "_nubulus-challenge.ejemplo.com",
				"txt_record_value": "token-abc",
				"nameservers":      []string{"ns1.example.net.", "ns2.example.net."},
			},
		})
	}))

	detail, err := client.DNS.CreateZone(context.Background(), "ejemplo.com")
	if err != nil {
		t.Fatalf("CreateZone: %v", err)
	}
	if detail.Zone.Status != "pending_verification" {
		t.Errorf("status = %q", detail.Zone.Status)
	}
	if detail.Verification == nil || detail.Verification.TXTRecordValue != "token-abc" {
		t.Fatalf("the challenge did not survive decoding: %+v", detail.Verification)
	}
}

// A record set that is not in the zone is a 404, built here rather than
// returned by the API: there is no route for a single RRset, so the client
// reads the zone and turns "not in it" into the missing-resource error every
// caller already knows how to handle.
func TestGetRRsetIsANotFoundWhenTheSetIsAbsent(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, map[string]any{
			"zone": "ejemplo.com", "serial": 7, "read_at": "2026-08-14T10:00:00Z",
			"rrsets": []map[string]any{
				{"name": "www.ejemplo.com.", "type": "A", "ttl": 300, "values": []string{"203.0.113.10"}},
			},
		})
	}))

	if _, err := client.DNS.GetRRset(context.Background(), "ejemplo.com", "mail.ejemplo.com.", "A"); !IsNotFound(err) {
		t.Fatalf("expected a not found, got %v", err)
	}

	// The same read finds the set that is there, whatever case it was asked for.
	set, err := client.DNS.GetRRset(context.Background(), "ejemplo.com", "WWW.ejemplo.com", "a")
	if err != nil {
		t.Fatalf("GetRRset: %v", err)
	}
	if len(set.Values) != 1 || set.Values[0] != "203.0.113.10" {
		t.Errorf("values = %v", set.Values)
	}
}

// A lost race is retried once. The prerequisite is rebuilt from a fresh read on
// the server side, so the second attempt is a different request and normally
// succeeds.
func TestPutRRsetRetriesALostRaceOnce(t *testing.T) {
	attempts := 0
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			writeJSON(t, w, http.StatusConflict, map[string]string{
				"error":   CodePreconditionFailed,
				"message": "The record set changed since it was read",
			})
			return
		}
		writeJSON(t, w, http.StatusOK, map[string]any{
			"name": "www.ejemplo.com.", "type": "A", "ttl": 300, "values": []string{"203.0.113.10"},
		})
	}))

	set, err := client.DNS.PutRRset(context.Background(), "ejemplo.com", "www", "A", 300, []string{"203.0.113.10"})
	if err != nil {
		t.Fatalf("PutRRset: %v", err)
	}
	if attempts != 2 {
		t.Errorf("attempts = %d, want 2", attempts)
	}
	if set.TTL != 300 {
		t.Errorf("ttl = %d", set.TTL)
	}
}

// The other 409 must NOT be retried: a zone that is pending verification will
// still be pending a second later, and retrying only delays the error that
// tells the user what to do.
func TestPutRRsetDoesNotRetryAZoneThatIsNotActive(t *testing.T) {
	attempts := 0
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		writeJSON(t, w, http.StatusConflict, map[string]string{
			"error":   CodeZoneNotActive,
			"message": "This zone has not been verified yet",
		})
	}))

	_, err := client.DNS.PutRRset(context.Background(), "ejemplo.com", "www", "A", 300, []string{"203.0.113.10"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1: ZONE_NOT_ACTIVE is not a lost race", attempts)
	}
	if CodeOf(err) != CodeZoneNotActive {
		t.Errorf("code = %q", CodeOf(err))
	}
}

// A refusal from something answering on behalf of the API is not the envelope,
// and losing its body would leave the user with a bare status code.
func TestAnErrorThatIsNotTheEnvelopeKeepsItsBody(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("Access denied"))
	}))

	_, err := client.DNS.ListZones(context.Background())
	if StatusOf(err) != http.StatusForbidden {
		t.Fatalf("status = %d", StatusOf(err))
	}
	_, detail := Explain("list the zones", err)
	if !strings.Contains(detail, "Access denied") {
		t.Errorf("the body was lost: %q", detail)
	}
	if !strings.Contains(detail, "did not come from the API") {
		t.Errorf("a bare 403 should say it did not come from the API: %q", detail)
	}
}

// The three token failures have to be explained as token failures. This one
// pins the least obvious of them.
func TestExplainNamesTheMissingScope(t *testing.T) {
	err := &APIError{Status: http.StatusForbidden, Code: CodeNoAccountRole, Message: "No account role"}
	_, detail := Explain("create the zone", err)
	if !strings.Contains(detail, "urn:zitadel:iam:org:projects:roles") {
		t.Errorf("the missing scope is the whole point of the message: %q", detail)
	}
}
