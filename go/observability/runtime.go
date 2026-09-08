package observability

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/openark/golib/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	exporter "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Runtime owns telemetry resources for one process lifetime.
type Runtime struct {
	Metrics   *sdkmetric.MeterProvider
	Traces    *sdktrace.TracerProvider
	Handler   http.Handler
	closeOnce sync.Once
	closeErr  error
}

var traceExportFailures = NewCounter("orchestrator_trace_export_failures_total", "Failed trace export batches")

type traceExporter struct{ sdktrace.SpanExporter }

func (e traceExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	err := e.SpanExporter.ExportSpans(ctx, spans)
	if err != nil {
		traceExportFailures.Add(ctx, 1)
	}
	return err
}

// New creates providers without installing global state. Endpoint is a full OTLP HTTP trace URL.
func New(ctx context.Context, endpoint string, ratio float64, version string) (*Runtime, error) {
	if math.IsNaN(ratio) || ratio < 0 || ratio > 1 {
		return nil, fmt.Errorf("trace sample ratio must be between 0 and 1")
	}
	registry := prometheus.NewRegistry()
	registry.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	exp, err := exporter.New(exporter.WithRegisterer(registry), exporter.WithoutScopeInfo())
	if err != nil {
		return nil, fmt.Errorf("create Prometheus exporter: %w", err)
	}
	res := resource.NewSchemaless(attribute.String("service.name", "orchestrator"), attribute.String("service.version", version))
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithResource(res), sdkmetric.WithReader(exp), sdkmetric.WithCardinalityLimit(2000))
	opts := []sdktrace.TracerProviderOption{sdktrace.WithResource(res), sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratio)))}
	if endpoint != "" {
		exp, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(endpoint), otlptracehttp.WithTimeout(3*time.Second), otlptracehttp.WithRetry(otlptracehttp.RetryConfig{Enabled: false}))
		if err != nil {
			_ = mp.Shutdown(ctx)
			return nil, fmt.Errorf("create trace exporter: %w", err)
		}
		opts = append(opts, sdktrace.WithBatcher(traceExporter{exp}, sdktrace.WithMaxQueueSize(2048), sdktrace.WithMaxExportBatchSize(256), sdktrace.WithExportTimeout(3*time.Second)))
	} else {
		opts = append(opts, sdktrace.WithSampler(sdktrace.NeverSample()))
	}
	return &Runtime{Metrics: mp, Traces: sdktrace.NewTracerProvider(opts...), Handler: promhttp.HandlerFor(registry, promhttp.HandlerOpts{EnableOpenMetrics: true, DisableCompression: true, Timeout: 5 * time.Second, MaxRequestsInFlight: 2})}, nil
}

// Install is called once, before business workers start. Instrument declarations made in init bind here.
func (r *Runtime) Install() {
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(error) { log.Error("telemetry SDK/export failed; inspect exporter health and configuration") }))
	otel.SetMeterProvider(r.Metrics)
	otel.SetTracerProvider(r.Traces)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	installed.Store(r)
	seedCounters()
}

// Close flushes trace batches before shutting down metrics and is safe to call twice.
func (r *Runtime) Close() error {
	r.closeOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		r.closeErr = errors.Join(r.Traces.Shutdown(ctx), r.Metrics.Shutdown(ctx))
	})
	return r.closeErr
}
