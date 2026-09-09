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

健康监控每 5 秒刷新一次，元数据库检查使用 2 秒 context；快照超过 15 秒未更新时 readiness 失效。抓取只读取节点本地缓存快照，不临时执行数据库查询或 Raft 校验。Trace 导出失败保持可观测，但不会让应用 readiness 失败。

## 指标契约

指标标签经过有界设计，主要指标族覆盖：

- 发现开始、结果、分阶段耗时、队列状态、近期/故障实例数量和轮询超时；
- 元数据库写任务、semaphore 等待/执行耗时、写缓冲长度、flush 结果/耗时/批大小；
- 恢复开始、结果、耗时和 pending 数量；
- 使用模板路由而非具体主机名的 HTTP 请求数/耗时、业务错误和 inflight；
- 按有限 layer 区分的 SQL 操作数/耗时，以及进程级元数据库连接池状态；
- 本地 readiness、backend readiness、active/Leader 状态、Raft 角色与 log/commit/applied index；
- Trace 导出失败和标准 `go_*` / `process_*` collector。

所有耗时统一为秒，Counter 以 `_total` 结尾，Gauge 可以下降。发现的 `phase=total` 已包含子阶段，不能相加。写缓冲 flush 失败时可能已有部分行成功。SQL 文本、绑定参数、地址、request ID、trace ID 和错误字符串都不进入标签或 span。

## Traces

```json
{
  "OTelTraceEndpoint": "https://collector.example.com/v1/traces",
  "OTelTraceSampleRatio": 0.1
}
```

endpoint 为空时关闭 trace 导出，但指标继续启用。地址必须是以 `/v1/traces` 结尾的 HTTP(S) URL，且不能包含 userinfo、query 或 fragment。认证头和 TLS 材料使用标准 OTLP exporter 环境配置。这两个设置改变后需要重启；导出失败不会让应用 readiness 失败。

exporter 使用 2048 的有界队列，每批最多 256，导出超时 3 秒。应用不会为等待队列空间阻塞，也不会自行增加重试循环；关闭时最多使用 5 秒完成 flush/shutdown。ratio 只控制 root sampling，上游 W3C 采样决定会继承，baggage 不会自动传播。

## 大盘与告警

导入 [`resources/metrics/orchestrator-grafana.json`](https://github.com/SisyphusSQ/orchestrator/blob/main/resources/metrics/orchestrator-grafana.json)。示例告警及测试位于 [`resources/metrics/`](https://github.com/SisyphusSQ/orchestrator/tree/main/resources/metrics)。其中阈值是可执行示例，不是已批准的生产 SLO，应结合真实流量和责任人调整。

Graphite、`/debug/metrics` 以及历史 raw/aggregated 内存指标 API 已删除。升级前必须移除 `GraphiteAddr`、`GraphitePath`、`GraphiteConvertHostnameDotsToUnderscores`、`GraphitePollSeconds`、`DiscoveryCollectionRetentionSeconds` 和 `DiscoveryQueueMaxStatisticsSize`；即使为空，新二进制也会拒绝这些字段。

遥测验收需要分层完成：解析并测试 Prometheus 规则，通过真实 prefix/认证/TLS 路径抓取运行节点，执行大盘查询，验证 readiness 失败模式，并单独连接 OTLP receiver。HTTP 200 或 JSON 可导入不能证明采集、告警、trace、真实 MySQL、多节点 Raft 或生产 SLO 行为。
