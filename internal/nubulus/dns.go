package nubulus

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DNSClient is the DNS API. Every route it uses is scoped to an account: the
// account is resolved from the token and never travels in a request, which is
// why nothing here takes an account id.
type DNSClient struct {
	service
}

// Zone is the ownership record of a zone: who it belongs to, whether control of
// the name was proven, and what state the registration is in. The records
// themselves live on the name servers.
type Zone struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Source string `json:"source"` // neodigit | external
	Status string `json:"status"` // pending_verification | active | suspended

	AccountID     string     `json:"account_id"`
	VerifiedAt    *time.Time `json:"verified_at,omitempty"`
	ReservedUntil *time.Time `json:"reserved_until,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	CreatedBy     string     `json:"created_by"`
}

// ZoneDetail is a Zone plus what had to be read from the primary.
//
// PrimaryError is the field that stops this being a plain Zone: a failed
// transfer is reported IN the answer and not as a status, because the
// registration is real information that does not stop being true when ns1 is
// unreachable. A provider that dropped it would show a zone with no serial and
// no way to say why.
type ZoneDetail struct {
	Zone         *Zone             `json:"zone"`
	Serial       *uint32           `json:"serial,omitempty"`
	Nameservers  []string          `json:"nameservers,omitempty"`
	ReadAt       *time.Time        `json:"read_at,omitempty"`
	PrimaryError string            `json:"primary_error,omitempty"`
	Verification *ZoneVerification `json:"verification,omitempty"`
}

// ZoneVerification is the challenge and the state of the proof. It is present
// on a zone that still has something to prove, and empty on one that never did
// (a Neodigit domain already assigned to the account).
type ZoneVerification struct {
	Zone     string `json:"zone"`
	Status   string `json:"status"`
	Source   string `json:"source"`
	Required bool   `json:"required"`

	TXTRecordHost  string     `json:"txt_record_host,omitempty"`
	TXTRecordValue string     `json:"txt_record_value,omitempty"`
	Nameservers    []string   `json:"nameservers"`
	ReservedUntil  *time.Time `json:"reserved_until,omitempty"`
	VerifiedAt     *time.Time `json:"verified_at,omitempty"`
}

// VerificationResult is the outcome of one attempt.
//
// An unsuccessful attempt is a 200 with Verified false, never an error status:
// the overwhelmingly common case is asking too soon, while the negative cache
// of the parent zone still answers NXDOMAIN for the challenge name.
type VerificationResult struct {
	Zone     string `json:"zone"`
	Status   string `json:"status"`
	Verified bool   `json:"verified"`

	Method     string    `json:"method,omitempty"`
	ReasonCode string    `json:"reason_code,omitempty"`
	Reason     string    `json:"reason,omitempty"`
	CheckedAt  time.Time `json:"checked_at"`
}

// RRset is a name + type + TTL + the set of values sharing them. It is the unit
// of every write: RFC 2136 operates on RRsets, so changing one of three A
// records means sending all three.
type RRset struct {
	Name   string   `json:"name"`
	Type   string   `json:"type"`
	TTL    uint32   `json:"ttl"`
	Values []string `json:"values"`
}

// ZoneContent is a whole zone as read from the primary.
type ZoneContent struct {
	Zone   string    `json:"zone"`
	Serial uint32    `json:"serial"`
	RRsets []RRset   `json:"rrsets"`
	ReadAt time.Time `json:"read_at"`
}

// FindRRset returns the RRset with this name and type, or nil. Both are matched
// in their normalized form, so a caller that stored "WWW.example.com" in state
// still finds it.
func (c *ZoneContent) FindRRset(name, rrtype string) *RRset {
	if c == nil {
		return nil
	}
	name = NormalizeOwnerName(name)
	rrtype = strings.ToUpper(strings.TrimSpace(rrtype))
	for i := range c.RRsets {
		if NormalizeOwnerName(c.RRsets[i].Name) == name && c.RRsets[i].Type == rrtype {
			found := c.RRsets[i]
			found.Values = append([]string(nil), c.RRsets[i].Values...)
			return &found
		}
	}
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Zones
// ─────────────────────────────────────────────────────────────────────────────

// CreateZone claims a zone for the account behind the token.
//
// It does NOT necessarily create anything on the name servers, and a caller
// that assumes otherwise is wrong half the time: a name already assigned to the
// account is created and active immediately, while any other name is RESERVED
// with a challenge and nothing exists on the name servers until control of it
// has been proven. The answer says which happened.
func (c *DNSClient) CreateZone(ctx context.Context, name string) (*ZoneDetail, error) {
	var out ZoneDetail
	body := map[string]string{"name": name}
	if err := c.do(ctx, http.MethodPost, "/api/v1/zones", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetZone reads one zone of the account.
func (c *DNSClient) GetZone(ctx context.Context, name string) (*ZoneDetail, error) {
	var out ZoneDetail
	if err := c.do(ctx, http.MethodGet, "/api/v1/zones/"+url.PathEscape(name), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListZones lists the zones of the account.
func (c *DNSClient) ListZones(ctx context.Context) ([]Zone, error) {
	var out struct {
		Data []Zone `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/v1/zones", nil, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// DeleteZone removes the zone from the catalogue and thereby from both name
