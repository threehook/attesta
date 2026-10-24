package httpapi

import (
	"attesta/backend/internal/adl"
	"attesta/backend/internal/authz"
	"attesta/backend/internal/presentation"
	"attesta/backend/internal/status"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

// maxResponseBytes bounds a wallet's answer; an SD-JWT presentation is a few KB.
const maxResponseBytes = 1 << 20

// DecisionLog records every decision Attesta takes; adl.Logger is the implementation. A nil Server.Decisions records nothing.
type DecisionLog interface {
	Log(ctx context.Context, d adl.Decision) error
}

// Presenter runs the OpenID4VP flow; presentation.Verifier is the implementation.
type Presenter interface {
	NewRequest(r presentation.Request) (id, authorizationRequest string, err error)
	Respond(ctx context.Context, id string, form url.Values) (*presentation.Presented, error)
	Complete(id string, o presentation.Outcome)
	Outcome(id string) (*presentation.Outcome, error)
}

type presentationRequest struct {
	Resource string `json:"resource"`
	PolicyID string `json:"policyId"`
	// Credentials are what the holder must present, each of a type of its own; the first identifies the holder.
	Credentials []credentialRequest `json:"credentials"`
	// IdentityClaim is the claim that identifies the holder in every credential. Empty means email.
	IdentityClaim string `json:"identityClaim"`
	// Action describes what the application is about to do, as texts by name. The policy sees it as input.action.
	Action map[string]string `json:"action"`
	// CredentialType and Claims ask for a single credential; they are shorthand for Credentials with one entry and cannot be combined with it.
	CredentialType string   `json:"credentialType"`
	Claims         []string `json:"claims"`
}

type credentialRequest struct {
	CredentialType string   `json:"credentialType"`
	Claims         []string `json:"claims"`
}

// credentials is the credentials a request asks for, or why the request is not acceptable.
func (r presentationRequest) credentials() ([]presentation.CredentialQuery, string) {
	list := r.Credentials
	switch {
	case len(list) > 0 && (r.CredentialType != "" || len(r.Claims) > 0):
		return nil, "ask for credentials, or for a credentialType with claims, not both"
	case len(list) == 0 && r.CredentialType != "":
		list = []credentialRequest{{CredentialType: r.CredentialType, Claims: r.Claims}}
	case len(list) == 0:
		return nil, "at least one credential is required"
	}

	queries := make([]presentation.CredentialQuery, len(list))
	for i, c := range list {
		if c.CredentialType == "" ||
			slices.ContainsFunc(queries[:i], func(q presentation.CredentialQuery) bool { return q.CredentialType == c.CredentialType }) {
			return nil, "every credential needs a credentialType of its own"
		}

		queries[i] = presentation.CredentialQuery{CredentialType: c.CredentialType, Claims: c.Claims}
	}

	return queries, ""
}

const (
	maxActionEntries = 20
	maxActionValue   = 200
)

// actionName limits the names of an action to plain names.
var actionName = regexp.MustCompile(`^[a-z][A-Za-z0-9_]{0,39}$`)

// checkAction says why an action cannot be used, or gives "" when it can.
func checkAction(action map[string]string) string {
	if len(action) > maxActionEntries {
		return fmt.Sprintf("an action has at most %d entries", maxActionEntries)
	}

	for name, value := range action {
		if !actionName.MatchString(name) || len([]rune(value)) > maxActionValue {
			return fmt.Sprintf("an action has plain names and values of at most %d characters", maxActionValue)
		}
	}

	return ""
}

// identityClaimName limits the identity claim to a plain top-level claim name.
var identityClaimName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// reservedClaims are set by the issuer or the token format and cannot say who the holder is: with iss, every credential of one issuer would be of
// one holder.
var reservedClaims = []string{"iss", "vct", "cnf", "status", "iat", "exp", "nbf", "jti"}

type presentationRequestResponse struct {
	RequestID string `json:"requestId"`
	// AuthorizationRequest is the `openid4vp://` link the application hands to the user's wallet.
	AuthorizationRequest string `json:"authorizationRequest"`
}

// handlePresentationRequest starts an authorization: the application gets a link for the user's wallet and a request id to poll for the decision.
func (s *Server) handlePresentationRequest(w http.ResponseWriter, r *http.Request) {
	if s.Presenter == nil {
		writeError(w, http.StatusNotImplemented, "presentations are not configured")
		return
	}

	var req presentationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Resource == "" || req.PolicyID == "" {
		writeError(w, http.StatusBadRequest, "resource and policyId are required")
		return
	}

	queries, problem := req.credentials()
	if problem != "" {
		writeError(w, http.StatusBadRequest, problem)
		return
	}

	if req.IdentityClaim != "" && (!identityClaimName.MatchString(req.IdentityClaim) || slices.Contains(reservedClaims, req.IdentityClaim)) {
		writeError(w, http.StatusBadRequest, "identityClaim is not a claim that can identify the holder")
		return
	}

	if problem := checkAction(req.Action); problem != "" {
		writeError(w, http.StatusBadRequest, problem)
		return
	}

	if _, ok := s.Policies.Get(req.PolicyID); !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("unknown policy %q", req.PolicyID))
		return
	}

	id, link, err := s.Presenter.NewRequest(presentation.Request{
		Resource: req.Resource, PolicyID: req.PolicyID, Credentials: queries, IdentityClaim: req.IdentityClaim, Action: req.Action,
		UserRoles: parseUserRoles(r.Header.Get(userRolesHeader)), TraceParent: traceParent(r),
	})
	if err != nil {
		s.Logger.Error("creating presentation request failed", "error", err)
		writeError(w, http.StatusInternalServerError, "could not create the request")

		return
	}

	s.Logger.Info("presentation request", "requestId", id, "resource", req.Resource, "policyId", req.PolicyID, "credentialTypes", typesOf(queries))
	writeJSON(w, http.StatusOK, presentationRequestResponse{RequestID: id, AuthorizationRequest: link})
}

