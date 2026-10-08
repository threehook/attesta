package adl

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	sdklog "go.opentelemetry.io/otel/sdk/log"
)

const traceParent = "00-28dbeec32e77635cc19bc3204ec56c41-dec5220770f8f4f4-01"

func testLogger(out io.Writer) *Logger {
	return &Logger{
		log: slog.New(slog.NewTextHandler(io.Discard, nil)), out: out, resource: map[string]string{"service.name": "attesta", "instance_id": "pod-1"},
		now: func() time.Time { return time.UnixMilli(1757240058042) },
	}
}

func logged(t *testing.T, d Decision) map[string]any {
	t.Helper()
	var out bytes.Buffer
	if err := testLogger(&out).Log(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	if strings.Count(out.String(), "\n") != 1 {
		t.Fatalf("want exactly one line, got %q", out.String())
	}
	var rec map[string]any
	if err := json.Unmarshal(out.Bytes(), &rec); err != nil {
		t.Fatal(err)
	}
	return rec
}

func at(t *testing.T, m map[string]any, path ...string) any {
	t.Helper()
	var cur any = m
	for _, key := range path {
		next, ok := cur.(map[string]any)[key]
		if !ok {
			t.Fatalf("no %v in %v", path, m)
		}
		cur = next
	}
	return cur
}

func allowed() Decision {
	return Decision{
		TraceParent: traceParent, RequestID: "req-1", Resource: "request_laadpaal", PolicyID: "request_laadpaal", CredentialType: "Employee",
		SubjectID: "jsmith@vlierdam.nl", Issuer: "did:key:z6Mk", Claims: map[string]string{"department": "burgerzaken"},
		UserRoles: []string{"laadpalen-aanvrager"}, Allow: true, Reason: "Geautoriseerd", DecidedBy: "policy",
	}
}

func TestRecordIsALevel1RecordWithTheCallersTrace(t *testing.T) {
	rec := logged(t, allowed())

	if rec["trace_id"] != "28dbeec32e77635cc19bc3204ec56c41" || rec["parent_span_id"] != "dec5220770f8f4f4" {
		t.Errorf("trace = %v / %v, want the caller's trace and span as the parent", rec["trace_id"], rec["parent_span_id"])
	}
	if span, _ := rec["span_id"].(string); !lowerHex(span, 16) || span == rec["parent_span_id"] {
		t.Errorf("span_id = %v, want a fresh 16 hex span", rec["span_id"])
	}
	if rec["event_name"] != "adl.access_evaluation" || rec["status"] != "Ok" || rec["timestamp"] != float64(1757240058042) {
		t.Errorf("event_name/status/timestamp = %v %v %v", rec["event_name"], rec["status"], rec["timestamp"])
	}
	if attrs := at(t, rec, "attributes").(map[string]any); len(attrs) != 0 {
		t.Errorf("attributes = %v, want none at Level 1", attrs)
	}
	if at(t, rec, "resource", "service.name") != "attesta" || at(t, rec, "resource", "instance_id") != "pod-1" {
		t.Errorf("resource = %v", rec["resource"])
	}
	if at(t, rec, "body", "adl.core.request", "subject", "id") != "jsmith@vlierdam.nl" ||
		at(t, rec, "body", "adl.core.request", "action", "name") != "authorize" ||
		at(t, rec, "body", "adl.core.request", "resource", "id") != "request_laadpaal" ||
		at(t, rec, "body", "adl.core.request", "context", "user_roles", "origin") != "application" {
		t.Errorf("request = %v", at(t, rec, "body", "adl.core.request"))
	}
	if at(t, rec, "body", "adl.core.response", "decision") != true ||
		at(t, rec, "body", "adl.core.response", "context", "reason", "policy") != "Geautoriseerd" {
		t.Errorf("response = %v", at(t, rec, "body", "adl.core.response"))
	}
}

func TestADenialIsStillOkAndShowsFalse(t *testing.T) {
	d := allowed()
	d.Allow, d.Reason = false, "Niet geautoriseerd vanwege rol"
	rec := logged(t, d)

	if rec["status"] != "Ok" || at(t, rec, "body", "adl.core.response", "decision") != false {
		t.Errorf("a denial must be status Ok with decision false: %v", rec)
	}
}

func TestACredentialThatDidNotVerifyIsADenialByAttesta(t *testing.T) {
	d := Decision{
		RequestID: "req-1", Resource: "request_laadpaal", PolicyID: "request_laadpaal", CredentialType: "Employee",
		Reason: "credential could not be verified", DecidedBy: "attesta", Detail: "invalid presentation: state does not match the request",
	}
	rec := logged(t, d)

	if rec["status"] != "Ok" || at(t, rec, "body", "adl.core.request", "subject", "id") != "unknown" {
		t.Errorf("record = %v", rec)
	}
	if at(t, rec, "body", "adl.core.response", "decision") != false ||
		at(t, rec, "body", "adl.core.response", "context", "reason", "attesta") != "credential could not be verified" ||
		!strings.Contains(at(t, rec, "body", "adl.core.response", "context", "reason", "detail").(string), "state does not match") {
		t.Errorf("response = %v", at(t, rec, "body", "adl.core.response"))
	}
}

func TestAFailedEvaluationIsAnErrorRecordWithoutResponse(t *testing.T) {
	d := allowed()
	d.Err = errors.New("gno policy error: boom")
	rec := logged(t, d)

	if rec["status"] != "Error" {
		t.Errorf("status = %v, want Error", rec["status"])
	}
	if _, has := at(t, rec, "body").(map[string]any)["adl.core.response"]; has {
		t.Error("an Error record carries no response")
	}
	if at(t, rec, "attributes", "error") != "gno policy error: boom" {
		t.Errorf("attributes = %v", rec["attributes"])
	}
}

func TestWithoutATraceparentARootTraceStarts(t *testing.T) {
	for _, tp := range []string{"", "garbage", "00-00000000000000000000000000000000-dec5220770f8f4f4-01", "00-28dbeec32e77635cc19bc3204ec56c41-0000000000000000-01"} {
		d := allowed()
		d.TraceParent = tp
		rec := logged(t, d)
		if id, _ := rec["trace_id"].(string); !lowerHex(id, 32) {
			t.Errorf("%q: trace_id = %v", tp, rec["trace_id"])
		}
		if _, has := rec["parent_span_id"]; has {
			t.Errorf("%q: a root trace has no parent_span_id", tp)
		}
	}
}

func TestValidTraceParent(t *testing.T) {
	if !ValidTraceParent(traceParent) || ValidTraceParent("00-ABC") || ValidTraceParent("01-28dbeec32e77635cc19bc3204ec56c41-dec5220770f8f4f4-01") {
		t.Error("ValidTraceParent misjudged")
	}
}

func TestANilLoggerDiscards(t *testing.T) {
	var l *Logger
	if err := l.Log(context.Background(), allowed()); err != nil {
		t.Fatal(err)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestAFailedWriteIsReported(t *testing.T) {
	if err := testLogger(failingWriter{}).Log(context.Background(), allowed()); err == nil {
		t.Fatal("want an error when the record cannot be written")
	}
}

type memExporter struct {
	mu      sync.Mutex
	records []sdklog.Record
}

func (e *memExporter) Export(_ context.Context, r []sdklog.Record) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, rec := range r {
		e.records = append(e.records, rec.Clone())
	}
	return nil
}
func (e *memExporter) Shutdown(context.Context) error   { return nil }
func (e *memExporter) ForceFlush(context.Context) error { return nil }

func TestOTLPCarriesTheRecordWithNativeTraceIDs(t *testing.T) {
	exp := &memExporter{}
	l := testLogger(io.Discard)
	l.otel = newOTelState(exp, l.resource)

	if err := l.Log(context.Background(), allowed()); err != nil {
		t.Fatal(err)
	}
	if err := l.Close(context.Background()); err != nil {
		t.Fatal(err)
	}

	if len(exp.records) != 1 {
		t.Fatalf("exported %d records, want 1", len(exp.records))
	}
	r := exp.records[0]
	if r.TraceID().String() != "28dbeec32e77635cc19bc3204ec56c41" || r.SpanID().String() == "0000000000000000" {
		t.Errorf("native ids = %s / %s", r.TraceID(), r.SpanID())
	}
	var body Record
	if err := json.Unmarshal([]byte(r.Body().AsString()), &body); err != nil || body.EventName != "adl.access_evaluation" || body.TraceID != r.TraceID().String() {
		t.Errorf("body = %s (%v)", r.Body().AsString(), err)
	}
	found := false
	for _, kv := range r.Resource().Attributes() {
		if string(kv.Key) == "service.name" && kv.Value.AsString() == "attesta" {
			found = true
		}
	}
	if !found {
		t.Error("service.name is missing from the OTLP resource")
	}
}

func TestConfig(t *testing.T) {
	if got := parseResource(" a=b , c = d,broken,=x"); len(got) != 2 || got["a"] != "b" || got["c"] != "d" {
		t.Errorf("parseResource = %v", got)
	}
	if o := (Config{}).outputs(); !o[OutputStdout] || o[OutputOTLP] {
		t.Errorf("default outputs = %v, want stdout only", o)
	}
	if o := (Config{Output: "stdout, otlp"}).outputs(); !o[OutputStdout] || !o[OutputOTLP] {
		t.Errorf("outputs = %v", o)
	}
	if r := (Config{Resource: map[string]string{"service.name": "x"}}).effectiveResource(); r["service.name"] != "x" || r["instance_id"] == "" {
		t.Errorf("effectiveResource = %v", r)
	}
}
