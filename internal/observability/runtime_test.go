package observability

import (
	"context"
	"encoding/hex"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openark/orchestrator/internal/models/vo"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

func scrape(t *testing.T, r *Runtime) map[string]*dto.MetricFamily {
	t.Helper()
	w := httptest.NewRecorder()
	r.Handler.ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	if w.Code != 200 {
		t.Fatalf("scrape: %d %s", w.Code, w.Body.String())
	}
	p := expfmt.NewTextParser(model.UTF8Validation)
	families, err := p.TextToMetricFamilies(w.Body)
	if err != nil {
		t.Fatal(err)
	}
	return families
}
func counterValue(t *testing.T, families map[string]*dto.MetricFamily, name string, labels map[string]string) float64 {
	t.Helper()
	f := families[name]
	if f == nil {
		t.Fatalf("missing %s", name)
	}
	for _, m := range f.Metric {
		match := true
		for key, value := range labels {
			found := false
			for _, l := range m.Label {
				if l.GetName() == key && l.GetValue() == value {
					found = true
				}
			}
			match = match && found
		}
		if match {
			return m.GetCounter().GetValue()
		}
	}
	t.Fatalf("missing labels %v in %s", labels, name)
	return 0
}

func TestRuntimeContract(t *testing.T) {
	var exports atomic.Int64
	var failExports atomic.Bool
	var receivedMu sync.Mutex
	var received []*tracepb.Span
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/traces" {
			t.Errorf("OTLP path %s", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		var batch collectortrace.ExportTraceServiceRequest
		if err := proto.Unmarshal(body, &batch); err != nil {
			t.Error(err)
		}
		receivedMu.Lock()
		for _, resource := range batch.ResourceSpans {
			for _, scope := range resource.ScopeSpans {
				received = append(received, scope.Spans...)
			}
		}
		receivedMu.Unlock()
		exports.Add(1)
		if failExports.Load() {
			w.WriteHeader(503)
			return
		}
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(200)
	}))
	defer receiver.Close()
	r, err := New(t.Context(), receiver.URL+"/v1/traces", 1, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	r.Install()
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	})
	t.Run("event counts and duration units", func(t *testing.T) {
		ctx, span := StartSpan(t.Context(), "fixture.operation")
		defer span.End()
		RecordDiscovery(ctx, "success", time.Second, 200*time.Millisecond, 800*time.Millisecond)
		RecordDiscovery(ctx, "failure", 2*time.Second, time.Second, time.Second)
		RecordBackendWrite(ctx, "failure", 50*time.Millisecond, 150*time.Millisecond)
		RecordFlush(ctx, errors.New("secret must not be exported"), time.Second, 17)
		f := scrape(t, r)
		if got := counterValue(t, f, "orchestrator_discovery_operations_total", map[string]string{"result": "success"}); got != 1 {
			t.Fatalf("success=%v", got)
		}
		if got := counterValue(t, f, "orchestrator_discovery_operations_total", map[string]string{"result": "failure"}); got != 1 {
			t.Fatalf("failure=%v", got)
		}
		if got := counterValue(t, f, "orchestrator_write_buffer_flushes_total", map[string]string{"result": "failure"}); got != 1 {
			t.Fatalf("flush=%v", got)
		}
		for _, m := range f["orchestrator_discovery_duration_seconds"].Metric {
			for _, l := range m.Label {
				if l.GetName() == "phase" && l.GetValue() == "total" {
					if m.Histogram.GetSampleCount() != 2 || m.Histogram.GetSampleSum() != 3 {
						t.Fatalf("histogram: %v", m)
					}
				}
			}
		}
	})
	t.Run("recovery emits one completion on early return", func(t *testing.T) {
		func() { _, finish := BeginRecovery(t.Context(), "dead_master"); defer finish("failure") }()
		f := scrape(t, r)
		if counterValue(t, f, "orchestrator_recovery_started_total", map[string]string{"kind": "dead_master"}) != 1 {
			t.Fatal("missing recovery start")
		}
		if counterValue(t, f, "orchestrator_recovery_completed_total", map[string]string{"kind": "dead_master", "result": "failure"}) != 1 {
			t.Fatal("missing early completion")
		}
	})
	t.Run("gauge can fall and stale health fails closed", func(t *testing.T) {
		var value atomic.Int64
		Gauge("orchestrator_fixture_gauge", "Fixture current size", value.Load)
		value.Store(3)
		if got := scrape(t, r)["orchestrator_fixture_gauge"].Metric[0].Gauge.GetValue(); got != 3 {
			t.Fatal(got)
		}
		value.Store(1)
		if got := scrape(t, r)["orchestrator_fixture_gauge"].Metric[0].Gauge.GetValue(); got != 1 {
			t.Fatal(got)
		}
		SetHealth(vo.Health{CheckedAt: time.Now().Add(-time.Minute), Ready: true, LeaderReady: true, Backend: true, RaftReady: true})
		h := CurrentHealth()
		if h.Ready || h.LeaderReady || h.Backend || h.RaftReady {
			t.Fatalf("stale healthy: %+v", h)
		}
	})
	t.Run("HTTP parent propagation and bounded route", func(t *testing.T) {
		req := httptest.NewRequest("GET", "http://example.invalid/api/instance/private-host?token=secret", nil)
		req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
		req, finish := BeginHTTP(req, "/api/instance/:host")
		sc := trace.SpanContextFromContext(req.Context())
		if sc.TraceID().String() != "4bf92f3577b34da6a3ce929d0e0e4736" {
			t.Fatal(sc)
		}
		InjectTrace(req)
		if !strings.Contains(req.Header.Get("traceparent"), sc.SpanID().String()) {
			t.Fatal("proxy trace context missing")
		}
		MarkBusinessError(req.Context())
		RecordSQL(req.Context(), "dynamic", time.Now(), errors.New("secret private-host SQL argument"))
		finish(200)
		f := scrape(t, r)
		if counterValue(t, f, "orchestrator_http_business_errors_total", map[string]string{"route": "/api/instance/:host"}) != 1 {
			t.Fatal("business failure not counted")
		}
		for _, family := range f {
			for _, m := range family.Metric {
				for _, l := range m.Label {
					if strings.Contains(l.GetValue(), "secret") || strings.Contains(l.GetValue(), "private-host") {
						t.Fatal("sensitive metric label")
					}
				}
			}
		}
	})
	t.Run("SDK limits attribute cardinality", func(t *testing.T) {
		c := NewCounter("orchestrator_fixture_cardinality_total", "Fixture cap")
		for i := range 2100 {
			c.instrument.Add(t.Context(), 1, metric.WithAttributes(attribute.Int("value", i)))
		}
		f := scrape(t, r)["orchestrator_fixture_cardinality_total"]
		if len(f.Metric) > 2001 {
			t.Fatalf("unbounded series %d", len(f.Metric))
		}
		sum := float64(0)
		for _, m := range f.Metric {
			sum += m.Counter.GetValue()
		}
		if sum != 2100 {
			t.Fatalf("overflow lost events %v", sum)
		}
	})
	t.Run("export failure is observable without failing readiness", func(t *testing.T) {
		failExports.Store(true)
		SetHealth(vo.Health{CheckedAt: time.Now(), Backend: true, Ready: true})
		_, span := StartSpan(t.Context(), "fixture.export_failure")
		span.End()
		if err := r.Traces.ForceFlush(t.Context()); err == nil {
			t.Fatal("export failure hidden")
		}
		if counterValue(t, scrape(t, r), "orchestrator_trace_export_failures_total", nil) < 1 {
			t.Fatal("export failure missing")
		}
		if !CurrentHealth().Ready {
			t.Fatal("trace exporter failure affected business readiness")
		}
		failExports.Store(false)
	})
	t.Run("OTLP flush and idempotent shutdown", func(t *testing.T) {
		_, span := StartSpan(t.Context(), "fixture.flush")
		span.End()
		if err := r.Traces.ForceFlush(t.Context()); err != nil {
			t.Fatal(err)
		}
		if exports.Load() == 0 {
			t.Fatal("no OTLP export")
		}
		receivedMu.Lock()
		spans := append([]*tracepb.Span(nil), received...)
		receivedMu.Unlock()
		var httpSpan, sqlSpan *tracepb.Span
		for _, span := range spans {
			encoded := span.String()
			if strings.Contains(encoded, "secret") || strings.Contains(encoded, "private-host") {
				t.Fatal("sensitive content exported in trace")
			}
			if span.Name == "GET /api/instance/:host" {
				httpSpan = span
			}
			if span.Name == "sql.dynamic" {
				sqlSpan = span
			}
		}
		if httpSpan == nil || sqlSpan == nil {
			t.Fatal("HTTP/SQL spans missing from OTLP payload")
		}
		if hex.EncodeToString(httpSpan.TraceId) != "4bf92f3577b34da6a3ce929d0e0e4736" || hex.EncodeToString(httpSpan.ParentSpanId) != "00f067aa0ba902b7" || hex.EncodeToString(sqlSpan.ParentSpanId) != hex.EncodeToString(httpSpan.SpanId) {
			t.Fatal("OTLP payload lost upstream/HTTP/SQL parent chain")
		}
		if err := r.Close(); err != nil {
			t.Fatal(err)
		}
		if err := r.Close(); err != nil {
			t.Fatal(err)
		}
	})
}

func TestInvalidSampler(t *testing.T) {
	for _, ratio := range []float64{-1, 2, math.NaN()} {
		r, err := New(t.Context(), "", ratio, "")
		if err == nil {
			_ = r.Close()
			t.Fatalf("accepted %v", ratio)
		}
	}
}

func BenchmarkTelemetry(b *testing.B) {
	// Global instruments bind once. Run this benchmark with -run='^$' so the
	// contract test's already-shutdown provider cannot turn it into a no-op.
	if installed.Load() != nil {
		b.Skip("run with -run='^$' to benchmark an active provider")
	}
	r, err := New(b.Context(), "", 0, "benchmark")
	if err != nil {
		b.Fatal(err)
	}
	r.Install()
	b.Cleanup(func() { _ = r.Close() })
	b.Run("RecordDiscovery", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			RecordDiscovery(context.Background(), "success", time.Second, 0, time.Second)
		}
	})
	b.Run("Scrape", func(b *testing.B) {
		request := httptest.NewRequest("GET", "/metrics", nil)
		b.ReportAllocs()
		for b.Loop() {
			response := httptest.NewRecorder()
			r.Handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				b.Fatal(response.Code)
			}
		}
	})
}