func typesOf(queries []presentation.CredentialQuery) []string {
	types := make([]string, len(queries))
	for i, q := range queries {
		types[i] = q.CredentialType
	}

	return types
}

// handlePresentationResponse is the wallet's direct_post answer. It verifies the presentation, applies the policy and records the decision for
// handlePresentationOutcome. The wallet gets an empty JSON object when its answer was processed, whatever the decision, and an OpenID4VP error
// response when the presentation did not verify.
func (s *Server) handlePresentationResponse(w http.ResponseWriter, r *http.Request) {
	if s.Presenter == nil {
		writeError(w, http.StatusNotImplemented, "presentations are not configured")
		return
	}

	id := r.PathValue("id")

	r.Body = http.MaxBytesReader(w, r.Body, maxResponseBytes)
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request", "error_description": "the response is not a form"})
		return
	}

	presented, err := s.Presenter.Respond(r.Context(), id, r.PostForm)
	switch {
	case errors.Is(err, presentation.ErrUnknownRequest):
		writeError(w, http.StatusNotFound, err.Error())
		return
	case errors.Is(err, presentation.ErrAlreadyAnswered):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request", "error_description": err.Error()})
		return
	case err != nil:
		s.Logger.Info("presentation rejected", "requestId", id, "error", err)

		var rejected *presentation.RejectedError
		errors.As(err, &rejected)
		d := decisionFor(id, requestOf(rejected))
		d.Reason, d.DecidedBy, d.Detail = reasonNotVerified, "attesta", err.Error()

		outcome := presentation.Outcome{Decision: authz.Deny, Reason: reasonOutcomeNotVerified}
		if errors.Is(err, status.ErrRevoked) {
			d.Reason, outcome.Reason = reasonRevoked, reasonRevoked
		}

		if !s.decide(w, r, id, d, outcome) {
			return
		}

		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request", "error_description": "the presentation did not verify"})

		return
	}

	first := presented.Credentials[0]
	d := decisionFor(id, presented.Request)

	d.SubjectID, d.Issuer, d.Claims = presented.Subject.ID, first.Issuer, policyClaims(first.Claims)
	for _, c := range presented.Credentials[1:] {
		d.Also = append(d.Also, adl.Credential{Type: c.Type, Issuer: c.Issuer, Claims: policyClaims(c.Claims)})
	}

	source, ok := s.Policies.Get(presented.Request.PolicyID)
	if !ok {
		d.Err = fmt.Errorf("unknown policy %q", presented.Request.PolicyID)
		if s.decide(w, r, id, d, presentation.Outcome{Decision: authz.NotApplicable, Reason: "unknown policy"}) {
			writeError(w, http.StatusNotFound, d.Err.Error())
		}

		return
	}

	decision, err := s.Authz.Evaluate(source, authz.Input{
		Resource: presented.Request.Resource, UserRoles: presented.Request.UserRoles, Action: presented.Request.Action,
		Credentials: credentialsOf(presented.Credentials),
	})
	if err != nil {
		s.Logger.Error("policy evaluation failed", "requestId", id, "policyId", presented.Request.PolicyID, "error", err)

		d.Err = err
		if s.decide(w, r, id, d, presentation.Outcome{Decision: authz.Indeterminate, Reason: "policy evaluation failed"}) {
			writeError(w, http.StatusInternalServerError, "policy evaluation failed")
		}

		return
	}

	s.Logger.Info("authorize result", "requestId", id, "issuer", first.Issuer, "resource", presented.Request.Resource, "decision", decision.Decision,
		"reason", decision.Reason)
	d.Allow, d.Reason, d.DecidedBy = decision.Decision == authz.Permit, decision.Reason, "policy"

	outcome := presentation.Outcome{Decision: decision.Decision, Reason: decision.Reason, Obligations: decision.Obligations, Advice: decision.Advice}
	if decision.Decision == authz.Permit {
		outcome.Subject = &presented.Subject
	}

	if s.decide(w, r, id, d, outcome) {
		writeJSON(w, http.StatusOK, struct{}{})
	}
}

