package adl

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	otellog "go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/trace"
)

// otelState holds the OpenTelemetry logs SDK objects needed to emit and to flush on shutdown.
type otelState struct {
	provider *sdklog.LoggerProvider
	logger   otellog.Logger
}

func newOTelExporter(ctx context.Context, cfg Config) (sdklog.Exporter, error) {
	opts := []otlploggrpc.Option{otlploggrpc.WithEndpoint(cfg.OTLPEndpoint)}
	if cfg.OTLPInsecure {
		opts = append(opts, otlploggrpc.WithInsecure())
	}
	exporter, err := otlploggrpc.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("create the otlp log exporter: %w", err)
	}
	return exporter, nil
}

// newOTelState batches records to exporter. The batch processor makes emission asynchronous, so a collector outage does not fail a decision; the
// stdout copy is the trail that survives it.
func newOTelState(exporter sdklog.Exporter, resourceAttrs map[string]string) *otelState {
	attrs := make([]attribute.KeyValue, 0, len(resourceAttrs))
	for k, v := range resourceAttrs {
		attrs = append(attrs, attribute.String(k, v))
	}
	provider := sdklog.NewLoggerProvider(
		sdklog.WithResource(resource.NewSchemaless(attrs...)),
		sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter)),
	)
	return &otelState{provider: provider, logger: provider.Logger("attesta/adl")}
}

// emit sends the record as the log body, with its trace and span id as the native correlation fields.
func (s *otelState) emit(ctx context.Context, rec Record) error {
	body, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("marshal the record for otlp: %w", err)
	}
	var r otellog.Record
	r.SetTimestamp(time.UnixMilli(rec.Timestamp))
	r.SetEventName(rec.EventName)
	r.SetBody(attribute.StringValue(string(body)))
	if rec.Status == StatusError {
		r.SetSeverity(otellog.SeverityError)
	} else {
		r.SetSeverity(otellog.SeverityInfo)
	}
	s.logger.Emit(withSpanContext(ctx, rec), r)
	return nil
}

func withSpanContext(ctx context.Context, rec Record) context.Context {
	traceID, err := trace.TraceIDFromHex(rec.TraceID)
	if err != nil {
		return ctx
	}
	spanID, err := trace.SpanIDFromHex(rec.SpanID)
	if err != nil {
		return ctx
	}
	return trace.ContextWithSpanContext(ctx, trace.NewSpanContext(trace.SpanContextConfig{TraceID: traceID, SpanID: spanID}))
}

func (s *otelState) shutdown(ctx context.Context) error {
	if s == nil {
		return nil
	}
	return s.provider.Shutdown(ctx)
}
