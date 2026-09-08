package observability

import (
	"context"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

var installed atomic.Pointer[Runtime]
var httpRequests = NewCounter("orchestrator_http_requests_total", "Completed HTTP requests by bounded route, method and status class")
var httpBusinessErrors = NewCounter("orchestrator_http_business_errors_total", "APIResponse ERROR results by route")
var httpDuration = duration("orchestrator_http_request_duration_seconds", "HTTP request duration by route and bounded method")
var httpInflight atomic.Int64

func init() {
	Gauge("orchestrator_http_inflight", "Currently executing HTTP requests", httpInflight.Load)
}

// ServeMetrics is node-local and must be registered outside the Raft proxy middleware.
func ServeMetrics(w http.ResponseWriter, r *http.Request) {
	runtime := installed.Load()
	if runtime == nil {
		http.Error(w, "telemetry not initialized", http.StatusServiceUnavailable)
		return
	}
	runtime.Handler.ServeHTTP(w, r)
}

type requestObservation struct{ businessError bool }
type requestObservationKey struct{}

// BeginHTTP uses a route template or _unmatched, never an arbitrary request URL.
func BeginHTTP(r *http.Request, route string) (*http.Request, func(int)) {
	if route == "" {
		route = "_unmatched"
	}
	method := r.Method
	switch method {
	case "GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS":
	default:
		method = "OTHER"
	}
	attrs := []attribute.KeyValue{attribute.String("route", route), attribute.String("method", method)}
	ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))
	ctx, span := tracer.Start(ctx, method+" "+route, trace.WithSpanKind(trace.SpanKindServer), trace.WithAttributes(attrs...))
	observation := &requestObservation{}
	ctx = context.WithValue(ctx, requestObservationKey{}, observation)
	started := time.Now()
	httpInflight.Add(1)
	return r.WithContext(ctx), func(status int) {
		httpInflight.Add(-1)
		httpRequests.instrument.Add(ctx, 1, metric.WithAttributes(append(attrs, attribute.String("status_class", strconv.Itoa(status/100)+"xx"))...))
		httpDuration.Record(ctx, time.Since(started).Seconds(), metric.WithAttributes(attrs...))
		if observation.businessError {
			httpBusinessErrors.instrument.Add(ctx, 1, metric.WithAttributes(attribute.String("route", route)))
		}
		if status >= 500 || observation.businessError {
			span.SetStatus(codes.Error, "request failed")
		}
		span.SetAttributes(attribute.Int("http.response.status_code", status))
		span.End()
	}
}

func MarkBusinessError(ctx context.Context) {
	if observation, ok := ctx.Value(requestObservationKey{}).(*requestObservation); ok {
		observation.businessError = true
	}
}

// InjectTrace propagates only W3C trace context, never arbitrary baggage.
func InjectTrace(r *http.Request) {
	otel.GetTextMapPropagator().Inject(r.Context(), propagation.HeaderCarrier(r.Header))
}
