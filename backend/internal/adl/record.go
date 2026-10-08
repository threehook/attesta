// Package adl writes Authorization Decision Log 1.0 Level 1 records, one per decision attesta takes.
//
// Spec: https://gitdocumentatie.logius.nl/publicatie/ftv/adl/1.0.0/
package adl

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
)

// Status is the record's status. A denial is Ok: Error is reserved for attesta failing to evaluate.
type Status string

const (
	StatusOk    Status = "Ok"
	StatusError Status = "Error"
)

// EventAccessEvaluation is the event_name of a single decision, the only kind attesta takes.
const EventAccessEvaluation = "adl.access_evaluation"

// Record is an ADL Level 1 record: no policy, information or configuration source references.
type Record struct {
	TraceID      string            `json:"trace_id"`
	SpanID       string            `json:"span_id"`
	ParentSpanID string            `json:"parent_span_id,omitempty"`
	EventName    string            `json:"event_name"`
	Timestamp    int64             `json:"timestamp"` // milliseconds since the Unix epoch
	Status       Status            `json:"status"`
	Attributes   map[string]any    `json:"attributes"`
	Resource     map[string]string `json:"resource,omitempty"`
	Body         Body              `json:"body"`
}

// Body holds the raw request and response under the keys the standard fixes. The response is absent when attesta could not evaluate.
type Body struct {
	Request  any `json:"adl.core.request"`
	Response any `json:"adl.core.response,omitempty"`
}

// traceContext is the part of a W3C traceparent a record needs.
type traceContext struct {
	traceID, parentSpanID string
}

// parseTraceParent reads a W3C traceparent header ("00-<trace-id>-<parent-id>-<flags>"). It returns false for anything malformed.
func parseTraceParent(header string) (traceContext, bool) {
	parts := strings.Split(strings.TrimSpace(header), "-")
	if len(parts) < 4 || parts[0] != "00" || !lowerHex(parts[1], 32) || !lowerHex(parts[2], 16) || !lowerHex(parts[3], 2) {
		return traceContext{}, false
	}
	if parts[1] == strings.Repeat("0", 32) || parts[2] == strings.Repeat("0", 16) {
		return traceContext{}, false
	}
	return traceContext{traceID: parts[1], parentSpanID: parts[2]}, true
}

// ValidTraceParent reports whether header is a usable W3C traceparent.
func ValidTraceParent(header string) bool {
	_, ok := parseTraceParent(header)
	return ok
}

func lowerHex(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// randomHex returns n random bytes as lowercase hex.
func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