// credentialsOf gives a policy the presented credentials by type.
func credentialsOf(presented []presentation.PresentedCredential) map[string]authz.Credential {
	out := make(map[string]authz.Credential, len(presented))
	for _, c := range presented {
		out[c.Type] = authz.Credential{Issuer: c.Issuer, Claims: policyClaims(c.Claims)}
	}

	return out
}

// reasonNotVerified is what the decision log says when the wallet's answer did not verify; the outcome the application sees keeps its own wording.
const (
	reasonNotVerified        = "credential could not be verified"
	reasonOutcomeNotVerified = "presentation did not verify"
	reasonRevoked            = "credential has been revoked"
)

func requestOf(r *presentation.RejectedError) presentation.Request {
	if r == nil {
		return presentation.Request{}
	}

	return r.Request
}

// firstType is the type of the credential that identifies the person, or empty for a request that is not known.
func firstType(r presentation.Request) string {
	if len(r.Credentials) == 0 {
		return ""
	}

	return r.Credentials[0].CredentialType
}

// decisionFor starts the log entry for a decision on request id.
func decisionFor(id string, req presentation.Request) adl.Decision {
	return adl.Decision{
		TraceParent: req.TraceParent, RequestID: id, Resource: req.Resource, PolicyID: req.PolicyID, CredentialType: firstType(req),
		UserRoles: req.UserRoles, Action: req.Action,
	}
}

// decide logs the decision and then records its outcome for the application. When the log entry cannot be written no decision is returned: the
// outcome becomes a denial, the wallet gets a server error, and decide reports false so the caller does not answer again.
func (s *Server) decide(w http.ResponseWriter, r *http.Request, id string, d adl.Decision, o presentation.Outcome) bool {
	if s.Decisions != nil {
		if err := s.Decisions.Log(r.Context(), d); err != nil {
			s.Logger.Error("decision log failed", "requestId", id, "error", err)
			s.Presenter.Complete(id, presentation.Outcome{Decision: authz.Indeterminate, Reason: "decision could not be logged"})
			writeError(w, http.StatusInternalServerError, "decision could not be logged")

			return false
		}
	}

	s.Presenter.Complete(id, o)

	return true
}

