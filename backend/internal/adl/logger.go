package adl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"
)

// Decision is one authorization decision, as far as attesta got.
type Decision struct {
	// TraceParent is the W3C traceparent the application sent when it started the request; empty or invalid starts a new trace.
	TraceParent string
	RequestID   string
	// Resource and PolicyID say what was asked and which policy decided.
	Resource, PolicyID, CredentialType string
	// SubjectID and Issuer identify the holder once the credential verified; empty when it did not.
	SubjectID, Issuer string
	// Claims are the disclosed claims the policy received.
	Claims map[string]string
	// UserRoles are the roles the application asserted; attesta cannot verify them.
	UserRoles []string
	// Allow and Reason are the decision. DecidedBy says who gave the reason: "policy", or "attesta" when no policy ran.
	Allow     bool
	Reason    string
	DecidedBy string
	// Detail adds to Reason for the log reader, such as why a credential did not verify.
	Detail string
	// Err is set when attesta could not evaluate; the record is then an Error record without a response.
	Err error
}

// Logger writes one record per decision to the configured outputs. A nil *Logger discards records.
type Logger struct {
	log      *slog.Logger
	out      io.Writer
	otel     *otelState
	resource map[string]string
	now      func() time.Time
}

// New builds a Logger. An output that cannot be set up is logged and skipped, so the others keep working.
func New(ctx context.Context, cfg Config, log *slog.Logger) *Logger {
	l := &Logger{log: log, out: io.Discard, resource: cfg.effectiveResource(), now: time.Now}
	outputs := cfg.outputs()
	if outputs[OutputStdout] {
		l.out = os.Stdout
	}
	if outputs[OutputOTLP] {
		if cfg.OTLPEndpoint == "" {
			log.Error("adl: otlp output requested without ATTESTA_ADL_OTLP_ENDPOINT, skipping it")
		} else if exporter, err := newOTelExporter(ctx, cfg); err != nil {
			log.Error("adl: otlp export not available, skipping it", "error", err)
		} else {
			l.otel = newOTelState(exporter, l.resource)
		}
	}
	log.Info("adl decision logging enabled", "output", cfg.Output)
	return l
}

// Close flushes records still batched for OTLP.
func (l *Logger) Close(ctx context.Context) error {
	if l == nil {
		return nil
	}
	return l.otel.shutdown(ctx)
}

// Log writes the record for d. It returns an error when the stdout record could not be written or built, so a caller can refuse to return a
// decision nobody can trace; an OTLP failure after the record is queued does not surface.
func (l *Logger) Log(ctx context.Context, d Decision) error {
	if l == nil {
		return nil
	}
	rec, err := l.record(d)
	if err != nil {
		return err
	}
	var errs []error
	if buf, err := json.Marshal(rec); err != nil {
		errs = append(errs, fmt.Errorf("marshal the record: %w", err))
	} else if _, err := l.out.Write(append(buf, '\n')); err != nil {
		errs = append(errs, fmt.Errorf("write the record: %w", err))
	}
	if l.otel != nil {
		if err := l.otel.emit(ctx, rec); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (l *Logger) record(d Decision) (Record, error) {
	spanID, err := randomHex(8)
	if err != nil {
		return Record{}, fmt.Errorf("make a span id: %w", err)
	}
	rec := Record{
		SpanID: spanID, EventName: EventAccessEvaluation, Timestamp: l.now().UnixMilli(), Status: StatusOk,
		Attributes: map[string]any{}, Resource: l.resource, Body: Body{Request: authzenRequest(d)},
	}
	if tc, ok := parseTraceParent(d.TraceParent); ok {
		rec.TraceID, rec.ParentSpanID = tc.traceID, tc.parentSpanID
	} else if rec.TraceID, err = randomHex(16); err != nil {
		return Record{}, fmt.Errorf("make a trace id: %w", err)
	}
	if d.Err != nil {
		rec.Status = StatusError
		rec.Attributes["error"] = d.Err.Error()
		return rec, nil
	}
	rec.Body.Response = authzenResponse(d)
	return rec, nil
}

// authzenRequest renders the question in the AuthZEN shape ADL prescribes for the request. The action is always "authorize": attesta has no
// actions, the resource it is asked about is the thing a policy decides on.
func authzenRequest(d Decision) map[string]any {
	subject := map[string]any{"type": "user", "id": "unknown"}
	if d.SubjectID != "" {
		subject["id"] = d.SubjectID
		subject["properties"] = map[string]any{"issuer": d.Issuer}
	}
	claims := d.Claims
	if claims == nil {
		claims = map[string]string{}
	}
	roles := d.UserRoles
	if roles == nil {
		roles = []string{}
	}
	return map[string]any{
		"subject":  subject,
		"action":   map[string]any{"name": "authorize"},
		"resource": map[string]any{"type": "resource", "id": d.Resource},
		"context": map[string]any{
			"request_id": d.RequestID, "policy_id": d.PolicyID, "credential_type": d.CredentialType,
			"claims":     claims,
			"user_roles": map[string]any{"values": roles, "origin": "application"},
		},
	}
}

// authzenResponse is the decision. The reason is keyed by who gave it, so a reader can tell a policy's reason from attesta's own.
func authzenResponse(d Decision) map[string]any {
	by := d.DecidedBy
	if by == "" {
		by = "policy"
	}
	reason := map[string]any{by: d.Reason}
	if d.Detail != "" {
		reason["detail"] = d.Detail
	}
	return map[string]any{"decision": d.Allow, "context": map[string]any{"reason": reason}}
}
