package nubulus

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// The error codes the provider reacts to by name. The API answers a single
// envelope — {"error": "<CODE>", "message": "<prose>"} — on every route.
const (
	// CodeNoAccountRole is a token with no role claim. It reads like a
	// permission problem and is not one: see Explain.
	CodeNoAccountRole = "NO_ACCOUNT_ROLE"
	// CodeNoAccount is a token whose organization maps to no account.
	CodeNoAccount = "NO_ACCOUNT"
	// CodePreconditionFailed is a DNS write that lost a race: the RFC 2136
	// prerequisite said "only if the record set is still exactly what I read"
	// and it was not. Retried once, in the client.
	CodePreconditionFailed = "UPDATE_PRECONDITION_FAILED"
	// CodeZoneNotActive is a write into a zone that is pending verification or
	// suspended.
	//
	// It is a 409 as well, and that is the trap: retrying it is pointless — the
	// zone will still be pending a second later — and the advice for it is the
	// opposite of the advice for a lost race. Anything that branches on 409
	// alone gets both of these wrong.
	CodeZoneNotActive = "ZONE_NOT_ACTIVE"
	// CodeNotFound is the ordinary missing resource.
	CodeNotFound = "NOT_FOUND"
)

// The codes of the tunnel API, which are matched by NAME and never by status.
//
// That is not a stylistic preference. Some of these currently come back with a
// 5xx even though every one of them is the caller's mistake or the caller's
// state: the classification of the code into a status is being corrected, and
// the provider has to behave the same before and after that lands. The code in
// the body is what has always been right, and it is what consumers are expected
// to read.
//
// A second reason to prefer the code even once the statuses are fixed: two
// unrelated failures share a status. A 409 is a lost write race on a DNS record
// set and a hostname claimed elsewhere on a route, and the advice for them is
// opposite.
const (
	// CodeInvalidInput is a malformed request: a port out of range, a hostname
	// that is not an FQDN, a path prefix that does not start with a slash.
	// Retrying it unchanged never helps.
	CodeInvalidInput = "INVALID_INPUT"
	// CodeHostnameConflict is a route hostname already in use by another
	// account. Hostnames are unique across the whole platform, so this is not
	// something the account that hits it can inspect or resolve on its own.
	CodeHostnameConflict = "HOSTNAME_CONFLICT"
	// CodeQuotaExceeded is the limit on how many tunnels an account may have.
	// Unlike the others it lifts on its own once something is deleted.
	CodeQuotaExceeded = "QUOTA_EXCEEDED"
	// CodeTunnelInactive is any operation on a tunnel that is not active.
	// Routes exist only inside an active tunnel, and retrying will not change
	// that until the tunnel itself does.
	CodeTunnelInactive = "TUNNEL_INACTIVE"
)

// APIError is a 4xx or 5xx with the service's own code and message.
type APIError struct {
	Status  int
	Code    string
	Message string

	Method string
	URL    string
}

func (e *APIError) Error() string {
	switch {
	case e.Code != "":
		return fmt.Sprintf("%s %s: HTTP %d %s: %s", e.Method, e.URL, e.Status, e.Code, e.Message)
	case e.Message != "":
		// No code means the body was not the envelope — something in front of
		// the API answered. Whatever it said is the only clue there is, so it
		// is kept rather than reduced to a status.
		return fmt.Sprintf("%s %s: HTTP %d: %s", e.Method, e.URL, e.Status, e.Message)
	default:
		return fmt.Sprintf("%s %s: HTTP %d", e.Method, e.URL, e.Status)
	}
}

// TransportError is a request that never got an answer: DNS, TLS, timeout, or
// a refused connection. It is kept apart from APIError because the advice is
// completely different — an endpoint that never answers is usually the wrong
// endpoint, or a network that does not let the request out.
type TransportError struct {
	Method string
	URL    string
	Err    error
}

func (e *TransportError) Error() string {
	return fmt.Sprintf("%s %s: %v", e.Method, e.URL, e.Err)
}

func (e *TransportError) Unwrap() error { return e.Err }

// parseAPIError turns a failed response into an APIError, tolerating a body
// that is not the envelope: anything answering on behalf of the API — a proxy,
// a load balancer, an error page — replies in plain text, and losing that text
// would leave the user with a bare status code.
func parseAPIError(method, url string, resp *http.Response) error {
	apiErr := &APIError{Status: resp.StatusCode, Method: method, URL: url}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil || len(raw) == 0 {
		return apiErr
	}

	var envelope struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &envelope); err == nil && envelope.Error != "" {
		apiErr.Code = envelope.Error
		apiErr.Message = envelope.Message
		return apiErr
	}

	apiErr.Message = strings.TrimSpace(string(raw))
	return apiErr
}

// StatusOf returns the HTTP status of err, or 0 when it is not an APIError.
func StatusOf(err error) int {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Status
	}
	return 0
}

// CodeOf returns the service's error code, or "".
func CodeOf(err error) string {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Code
	}
	return ""
}

