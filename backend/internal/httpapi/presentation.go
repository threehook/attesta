package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"zk-puoi/backend/internal/authz"
	"zk-puoi/backend/internal/presentation"
)

// maxResponseBytes bounds a wallet's answer; an SD-JWT presentation is a few KB.
const maxResponseBytes = 1 << 20

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
		Resource: req.Resource, PolicyID: req.PolicyID, CredentialType: req.CredentialType, Claims: req.Claims,
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
		s.Presenter.Complete(id, presentation.Outcome{Allow: false, Reason: "presentation did not verify"})
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request", "error_description": "the presentation did not verify"})
		return
	}

	source, ok := s.Policies.Get(presented.Request.PolicyID)
	if !ok {
		s.Presenter.Complete(id, presentation.Outcome{Allow: false, Reason: "unknown policy"})
		writeError(w, http.StatusNotFound, fmt.Sprintf("unknown policy %q", presented.Request.PolicyID))
		return
	}
	decision, err := s.Authz.Evaluate(source, authz.Input{Resource: presented.Request.Resource, Type: presented.Type, Issuer: presented.Issuer})
	if err != nil {
		s.Logger.Error("policy evaluation failed", "requestId", id, "policyId", presented.Request.PolicyID, "error", err)
		s.Presenter.Complete(id, presentation.Outcome{Allow: false, Reason: "policy evaluation failed"})
		writeError(w, http.StatusInternalServerError, "policy evaluation failed")
		return
	}

	s.Logger.Info("authorize result", "requestId", id, "issuer", presented.Issuer, "resource", presented.Request.Resource, "allow", decision.Allow,
		"reason", decision.Reason)
	outcome := presentation.Outcome{Allow: decision.Allow, Reason: decision.Reason}
	if decision.Allow {
		outcome.Subject = &presented.Subject
	}
	s.Presenter.Complete(id, outcome)
	writeJSON(w, http.StatusOK, struct{}{})
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
