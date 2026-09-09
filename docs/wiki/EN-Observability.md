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

The health monitor refreshes every five seconds, uses a two-second backend context, and invalidates readiness when its snapshot is older than fifteen seconds. Scrapes read the cached local snapshot and do not run a database query or Raft verification. Trace export failure is observable but does not make application readiness fail.

## Metric contract

Metric labels are deliberately bounded. The main families cover:

- discovery starts, outcomes, phase duration, queue state, recent/dead instance counts, and poll overruns;
- backend write tasks, semaphore wait/execute duration, write-buffer size, flush outcome, duration, and batch size;
- recovery starts, outcomes, duration, and pending recovery count;
- HTTP request count/duration, business errors, and inflight requests using template routes rather than concrete hosts;
- SQL operation count/duration by bounded layer, plus process-owned backend pool statistics;
- local readiness, backend readiness, active/leader state, Raft role and log/commit/applied indexes;
- trace export failures and standard `go_*` / `process_*` collectors.

All durations use seconds; counters end in `_total`; gauges may decrease. Discovery `phase=total` contains its child phases and must not be added to them. A failed write-buffer flush may have partially written rows. SQL text, bound values, addresses, request IDs, trace IDs, and error strings are excluded from labels and spans.

## Traces

```json
{
  "OTelTraceEndpoint": "https://collector.example.com/v1/traces",
  "OTelTraceSampleRatio": 0.1
}
```

An empty endpoint disables trace export; metrics remain enabled. The endpoint must be an HTTP(S) `/v1/traces` URL without userinfo, query, or fragment. Standard OTLP exporter environment settings supply headers/TLS material. Both settings require restart. Export failure does not make application readiness fail.

The exporter uses a bounded queue of 2048 spans, batches at most 256, and applies a three-second export timeout. Application code neither blocks for queue space nor adds an independent retry loop. Shutdown has a five-second flush/shutdown budget. The ratio controls root sampling; upstream W3C sampling decisions are inherited, while baggage is not propagated automatically.

## Dashboards and alerts

Import [`resources/metrics/orchestrator-grafana.json`](https://github.com/SisyphusSQ/orchestrator/blob/main/resources/metrics/orchestrator-grafana.json). Example alert rules and their tests live under [`resources/metrics/`](https://github.com/SisyphusSQ/orchestrator/tree/main/resources/metrics). Thresholds are examples, not approved production SLOs; tune them with real traffic and ownership.

Graphite, `/debug/metrics`, and the historical raw/aggregated in-memory metric APIs have been removed. Delete `GraphiteAddr`, `GraphitePath`, `GraphiteConvertHostnameDotsToUnderscores`, `GraphitePollSeconds`, `DiscoveryCollectionRetentionSeconds`, and `DiscoveryQueueMaxStatisticsSize` before upgrade; the new binary rejects them even when empty.

Validate telemetry in layers: parse and test Prometheus rules, scrape a running node through the real prefix/authentication/TLS path, execute dashboard queries, verify readiness failure modes, and separately exercise an OTLP receiver. A 200 response or importable JSON does not prove collection, alerts, traces, real MySQL, multi-node Raft, or production SLO behavior.
