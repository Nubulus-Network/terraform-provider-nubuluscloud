package nubulus

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The client has to keep working after the context it was built with is
// cancelled, and this is not a hypothetical: Terraform cancels the context it
// hands to a provider's Configure the moment Configure returns, while the token
// source built there is used by every request afterwards.
//
// Getting it wrong fails on the TOKEN endpoint with "context canceled", which
// reads as an unreachable identity provider and sends whoever hits it to check
// DNS, firewalls and credentials — none of which are the problem. It cost a real
// run to find; this test is what stops it coming back.
func TestTheClientSurvivesTheContextItWasBuiltWith(t *testing.T) {
	tokens := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/token" {
			tokens++
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "a-token", "token_type": "Bearer", "expires_in": 3600,
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	client, err := New(ctx, Config{
		ClientID:     "id",
		ClientSecret: "secret",
		TokenURL:     server.URL + "/token",
		DNSEndpoint:  server.URL,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Configure returns, and Terraform cancels its context.
	cancel()

	if _, err := client.DNS.ListZones(context.Background()); err != nil {
		t.Fatalf("the client stopped working when the configure context was cancelled: %v", err)
	}
	if tokens != 1 {
		t.Errorf("token requests = %d, want 1", tokens)
	}
}
