// Package observability owns the bounded metric and trace vocabulary.
package observability

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

var meter = otel.Meter("github.com/openark/orchestrator")
var tracer = otel.Tracer("github.com/openark/orchestrator")

// Counter binds only static, reviewed attributes. Values must be non-negative.
type Counter struct {
	instrument metric.Int64Counter
	options    metric.AddOption
}

// NewCounter declares a code-owned instrument; invalid definitions are programmer errors.
func NewCounter(name, description string, attrs ...attribute.KeyValue) Counter {
	c, err := meter.Int64Counter(name, metric.WithDescription(description))
	if err != nil {
		panic(err)
	}
	return Counter{c, metric.WithAttributes(attrs...)}
}

func (c Counter) Add(ctx context.Context, n int64) { c.instrument.Add(ctx, n, c.options) }

// Gauge reads local synchronized state only. Callbacks must not perform network IO.
func Gauge(name, description string, read func() int64, attrs ...attribute.KeyValue) {
	_, err := meter.Int64ObservableGauge(name, metric.WithDescription(description), metric.WithInt64Callback(func(ctx context.Context, o metric.Int64Observer) error {
		o.Observe(read(), metric.WithAttributes(attrs...))
		return nil
	}))
	if err != nil {
		panic(err)
	}
}

func duration(name, description string) metric.Float64Histogram {
	h, err := meter.Float64Histogram(name, metric.WithUnit("s"), metric.WithDescription(description), metric.WithExplicitBucketBoundaries(.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30, 60))
	if err != nil {
		panic(err)
	}
	return h
}

var DiscoveryStarted = NewCounter("orchestrator_discovery_started_total", "Discovery attempts after cache and freshness checks")
var discoveryCompleted = NewCounter("orchestrator_discovery_operations_total", "Completed discovery attempts")
var discoveryDuration = duration("orchestrator_discovery_duration_seconds", "Discovery attempt duration by phase; phases are not additive")
var backendCompleted = NewCounter("orchestrator_backend_write_operations_total", "Completed serialized backend write tasks, not SQL statements")
var backendDuration = duration("orchestrator_backend_write_duration_seconds", "Backend task semaphore wait and execution duration")
var flushCompleted = NewCounter("orchestrator_write_buffer_flushes_total", "Nonempty write buffer flush results")
var flushDuration = duration("orchestrator_write_buffer_flush_duration_seconds", "Nonempty flush execution including backend task wait")
var flushBatch = func() metric.Int64Histogram {
	h, e := meter.Int64Histogram("orchestrator_write_buffer_flush_batch_size", metric.WithDescription("Instances attempted per nonempty flush"), metric.WithExplicitBucketBoundaries(1, 10, 50, 100, 500, 1000, 5000))
	if e != nil {
		panic(e)
	}
	return h
}()
var recoveryDuration = duration("orchestrator_recovery_duration_seconds", "Recovery attempt duration from registration through its terminal path")

// Result classifies a returned error without exporting its possibly sensitive text.
func Result(err error) string {
	if err != nil {
		return "failure"
	}
	return "success"
}

func RecordDiscovery(ctx context.Context, result string, total, backend, instance time.Duration) {
	discoveryCompleted.instrument.Add(ctx, 1, metric.WithAttributes(attribute.String("result", result)))
	for phase, elapsed := range map[string]time.Duration{"total": total, "backend": backend, "instance": instance} {
		discoveryDuration.Record(ctx, elapsed.Seconds(), metric.WithAttributes(attribute.String("phase", phase)))
	}
}
func RecordBackendWrite(ctx context.Context, result string, wait, execute time.Duration) {
	backendCompleted.instrument.Add(ctx, 1, metric.WithAttributes(attribute.String("result", result)))
	backendDuration.Record(ctx, wait.Seconds(), metric.WithAttributes(attribute.String("phase", "wait")))
	backendDuration.Record(ctx, execute.Seconds(), metric.WithAttributes(attribute.String("phase", "execute")))
}
func RecordFlush(ctx context.Context, err error, elapsed time.Duration, size int) {
	flushCompleted.instrument.Add(ctx, 1, metric.WithAttributes(attribute.String("result", Result(err))))
	flushDuration.Record(ctx, elapsed.Seconds())
	flushBatch.Record(ctx, int64(size))
}
func RecordRecovery(ctx context.Context, kind string, elapsed time.Duration) {
	recoveryDuration.Record(ctx, elapsed.Seconds(), metric.WithAttributes(attribute.String("kind", kind)))
}

