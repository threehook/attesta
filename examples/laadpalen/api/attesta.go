package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// errUnknownRequest means the sidecar no longer knows the request: it expired, or the sidecar restarted.
var errUnknownRequest = errors.New("unknown or expired request")

type authorizationRequest struct {
	RequestID string `json:"requestId"`
	// Link is the openid4vp:// link the employee opens in their wallet.
	Link string `json:"authorizationRequest"`
	// TraceID is the trace this app started for the request; Attesta's decision log carries it, and so does this app's own log.
	TraceID string `json:"-"`
}

type subject struct {
	Issuer string `json:"issuer"`
	Email  string `json:"email"`
}

// outcome is the sidecar's answer to a request: pending until the wallet has answered, then done with the decision.
type outcome struct {
	Status  string   `json:"status"`
	Allow   bool     `json:"allow"`
	Reason  string   `json:"reason"`
	Subject *subject `json:"subject"`
}

// authorizer is what this app needs from Attesta; attestaClient is the implementation, a client of the sidecar next to it in the pod.
type authorizer interface {
	start(ctx context.Context, resource, policyID, credentialType string, claims, userRoles []string) (authorizationRequest, error)
	// outcome returns the decision, and the sidecar's answer as it came, for the page's API panel.
	outcome(ctx context.Context, id string) (outcome, json.RawMessage, error)
}

type attestaClient struct {
	base string
	http *http.Client
}

func newAttestaClient(base string) *attestaClient {
	return &attestaClient{base: base, http: &http.Client{Timeout: 10 * time.Second}}
}

func (c *attestaClient) start(ctx context.Context, resource, policyID, credentialType string, claims, userRoles []string) (authorizationRequest, error) {
	body, err := json.Marshal(map[string]any{"resource": resource, "policyId": policyID, "credentialType": credentialType, "claims": claims})
	if err != nil {
		return authorizationRequest{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/v1/authorize/requests", bytes.NewReader(body))
	if err != nil {
		return authorizationRequest{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	traceID, spanID := randomHex(16), randomHex(8)
	req.Header.Set("traceparent", "00-"+traceID+"-"+spanID+"-01")
	if len(userRoles) > 0 {
		req.Header.Set("Att-User-Roles", strings.Join(userRoles, ","))
	}
	raw, status, err := c.do(req)
	if err != nil {
		return authorizationRequest{}, err
	}
	if status != http.StatusOK {
		return authorizationRequest{}, fmt.Errorf("Attesta answered %d: %s", status, bytes.TrimSpace(raw))
	}
	var out authorizationRequest
	if err := json.Unmarshal(raw, &out); err != nil || out.RequestID == "" || out.Link == "" {
		return authorizationRequest{}, fmt.Errorf("Attesta sent an unusable answer: %s", raw)
	}
	out.TraceID = traceID
	return out, nil
}

// randomHex returns n random bytes as lowercase hex.
func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (c *attestaClient) outcome(ctx context.Context, id string) (outcome, json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/v1/authorize/requests/"+id, nil)
	if err != nil {
		return outcome{}, nil, err
	}
	raw, status, err := c.do(req)
	if err != nil {
		return outcome{}, nil, err
	}
	switch status {
	case http.StatusOK:
	case http.StatusNotFound:
		return outcome{}, nil, errUnknownRequest
	default:
		return outcome{}, nil, fmt.Errorf("Attesta answered %d: %s", status, bytes.TrimSpace(raw))
	}
	var out outcome
	if err := json.Unmarshal(raw, &out); err != nil {
		return outcome{}, nil, fmt.Errorf("Attesta sent an unusable answer: %s", raw)
	}
	return out, raw, nil
}

func (c *attestaClient) do(req *http.Request) ([]byte, int, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return raw, resp.StatusCode, err
}
