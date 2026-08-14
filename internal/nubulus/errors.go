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
func Explain(action string, err error) (summary, detail string) {
	summary = "Could not " + action

	var transportErr *TransportError
	if errors.As(err, &transportErr) {
		return summary, fmt.Sprintf(
			"%s\n\nThe endpoint did not answer at all. Check that the URL in the provider block is "+
				"reachable from the machine running Terraform.", transportErr.Error())
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
			"(\"projects\", plural). The provider asks for it; a token fetched by hand, or one whose " +
			"project_id does not match the platform's, will not have it."

	case apiErr.Code == CodeNoAccount:
		return summary, apiErr.Error() + "\n\n" +
			"The token is valid but the organization it was issued in does not map to any Nubulus " +
			"account. This usually means the credential belongs to a different environment than the " +
			"endpoint it is being used against."

	case apiErr.Status == http.StatusForbidden && apiErr.Code == "":
		return summary, apiErr.Error() + "\n\n" +
			"A 403 carrying no error code did not come from the API: something between Terraform " +
			"and it refused the request. Check the endpoint in the provider block and whether the " +
			"network Terraform runs on can reach it."

	case apiErr.Status == http.StatusForbidden:
		return summary, apiErr.Error() + "\n\n" +
			"If the credential is an application token, check the role it was created with: member " +
			"may edit records, while creating or deleting a whole zone needs owner or admin."

	case apiErr.Code == CodeZoneNotActive:
		return summary, apiErr.Error() + "\n\n" +
			"Records can only be written into a zone that is active. A zone claimed for a name " +
			"registered elsewhere stays pending until control of it has been proven, and it does not " +
			"exist on the name servers until then — publish the challenge TXT record and add a " +
			"`nubuluscloud_dns_zone_verification` resource that the records depend on."

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
