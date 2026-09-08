# Observability

**English** · [中文](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Observability) · [Wiki home](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

Each HTTP listener exposes node-local Prometheus metrics and health snapshots. These routes honor `URLPrefix`, existing authentication, TLS, and mTLS, and are never proxied to the Raft leader.

| Route | Meaning | Healthy response |
| --- | --- | --- |
| `/metrics` | Prometheus/OpenMetrics scrape | 200 after initialization |
| `/health/live` | HTTP process liveness; no dependency probe | 200 |
| `/health/ready` | Fresh backend and local Raft readiness | 200, otherwise 503 |
| `/health/leader-ready` | This node is ready and is leader | 200, otherwise 503 |
| `/api/status` | Historical application status contract | existing API semantics |

Scrape every Raft node rather than a leader-only VIP. Add a fixed deployment label such as `orchestrator_cluster` at the Prometheus target; do not put MySQL hostnames, cluster names, SQL, errors, or user input into metric labels.

## Traces

```json
{
  "OTelTraceEndpoint": "https://collector.example.com/v1/traces",
  "OTelTraceSampleRatio": 0.1
}
```

An empty endpoint disables trace export; metrics remain enabled. The endpoint must be an HTTP(S) `/v1/traces` URL without userinfo, query, or fragment. Standard OTLP exporter environment settings supply headers/TLS material. Both settings require restart. Export failure does not make application readiness fail.

## Dashboards and alerts

Import [`resources/metrics/orchestrator-grafana.json`](https://github.com/SisyphusSQ/orchestrator/blob/main/resources/metrics/orchestrator-grafana.json). Example alert rules and their tests live under [`resources/metrics/`](https://github.com/SisyphusSQ/orchestrator/tree/main/resources/metrics). Thresholds are examples, not approved production SLOs; tune them with real traffic and ownership.

Graphite, `/debug/metrics`, and the historical raw/aggregated in-memory metric APIs have been removed. Delete their configuration before upgrade. See the detailed [observability reference](https://github.com/SisyphusSQ/orchestrator/blob/main/docs/observability.md) for metric names, cardinality caps, histogram semantics, tracing coverage, and rollback boundaries.