// StartSpan returns a span whose name is a static operation, never a URL or SQL string.
func StartSpan(ctx context.Context, operation string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	return tracer.Start(ctx, operation, trace.WithAttributes(attrs...))
}

// EndSpan deliberately excludes raw errors, SQL, addresses and credentials.
func EndSpan(span trace.Span, err error) {
	if err != nil {
		span.SetStatus(codes.Error, "operation failed")
	}
	span.End()
}

var sqlCompleted = NewCounter("orchestrator_sql_operations_total", "SQL executions at the GORM or dynamic adapter boundary")
var sqlDuration = duration("orchestrator_sql_duration_seconds", "SQL execution and result consumption duration")

// RecordSQL observes an already completed SQL operation without retaining SQL text or arguments.
func RecordSQL(ctx context.Context, layer string, begin time.Time, err error) {
	attrs := []attribute.KeyValue{attribute.String("layer", layer), attribute.String("result", Result(err))}
	sqlCompleted.instrument.Add(ctx, 1, metric.WithAttributes(attrs...))
	sqlDuration.Record(ctx, time.Since(begin).Seconds(), metric.WithAttributes(attribute.String("layer", layer)))
	_, span := tracer.Start(ctx, "sql."+layer, trace.WithTimestamp(begin), trace.WithAttributes(attrs...))
	EndSpan(span, err)
}

func seedCounters() {
	ctx := context.Background()
	DiscoveryStarted.Add(ctx, 0)
	for _, result := range []string{"success", "failure", "skipped"} {
		discoveryCompleted.instrument.Add(ctx, 0, metric.WithAttributes(attribute.String("result", result)))
	}
	for _, result := range []string{"success", "failure"} {
		backendCompleted.instrument.Add(ctx, 0, metric.WithAttributes(attribute.String("result", result)))
		flushCompleted.instrument.Add(ctx, 0, metric.WithAttributes(attribute.String("result", result)))
	}
	for _, kind := range []string{"dead_master", "dead_intermediate_master", "dead_co_master", "dead_replication_group_member"} {
		NewCounter("orchestrator_recovery_started_total", "Recovery starts", attribute.String("kind", kind)).Add(ctx, 0)
		for _, result := range []string{"success", "failure"} {
			NewCounter("orchestrator_recovery_completed_total", "Recovery completions", attribute.String("kind", kind), attribute.String("result", result)).Add(ctx, 0)
		}
	}
	traceExportFailures.Add(ctx, 0)
}

// BeginRecovery records exactly one terminal result per registered attempt, including early returns.
// Result describes promotion success; hook failures are separate child spans.
func BeginRecovery(parent context.Context, kind string) (context.Context, func(string)) {
	ctx, span := StartSpan(parent, "recovery.attempt", attribute.String("kind", kind))
	NewCounter("orchestrator_recovery_started_total", "Recovery starts", attribute.String("kind", kind)).Add(ctx, 1)
	started := time.Now()
	return ctx, func(result string) {
		NewCounter("orchestrator_recovery_completed_total", "Recovery completions", attribute.String("kind", kind), attribute.String("result", result)).Add(ctx, 1)
		RecordRecovery(ctx, kind, time.Since(started))
		span.SetAttributes(attribute.String("result", result))
		if result == "failure" {
			span.SetStatus(codes.Error, "recovery failed")
		}
		span.End()
	}
}