// traceParent returns the request's W3C traceparent, or "" when it is missing or malformed.
func traceParent(r *http.Request) string {
	if h := r.Header.Get("Traceparent"); adl.ValidTraceParent(h) {
		return h
	}

	return ""
}

type presentationOutcomeResponse struct {
	Status   string `json:"status"`
	Decision string `json:"decision,omitempty"`
	Reason   string `json:"reason,omitempty"`
	// Obligations must be carried out with the decision; advice may be ignored.
	Obligations []statementResponse `json:"obligations,omitempty"`
	Advice      []statementResponse `json:"advice,omitempty"`
	// Subject identifies who was allowed; it is absent for a denial.
	Subject *subjectResponse `json:"subject,omitempty"`
}

type statementResponse struct {
	ID                   string              `json:"id"`
	AttributeAssignments []attributeResponse `json:"attributeAssignments"`
}

type attributeResponse struct {
	AttributeID string `json:"attributeId"`
	Value       string `json:"value"`
}

func statementsResponse(statements []authz.Statement) []statementResponse {
	out := make([]statementResponse, 0, len(statements))
	for _, st := range statements {
		attrs := make([]attributeResponse, len(st.Attributes))
		for i, a := range st.Attributes {
			attrs[i] = attributeResponse{AttributeID: a.ID, Value: a.Value}
		}

		out = append(out, statementResponse{ID: st.ID, AttributeAssignments: attrs})
	}

	return out
}

type subjectResponse struct {
	Issuer string `json:"issuer"`
	// ID is the value of the identity claim. Email is the same value, and only when the identity claim is email.
	ID    string `json:"id"`
	Email string `json:"email,omitempty"`
}

// handlePresentationOutcome lets the application that started a request fetch the decision once the wallet has answered.
func (s *Server) handlePresentationOutcome(w http.ResponseWriter, r *http.Request) {
	if s.Presenter == nil {
		writeError(w, http.StatusNotImplemented, "presentations are not configured")
		return
	}

	outcome, err := s.Presenter.Outcome(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	if outcome == nil {
		writeJSON(w, http.StatusOK, presentationOutcomeResponse{Status: "pending"})
		return
	}

	resp := presentationOutcomeResponse{
		Status: "done", Decision: string(outcome.Decision), Reason: outcome.Reason,
		Obligations: statementsResponse(outcome.Obligations), Advice: statementsResponse(outcome.Advice),
	}
	if outcome.Subject != nil {
		resp.Subject = &subjectResponse{Issuer: outcome.Subject.Issuer, ID: outcome.Subject.ID}
		if outcome.Subject.Claim == "email" {
			resp.Subject.Email = outcome.Subject.ID
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// userRolesHeader carries the roles the calling application asserts for the user, comma-separated.
const userRolesHeader = "Att-User-Roles"

const (
	maxUserRoles     = 50
	maxUserRoleBytes = 100
)

// parseUserRoles splits the header value into trimmed, distinct roles; empty or oversized entries and entries beyond maxUserRoles are dropped.
// It returns an empty, non-nil slice when there are none.
func parseUserRoles(value string) []string {
	roles := []string{}

	for role := range strings.SplitSeq(value, ",") {
		role = strings.TrimSpace(role)
		if role == "" || len(role) > maxUserRoleBytes || slices.Contains(roles, role) {
			continue
		}

		if len(roles) == maxUserRoles {
			break
		}

		roles = append(roles, role)
	}

	return roles
}

// policyClaims gives a policy the claims as strings: text as it is, anything else (numbers, booleans, lists, objects) as its JSON.
func policyClaims(claims map[string]any) map[string]string {
	out := make(map[string]string, len(claims))
	for name, value := range claims {
		if text, ok := value.(string); ok {
			out[name] = text
			continue
		}

		encoded, err := json.Marshal(value)
		if err != nil {
			continue // cannot happen for values that came out of JSON
		}

		out[name] = string(encoded)
	}

	return out
}
