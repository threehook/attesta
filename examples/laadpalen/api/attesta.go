package main

import (
	"context"
	"encoding/json"

	"github.com/threehook/enforcer-go"
)

var errUnknownRequest = enforcer.ErrUnknownRequest

type (
	authorizationRequest = enforcer.Request
	subject              = enforcer.Subject
	outcome              = enforcer.Outcome
)

// authorizer is what this app needs from Attesta; enforcer.Client is the implementation, a client of the sidecar next to it in the pod.
type authorizer interface {
	Start(ctx context.Context, resource, policyID, credentialType string, claims, userRoles []string) (authorizationRequest, error)
	// Outcome returns the decision, and the sidecar's answer as it came, for the page's API panel.
	Outcome(ctx context.Context, id string) (outcome, json.RawMessage, error)
}