// IsNotFound reports whether err is a 404.
//
// It is the status and not the code that decides, because the two disagree on
// purpose in one place: a record set that is not there comes back as a 404
// with a code of its own (RRSET_NOT_FOUND).
func IsNotFound(err error) bool { return StatusOf(err) == http.StatusNotFound }

// IsConflict reports whether err is a 409.
func IsConflict(err error) bool { return StatusOf(err) == http.StatusConflict }

// IsLostRace reports whether err is the one 409 that is worth retrying: the
// record set changed between being read and being written.
func IsLostRace(err error) bool {
	return IsConflict(err) && CodeOf(err) == CodePreconditionFailed
}

// Explain turns an error into the two halves of a Terraform diagnostic.
//
// It exists because the honest message from the service sends people to look in
// the wrong place for three of the failures they are most likely to hit. All
// three are properties of the TOKEN, and none of them says so:
//
//   - NO_ACCOUNT_ROLE reads as "this account may not do that" and means the
//     token was minted without the roles scope;
//   - NO_ACCOUNT reads as "not found" and means the organization behind the
//     token maps to no account;
//   - a 403 with no error code did not come from the API at all, so looking for
//     the cause in the credential is looking in the wrong place.
//
// Everything else falls through with the service's own words, which are good.
//
// The cases that match on a CODE come first, and must keep coming first. Two
// failures with nothing in common share a status often enough that deciding on
// the status alone gets one of the pair wrong, and a code that arrives with the
// wrong status — which happens today for several of the tunnel ones — would
// otherwise be explained as whatever that status usually means.
func Explain(action string, err error) (summary, detail string) {
	summary = "Could not " + action

	var transportErr *TransportError
	if errors.As(err, &transportErr) {
		return summary, fmt.Sprintf(
			"%s\n\nThe endpoint did not answer at all. Check that it is reachable from the machine "+
				"running Terraform.", transportErr.Error())
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return summary, err.Error()
	}

	switch {
	case apiErr.Code == CodeNoAccountRole:
		return summary, apiErr.Error() + "\n\n" +
			"This is almost always the token and not the permissions: an application token only " +
			"carries a role claim when it was requested with the scope\n\n" +
			"    urn:zitadel:iam:org:projects:roles\n\n" +
			"(\"projects\", plural). The provider asks for it; a token fetched by hand, or one " +
			"requested against a different project, will not have it."

	case apiErr.Code == CodeNoAccount:
		return summary, apiErr.Error() + "\n\n" +
			"The token is valid but the organization it was issued in does not map to any Nubulus " +
			"account. This usually means the credential belongs to a different environment than the " +
			"endpoint it is being used against."

	case apiErr.Code == CodeZoneNotActive:
		return summary, apiErr.Error() + "\n\n" +
			"Records can only be written into a zone that is active. A zone claimed for a name " +
			"registered elsewhere stays pending until control of it has been proven, and it does not " +
			"exist on the name servers until then — publish the challenge TXT record and add a " +
			"`nubuluscloud_dns_zone_verification` resource that the records depend on."

	case apiErr.Code == CodeInvalidInput:
		return summary, apiErr.Error() + "\n\n" +
			"The request was refused as malformed, so this is the configuration rather than the " +
			"platform, whatever the status code says. The message above names the field."

	case apiErr.Code == CodeHostnameConflict:
		return summary, apiErr.Error() + "\n\n" +
			"A hostname may only be routed by one account. Nothing in this account is holding it, " +
			"so it cannot be found or freed from here."

	case apiErr.Code == CodeQuotaExceeded:
		return summary, apiErr.Error() + "\n\n" +
			"The account has reached the number of tunnels it may have. Unlike the other refusals " +
			"this one lifts on its own once a tunnel is destroyed."

	case apiErr.Code == CodeTunnelInactive:
		return summary, apiErr.Error() + "\n\n" +
			"Routes live inside an active tunnel, and this one is not active. A tunnel becomes " +
			"active on its own; retrying before it does will fail the same way."

	case apiErr.Status == http.StatusForbidden && apiErr.Code == "":
		return summary, apiErr.Error() + "\n\n" +
			"A 403 carrying no error code did not come from the API: something between Terraform " +
			"and it refused the request. Check whether the network Terraform runs on can reach it."

	case apiErr.Status == http.StatusForbidden:
		return summary, apiErr.Error() + "\n\n" +
			"If the credential is an application token, check the role it was created with: member " +
			"may edit records, while creating or deleting a whole zone needs owner or admin."

	case apiErr.Status == http.StatusConflict:
		return summary, apiErr.Error() + "\n\n" +
			"The record changed between being read and being written — somebody else edited the same " +
			"record set. The provider already retried once; re-running the plan is the fix."

	case apiErr.Status == http.StatusBadGateway:
		return summary, apiErr.Error() + "\n\n" +
			"The DNS primary could not be reached or refused the operation. This is a platform " +
			"problem rather than one with the configuration; the code says which part failed."
	}

	return summary, apiErr.Error()
}
