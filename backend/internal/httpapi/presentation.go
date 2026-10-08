package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"attesta/backend/internal/adl"
	"attesta/backend/internal/authz"
	"attesta/backend/internal/presentation"
)

// maxResponseBytes bounds a wallet's answer; an SD-JWT presentation is a few KB.
const maxResponseBytes = 1 << 20

// DecisionLog records every decision attesta takes; adl.Logger is the implementation. A nil Server.Decisions records nothing.
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
	Resource       string   `json:"resource"`
	PolicyID       string   `json:"policyId"`
	CredentialType string   `json:"credentialType"`
	Claims         []string `json:"claims"`
}

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
	if req.Resource == "" || req.PolicyID == "" || req.CredentialType == "" {
		writeError(w, http.StatusBadRequest, "resource, policyId and credentialType are required")
		return
	}
	if _, ok := s.Policies.Get(req.PolicyID); !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("unknown policy %q", req.PolicyID))
		return
	}

	id, link, err := s.Presenter.NewRequest(presentation.Request{
		Resource: req.Resource, PolicyID: req.PolicyID, CredentialType: req.CredentialType, Claims: req.Claims, UserRoles: parseUserRoles(r.Header.Get(userRolesHeader)),
		TraceParent: traceParent(r),
	})
	if err != nil {
		s.Logger.Error("creating presentation request failed", "error", err)
		writeError(w, http.StatusInternalServerError, "could not create the request")
		return
	}
	s.Logger.Info("presentation request", "requestId", id, "resource", req.Resource, "policyId", req.PolicyID, "credentialType", req.CredentialType)
	writeJSON(w, http.StatusOK, presentationRequestResponse{RequestID: id, AuthorizationRequest: link})
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
		var rejected *presentation.Rejected
		errors.As(err, &rejected)
		d := decisionFor(id, requestOf(rejected))
		d.Reason, d.DecidedBy, d.Detail = reasonNotVerified, "attesta", err.Error()
		if !s.decide(w, r, id, d, presentation.Outcome{Reason: reasonOutcomeNotVerified}) {
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request", "error_description": "the presentation did not verify"})
		return
	}

	d := decisionFor(id, presented.Request)
	d.SubjectID, d.Issuer, d.Claims = presented.Subject.Email, presented.Issuer, policyClaims(presented.Claims)
	source, ok := s.Policies.Get(presented.Request.PolicyID)
	if !ok {
		d.Err = fmt.Errorf("unknown policy %q", presented.Request.PolicyID)
		if s.decide(w, r, id, d, presentation.Outcome{Reason: "unknown policy"}) {
			writeError(w, http.StatusNotFound, d.Err.Error())
		}
		return
	}
	decision, err := s.Authz.Evaluate(source, authz.Input{
		Resource: presented.Request.Resource, Type: presented.Type, Issuer: presented.Issuer, Claims: policyClaims(presented.Claims),
		UserRoles: presented.Request.UserRoles,
	})
	if err != nil {
		s.Logger.Error("policy evaluation failed", "requestId", id, "policyId", presented.Request.PolicyID, "error", err)
		d.Err = err
		if s.decide(w, r, id, d, presentation.Outcome{Reason: "policy evaluation failed"}) {
			writeError(w, http.StatusInternalServerError, "policy evaluation failed")
		}
		return
	}

	s.Logger.Info("authorize result", "requestId", id, "issuer", presented.Issuer, "resource", presented.Request.Resource, "allow", decision.Allow,
		"reason", decision.Reason)
	d.Allow, d.Reason, d.DecidedBy = decision.Allow, decision.Reason, "policy"
	outcome := presentation.Outcome{Allow: decision.Allow, Reason: decision.Reason}
	if decision.Allow {
		outcome.Subject = &presented.Subject
	}
	if s.decide(w, r, id, d, outcome) {
		writeJSON(w, http.StatusOK, struct{}{})
	}
}

// reasonNotVerified is what the decision log says when the wallet's answer did not verify; the outcome the application sees keeps its own wording.
const (
	reasonNotVerified        = "credential could not be verified"
	reasonOutcomeNotVerified = "presentation did not verify"
)

func requestOf(r *presentation.Rejected) presentation.Request {
	if r == nil {
		return presentation.Request{}
	}
	return r.Request
}

// decisionFor starts the log entry for a decision on request id.
func decisionFor(id string, req presentation.Request) adl.Decision {
	return adl.Decision{
		TraceParent: req.TraceParent, RequestID: id, Resource: req.Resource, PolicyID: req.PolicyID, CredentialType: req.CredentialType,
		UserRoles: req.UserRoles,
	}
}

// decide logs the decision and then records its outcome for the application. When the log entry cannot be written no decision is returned: the
// outcome becomes a denial, the wallet gets a server error, and decide reports false so the caller does not answer again.
func (s *Server) decide(w http.ResponseWriter, r *http.Request, id string, d adl.Decision, o presentation.Outcome) bool {
	if s.Decisions != nil {
		if err := s.Decisions.Log(r.Context(), d); err != nil {
			s.Logger.Error("decision log failed", "requestId", id, "error", err)
			s.Presenter.Complete(id, presentation.Outcome{Allow: false, Reason: "decision could not be logged"})
			writeError(w, http.StatusInternalServerError, "decision could not be logged")
			return false
		}
	}
	s.Presenter.Complete(id, o)
	return true
}

// traceParent returns the request's W3C traceparent, or "" when it is missing or malformed.
func traceParent(r *http.Request) string {
	if h := r.Header.Get("traceparent"); adl.ValidTraceParent(h) {
		return h
	}
	return ""
}

type presentationOutcomeResponse struct {
	Status string `json:"status"`
	Allow  *bool  `json:"allow,omitempty"`
	Reason string `json:"reason,omitempty"`
	// Subject identifies who was allowed; it is absent for a denial.
	Subject *subjectResponse `json:"subject,omitempty"`
}

type subjectResponse struct {
	Issuer string `json:"issuer"`
	Email  string `json:"email"`
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
	resp := presentationOutcomeResponse{Status: "done", Allow: &outcome.Allow, Reason: outcome.Reason}
	if outcome.Subject != nil {
		resp.Subject = &subjectResponse{Issuer: outcome.Subject.Issuer, Email: outcome.Subject.Email}
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
	for _, role := range strings.Split(value, ",") {
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