// servers. It is the destructive one: the domain stops resolving.
func (c *DNSClient) DeleteZone(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodDelete, "/api/v1/zones/"+url.PathEscape(name), nil, nil)
}

// Verification returns the challenge and the current state. It checks nothing:
// this is the instructions, not the attempt.
func (c *DNSClient) Verification(ctx context.Context, zone string) (*ZoneVerification, error) {
	var out ZoneVerification
	path := "/api/v1/zones/" + url.PathEscape(zone) + "/verification"
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Verify attempts the proof once. A failed attempt is a normal answer with
// Verified false and a reason, not an error.
func (c *DNSClient) Verify(ctx context.Context, zone string) (*VerificationResult, error) {
	var out VerificationResult
	path := "/api/v1/zones/" + url.PathEscape(zone) + "/verify"
	if err := c.do(ctx, http.MethodPost, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Records
// ─────────────────────────────────────────────────────────────────────────────

// ZoneContent reads the whole zone from the primary.
func (c *DNSClient) ZoneContent(ctx context.Context, zone string) (*ZoneContent, error) {
	var out ZoneContent
	path := "/api/v1/zones/" + url.PathEscape(zone) + "/rrsets"
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetRRset reads one record set.
//
// IT READS THE WHOLE ZONE, because the API has no route for a single record
// set: there is a GET for the zone content and a PUT/DELETE for one record set,
// and nothing in between. The read is cached on the server side, so the cost is
// one HTTP request per refreshed resource rather than one zone transfer.
//
// A missing RRset comes back as an APIError with a 404, so callers can treat it
// exactly like any other missing resource.
func (c *DNSClient) GetRRset(ctx context.Context, zone, name, rrtype string) (*RRset, error) {
	content, err := c.ZoneContent(ctx, zone)
	if err != nil {
		return nil, err
	}
	if found := content.FindRRset(name, rrtype); found != nil {
		return found, nil
	}
	return nil, &APIError{
		Status:  http.StatusNotFound,
		Code:    "RRSET_NOT_FOUND",
		Message: "No " + strings.ToUpper(rrtype) + " record set at " + name,
		Method:  http.MethodGet,
		URL:     c.base + "/api/v1/zones/" + url.PathEscape(zone) + "/rrsets",
	}
}

// PutRRset replaces a record set with these values. It is idempotent: the same
// call twice is the same zone.
func (c *DNSClient) PutRRset(ctx context.Context, zone, name, rrtype string, ttl uint32, values []string) (*RRset, error) {
	body := struct {
		TTL    uint32   `json:"ttl"`
		Values []string `json:"values"`
	}{TTL: ttl, Values: values}

	var out RRset
	err := c.retryOnConflict(ctx, func() error {
		return c.do(ctx, http.MethodPut, rrsetPath(zone, name, rrtype), body, &out)
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteRRset removes a record set.
func (c *DNSClient) DeleteRRset(ctx context.Context, zone, name, rrtype string) error {
	return c.retryOnConflict(ctx, func() error {
		return c.do(ctx, http.MethodDelete, rrsetPath(zone, name, rrtype), nil, nil)
	})
}

// retryOnConflict runs fn again, once, when the write lost a race.
//
// UPDATE_PRECONDITION_FAILED means the service read the record set, wrote
// "change it only if it is still exactly this", and somebody else got there
// first. Nothing is wrong with the request, so the useful response is to
// re-read and try again, which is what the next call does, since the service
// rebuilds the precondition from a fresh read.
//
// It matches on the CODE and not on the 409, because there is a second 409 that
// must never be retried: writing into a zone that is pending verification or
// suspended. That one will still be true a second later.
//
// ONCE, and not in a loop: two writers taking turns would otherwise have the
// provider spinning until the timeout, and the second failure is worth
// surfacing rather than hiding.
func (c *DNSClient) retryOnConflict(ctx context.Context, fn func() error) error {
	err := fn()
	if !IsLostRace(err) {
		return err
	}
	select {
	case <-ctx.Done():
		return err
	case <-time.After(time.Second):
	}
	return fn()
}

func rrsetPath(zone, name, rrtype string) string {
	return "/api/v1/zones/" + url.PathEscape(zone) +
		"/rrsets/" + url.PathEscape(NormalizeOwnerName(name)) +
		"/" + url.PathEscape(strings.ToUpper(strings.TrimSpace(rrtype)))
}
