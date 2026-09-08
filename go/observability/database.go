package observability

import (
	"context"
	"database/sql"
	"sync/atomic"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var backendPool atomic.Pointer[sql.DB]

// SetBackendPool observes the existing pool without acquiring ownership or opening connections.
func SetBackendPool(database *sql.DB) { backendPool.Store(database) }
func backendStats() sql.DBStats {
	p := backendPool.Load()
	if p == nil {
		return sql.DBStats{}
	}
	return p.Stats()
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
