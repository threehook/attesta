package httpapi

import (
	"attesta/backend/internal/authz"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
)

type putPolicyRequest struct {
	ID     string `json:"id"`
	Source string `json:"source"`
}

// handlePutPolicy installs or replaces a policy without restarting the server — the manual deploy path described in the README, gated by
// requireAdmin. The source is validated (it must compile as Rego) before it's accepted; an invalid policy never reaches the store.
func (s *Server) handlePutPolicy(w http.ResponseWriter, r *http.Request) {
	var req putPolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ID == "" || req.Source == "" {
		writeError(w, http.StatusBadRequest, "id and source are required")
		return
	}

	if err := s.Policies.Put(req.ID, req.Source); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.Logger.Info("policy deployed", "id", req.ID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deployed", "id": req.ID})
}

// maxPolicySource bounds a policy sent for diagnosis; real ones are a few kilobytes.
const maxPolicySource = 256 << 10

// handleDiagnosePolicy reports the problems in a policy source, with their places, and stores nothing.
func (s *Server) handleDiagnosePolicy(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Source string `json:"source"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxPolicySource)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	diagnostics := s.Authz.Diagnose(req.Source)
	if diagnostics == nil {
		diagnostics = []authz.Diagnostic{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"diagnostics": diagnostics})
}

type evaluateRequest struct {
	Source string `json:"source"`
	Input  struct {
		Resource    string   `json:"resource"`
		UserRoles   []string `json:"userRoles"`
		Credentials map[string]struct {
			Issuer string            `json:"issuer"`
			Claims map[string]string `json:"claims"`
		} `json:"credentials"`
	} `json:"input"`
}

type statementBody struct {
	ID         string            `json:"id"`
	Attributes map[string]string `json:"attributes"`
}

func statementsBody(list []authz.Statement) []statementBody {
	out := make([]statementBody, len(list))
	for i, st := range list {
		out[i] = statementBody{ID: st.ID, Attributes: make(map[string]string, len(st.Attributes))}
		for _, a := range st.Attributes {
			out[i].Attributes[a.ID] = a.Value
		}
	}
	return out
}

// handleEvaluatePolicy runs a policy source on an input given in the request and returns the decision, or the problems of the policy when it is not
// accepted; nothing is stored. The input has the shape a policy sees: resource, userRoles and credentials by type, whose claims are text.
func (s *Server) handleEvaluatePolicy(w http.ResponseWriter, r *http.Request) {
	var req evaluateRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2*maxPolicySource))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		var typeErr *json.UnmarshalTypeError
		if errors.As(err, &typeErr) && typeErr.Type.Kind() == reflect.String {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid input: %s must be text", typeErr.Field))
			return
		}
		writeError(w, http.StatusBadRequest, "invalid input: "+err.Error())
		return
	}
	if diagnostics := s.Authz.Diagnose(req.Source); len(diagnostics) > 0 {
		writeJSON(w, http.StatusOK, map[string]any{"diagnostics": diagnostics})
		return
	}

	in := authz.Input{Resource: req.Input.Resource, UserRoles: req.Input.UserRoles, Credentials: make(map[string]authz.Credential, len(req.Input.Credentials))}
	for t, c := range req.Input.Credentials {
		in.Credentials[t] = authz.Credential{Issuer: c.Issuer, Claims: c.Claims}
	}
	result, err := s.Authz.Evaluate(req.Source, in)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": map[string]any{
		"decision": result.Decision, "reason": result.Reason, "obligations": statementsBody(result.Obligations), "advice": statementsBody(result.Advice),
	}})
}
