package observability

import (
	"context"
	"sync/atomic"

	"github.com/openark/orchestrator/internal/models/domain"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type backendStatsProvider struct {
	read func() domain.DatabasePoolStats
}

var backendPoolStats atomic.Pointer[backendStatsProvider]

// SetBackendStatsProvider observes an existing repository-owned pool without
// exposing the raw database handle outside the repository layer.
func SetBackendStatsProvider(read func() domain.DatabasePoolStats) {
	backendPoolStats.Store(&backendStatsProvider{read: read})
}
func backendStats() domain.DatabasePoolStats {
	provider := backendPoolStats.Load()
	if provider == nil || provider.read == nil {
		return domain.DatabasePoolStats{}
	}
	return provider.read()
}
func init() {
	Gauge("orchestrator_backend_connections", "Existing backend pool connections", func() int64 { return int64(backendStats().InUse) }, attribute.String("state", "in_use"))
	Gauge("orchestrator_backend_connections", "Existing backend pool connections", func() int64 { return int64(backendStats().Idle) }, attribute.String("state", "idle"))
	Gauge("orchestrator_backend_max_connections", "Backend pool configured connection limit", func() int64 { return int64(backendStats().MaxOpenConnections) })
	_, err := meter.Int64ObservableCounter("orchestrator_backend_connection_wait_total", metric.WithDescription("Cumulative waits for a backend connection"), metric.WithInt64Callback(func(ctx context.Context, o metric.Int64Observer) error {
		o.Observe(backendStats().WaitCount)
		return nil
	}))
	if err != nil {
		panic(err)
	}
	_, err = meter.Float64ObservableCounter("orchestrator_backend_connection_wait_seconds_total", metric.WithUnit("s"), metric.WithDescription("Cumulative backend connection wait time"), metric.WithFloat64Callback(func(ctx context.Context, o metric.Float64Observer) error {
		o.Observe(backendStats().WaitDuration.Seconds())
		return nil
	}))
	if err != nil {
		panic(err)
	}
}
