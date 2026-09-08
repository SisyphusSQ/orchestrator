# 可观测性

[English](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Observability) · **中文** · [Wiki 首页](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

每个 HTTP listener 都提供节点本地的 Prometheus 指标与健康快照。这些路由遵循 `URLPrefix`、现有认证、TLS 和 mTLS，并且不会代理到 Raft Leader。

| 路由 | 含义 | 健康响应 |
| --- | --- | --- |
| `/metrics` | Prometheus/OpenMetrics 抓取 | 初始化后 200 |
| `/health/live` | HTTP 进程存活，不探测依赖 | 200 |
| `/health/ready` | 后端与本地 Raft 状态新鲜且就绪 | 200，否则 503 |
| `/health/leader-ready` | 本节点就绪且是 Leader | 200，否则 503 |
| `/api/status` | 历史应用状态契约 | 保留原 API 语义 |

Prometheus 必须逐个抓取 Raft 节点，不能只访问 Leader VIP。可在 target 侧添加 `orchestrator_cluster` 等固定部署标签；不要把 MySQL hostname、集群名、SQL、错误全文或用户输入放进指标标签。

## Traces

```json
{
  "OTelTraceEndpoint": "https://collector.example.com/v1/traces",
  "OTelTraceSampleRatio": 0.1
}
```

endpoint 为空时关闭 trace 导出，但指标继续启用。地址必须是以 `/v1/traces` 结尾的 HTTP(S) URL，且不能包含 userinfo、query 或 fragment。认证头和 TLS 材料使用标准 OTLP exporter 环境配置。这两个设置改变后需要重启；导出失败不会让应用 readiness 失败。

## 大盘与告警

导入 [`resources/metrics/orchestrator-grafana.json`](https://github.com/SisyphusSQ/orchestrator/blob/main/resources/metrics/orchestrator-grafana.json)。示例告警及测试位于 [`resources/metrics/`](https://github.com/SisyphusSQ/orchestrator/tree/main/resources/metrics)。其中阈值是可执行示例，不是已批准的生产 SLO，应结合真实流量和责任人调整。

Graphite、`/debug/metrics` 以及历史 raw/aggregated 内存指标 API 已删除，升级前必须移除对应配置。指标名、基数上限、直方图语义、trace 覆盖和回滚边界见[可观测性深度参考](https://github.com/SisyphusSQ/orchestrator/blob/main/docs/observability.md)。
