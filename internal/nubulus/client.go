// Package nubulus is the HTTP client the provider talks to the platform with.
//
// It is deliberately a separate package from internal/provider: the resources
// deal in Terraform types and diagnostics, this one deals in JSON and status
// codes, and keeping the two apart is what makes the client testable against an
// httptest server without a Terraform process anywhere near it.
package nubulus

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

// The defaults of every endpoint the provider talks to. They are values rather
// than constants baked into the request builders so that a second instance —
// a staging identity provider, a local API — is an environment variable and not
// a release. They are not configurable from a Terraform configuration: see
// clientConfig in internal/provider.
const (
	DefaultTokenURL       = "https://idp.nubulusnetwork.es/oauth/v2/token"
	DefaultDNSEndpoint    = "https://dns.api.nubulusnetwork.es"
	DefaultTunnelEndpoint = "https://tunel.api.nubulusnetwork.es"

	// DefaultProjectID is the project the access token is scoped to. It is what
	// the audience scope below is built from, and it is the same value that
	// appears in the `aud` of every token the platform issues.
	DefaultProjectID = "385111705782321341"
)

// Scopes builds the scope list a machine token MUST be requested with.
//
// This is the single most expensive thing to get wrong in the whole provider,
// and three of the four fail in silence:
//
//   - without the audience scope the `aud` becomes the username, and with it
//     the role claim disappears — the token looks valid and every service
//     refuses everybody;
//   - without urn:zitadel:iam:user:resourceowner there is no resourceowner:id,
//     which is the claim the services resolve the account from;
//   - without urn:zitadel:iam:org:projects:roles — "projects", PLURAL, matching
//     neither of the two names the role claim itself goes by — the token comes
//     back with a correct audience, a correct resource owner and NO role claim
//     at all, so the API refuses every request made with it. A token issued for
//     a person carries the roles without asking; one issued for a machine does
//     not.
//
// The same four scopes come back in the response that creates an application
// token, which is the authority if they ever change.
func Scopes(projectID string) []string {
	return []string{
		"openid",
		"urn:zitadel:iam:org:project:id:" + projectID + ":aud",
		"urn:zitadel:iam:user:resourceowner",
		"urn:zitadel:iam:org:projects:roles",
	}
}

// Config is everything the client is built from.
type Config struct {
	ClientID     string
	ClientSecret string
	TokenURL     string
	ProjectID    string

	DNSEndpoint    string
	TunnelEndpoint string

	UserAgent string

	// HTTPClient replaces the whole authenticated transport. It exists for the
	// tests, which point the client at an httptest server and have no token
	// endpoint to get a token from.
	HTTPClient *http.Client
}

// Client is the entry point: one authenticated transport, one typed client per
// service behind it.
type Client struct {
	// DNS is the DNS API. It is public because a nil check on it is how a
	// resource would notice a misconfigured provider.
	DNS *DNSClient
	// Tunnel is the tunnel API, behind the same authenticated transport.
	Tunnel *TunnelClient
}

// New builds the client, minting nothing: the first token is fetched lazily by
// the first request, so a provider block with bad credentials fails on the
// first API call with a message about that call rather than at configure time
// with one about OAuth.
func New(ctx context.Context, cfg Config) (*Client, error) {
	if cfg.TokenURL == "" {
		cfg.TokenURL = DefaultTokenURL
	}
	if cfg.ProjectID == "" {
		cfg.ProjectID = DefaultProjectID
	}
	if cfg.DNSEndpoint == "" {
		cfg.DNSEndpoint = DefaultDNSEndpoint
	}
	if cfg.TunnelEndpoint == "" {
		cfg.TunnelEndpoint = DefaultTunnelEndpoint
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		if cfg.ClientID == "" || cfg.ClientSecret == "" {
			return nil, fmt.Errorf("client_id and client_secret are required")
		}
		oauthCfg := &clientcredentials.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			TokenURL:     cfg.TokenURL,
			Scopes:       Scopes(cfg.ProjectID),
			// Zitadel takes the credentials in the body. Letting oauth2
			// auto-detect costs a failed probe on every provider start, and the
			// failure is logged nowhere the user will look.
			AuthStyle: oauth2.AuthStyleInParams,
		}
		// THE TOKEN SOURCE MUST NOT CAPTURE THIS CONTEXT'S CANCELLATION, and
		// getting that wrong looks like a network fault rather than a bug.
		//
		// oauth2 keeps the context it is built with and reuses it for every
		// token fetch, but the one handed to the provider is cancelled the
		// moment configuration returns — long before the first resource asks
		// for anything. The result is every single call failing with "context
		// canceled" on the *token endpoint*, which reads as "the identity
		// provider is unreachable" and sends you to check DNS and firewalls.
		//
		// WithoutCancel keeps the values and drops the cancellation, which is
		// exactly the half that is wanted here. Per-request deadlines still
		// apply: those come from the request context, not from this one.
		tokenCtx := context.WithValue(context.WithoutCancel(ctx), oauth2.HTTPClient, &http.Client{
			Timeout: 30 * time.Second,
		})
		httpClient = oauthCfg.Client(tokenCtx)
		httpClient.Timeout = 60 * time.Second
	}

	return &Client{
		DNS: &DNSClient{service: service{
			base:      strings.TrimSuffix(cfg.DNSEndpoint, "/"),
			http:      httpClient,
			userAgent: cfg.UserAgent,
		}},
		Tunnel: &TunnelClient{service: service{
			base:      strings.TrimSuffix(cfg.TunnelEndpoint, "/"),
			http:      httpClient,
			userAgent: cfg.UserAgent,
		}},
	}, nil
}

// service is one base URL plus the authenticated transport.
type service struct {
	base      string
	http      *http.Client
	userAgent string
}

// do performs a request and decodes the answer.
//
// out may be nil, in which case the body is drained and discarded — which is
// not the same as ignoring it: a connection whose body is never read cannot be
// reused, and a provider makes a lot of small requests.
func (s *service) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		encoded, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("encoding the request body: %w", err)
		}
		body = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, s.base+path, body)
	if err != nil {
		return fmt.Errorf("building the request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if s.userAgent != "" {
		req.Header.Set("User-Agent", s.userAgent)
	}

	resp, err := s.http.Do(req)
	if err != nil {
		return &TransportError{Method: method, URL: s.base + path, Err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return parseAPIError(method, s.base+path, resp)
	}

	if out == nil || resp.StatusCode == http.StatusNoContent {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decoding the answer of %s %s: %w", method, path, err)
	}
	return nil
}
