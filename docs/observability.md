# 可观测性：Prometheus、OpenTelemetry 与 Grafana

TOO-412 将 Graphite、rcrowley/go-metrics、自研 Collection 和队列历史样本 API 直接替换为 OTel Metrics + Prometheus；Trace 使用 OTel SDK 与 OTLP HTTP。旧消费者已确认停用，不提供双写或 raw 样本兼容层。

## 接入

业务 HTTP listener 提供以下节点本地接口，均支持 `URLPrefix`、GET/HEAD、既有路由认证、TLS/mTLS 验证，不经过 Raft leader 代理：

| 接口 | 用途 | 状态 |
| --- | --- | --- |
| `/metrics` | Prometheus/OpenMetrics 抓取 | 200；未初始化 503 |
| `/health/live` | 进程 HTTP 响应能力 | 200，不探测依赖 |
| `/health/ready` | backend 可用且满足当前模式的服务条件 | 可用 200，否则 503 |
| `/health/leader-ready` | 本节点具备 leader 工作条件 | 可用 200，否则 503；follower 返回 503 |

`/api/status` 保持原有行为。新健康接口返回本节点快照，不将 follower 自动代理给 leader。Raft 为唯一运行架构；就绪要求元数据库可用且本节点 Raft 就绪。未 bootstrap、未加入配置或失去联系时不会回退为非 Raft 就绪。

健康监控每 5 秒检查一次，backend 检查使用 2 秒 context；Raft 复用现有状态/领导权验证。超过 15 秒没有更新时 readiness 失效。抓取接口只读取本地同步状态，不发起数据库查询或 Raft 验证。Trace exporter 故障不使业务 readiness 失败。

Prometheus 必须逐节点抓取，不能使用仅指向 leader 的 VIP。为每个目标添加固定的 `orchestrator_cluster` 标签，表示 orchestrator 部署集群，区别于被管理的 MySQL 集群。`job`、`instance` 来自 Prometheus target。

配置增加：

```json
{
  "OTelTraceEndpoint": "http://127.0.0.1:14318/v1/traces",
  "OTelTraceSampleRatio": 0.1
}
```

- Endpoint 默认空，表示关闭 trace export；metrics 始终可抓取。配置完整 `/v1/traces` URL，不允许 userinfo、query、fragment。远程部署推荐 HTTPS，通过标准 OTLP exporter TLS/header 环境配置提供认证材料；禁止把凭据写到 URL。
- Ratio 默认 0.1，仅决定 root span 采样；W3C 上游采样决定会被继承，测试环境可设 1。只传播 trace context，不自动传播 baggage。
- 这两个配置需要重启；SIGHUP/API reload 尝试改变它们会失败，且不应用候选配置。其他配置沿用已有行为。
- Trace batch 队列容量 2048，每批最多 256，导出超时 3 秒；应用侧不做重试或阻塞业务等待队列空间。关闭时最多使用 5 秒 flush/shutdown。
- Gin 负责 HTTP 响应压缩，Prometheus handler 禁用自身压缩，避免二次 gzip。

## 指标契约

时间统一使用秒，计数器以 `_total` 结尾，Gauge 表达可回落的当前状态。下列名称是实际 Prometheus 暴露名称。

| 指标族 | 类型 / 标签 | 语义 |
| --- | --- | --- |
| `orchestrator_discovery_started_total` | Counter | 缓存与新鲜度检查后实际开始的探测 |
| `orchestrator_discovery_operations_total` | Counter / result=success,failure,skipped | 已开始探测的终态；探测过滤为 skipped，探测错误或没有实例为 failure |
| `orchestrator_discovery_duration_seconds` | Histogram / phase=total,backend,instance | 尝试耗时；total 包含子阶段，不能相加；前置缓存命中不产生样本 |
| `orchestrator_discovery_queue_items` | Gauge / queue=DEFAULT,DEADINSTANCES; state=queued,active | 去重 key 数量；queued 不重复累加 channel 和 map |
| `orchestrator_discovery_recent_instances` | Gauge | 当前最近探测缓存条目数，对应旧 discoveries.recent_count |
| `orchestrator_dead_instances` | Gauge | 当前死实例过滤器大小，可增可减 |
| `orchestrator_discoveries_instance_poll_seconds_exceeded_total` | Counter | 发现处理超过轮询周期，保留原有触发点 |
| `orchestrator_backend_write_operations_total` | Counter / result=success,failure | semaphore 包围的写任务数，不是 SQL 条数；错误、取消与 panic 分支记失败 |
| `orchestrator_backend_write_duration_seconds` | Histogram / phase=wait,execute | semaphore 等待和任务执行分开 |
| `orchestrator_write_buffer_items` | Gauge | 待写 instance channel 当前长度 |
| `orchestrator_write_buffer_flushes_total` | Counter / result=success,failure | 每次非空 flush 一次；失败不保证整个批次未写入 |
| `orchestrator_write_buffer_flush_duration_seconds` | Histogram | 批次执行时间，包含 backend task 等待，不包含两次 flush 间隔 |
| `orchestrator_write_buffer_flush_batch_size` | Histogram | 该批次尝试写入的实例数 |
| `orchestrator_recovery_started_total` | Counter / kind | 进入注册后的恢复尝试 |
| `orchestrator_recovery_completed_total` | Counter / kind,result | 每次开始对应一个函数终态；提前失败也记录；success 按产生提升目标判定，hook 失败另见 trace |
| `orchestrator_recovery_duration_seconds` | Histogram / kind | 注册后到恢复函数返回的耗时，不含调用方后续 postponed 工作 |
| `orchestrator_recovery_pending` | Gauge | 现有 pending recovery 计数 |
| `orchestrator_http_requests_total` | Counter / route,method,status_class | 请求终态；模板 route 不含具体 hostname 或 query，未知路由统一 `_unmatched`，未知 method 统一 OTHER |
| `orchestrator_http_request_duration_seconds` | Histogram / route,method | 请求耗时；不交叉附加状态码，限制桶数量 |
| `orchestrator_http_business_errors_total` | Counter / route | `APIResponse.Code=ERROR`，独立于 HTTP 状态 |
| `orchestrator_http_inflight` | Gauge | 当前 HTTP 请求，含当前抓取请求 |
| `orchestrator_sql_operations_total` | Counter / layer=backend,backend_adapter,dynamic; result | GORM 与受控动态读取边界；不是所有 topology SQL 的完整审计 |
| `orchestrator_sql_duration_seconds` | Histogram / layer | 执行及结果读取耗时，SQL 文本和参数不进入标签或 span |
| `orchestrator_backend_connections` | Gauge / state=idle,in_use | 现有进程级 backend pool 的统计，不打开第二套连接池 |
| `orchestrator_backend_max_connections` | Gauge | backend pool 连接上限 |
| `orchestrator_backend_connection_wait_total` / `orchestrator_backend_connection_wait_seconds_total` | Counter | pool 累计等待次数及时间 |
| `orchestrator_ready` / `orchestrator_backend_ready` / `orchestrator_active` / `orchestrator_leader_ready` | Gauge / 0,1 | 缓存的本地健康及角色状态 |
| `orchestrator_raft_ready` / `orchestrator_raft_leader` | Gauge / 0,1 | 本节点 Raft 就绪与领导角色 |
| `orchestrator_raft_last_index` / `orchestrator_raft_commit_index` / `orchestrator_raft_applied_index` | Gauge | 本地 Raft 进度索引；不添加成员地址标签 |
| `orchestrator_trace_export_failures_total` | Counter | exporter 返回失败的批次，SDK 其他错误使用脱敏日志 |
| `go_*` / `process_*` | 标准 collector | Go runtime / 进程状态；可用的 process 指标随平台变化，以目标实际暴露结果为准 |

`kind` 固定为 `dead_master`、`dead_intermediate_master`、`dead_co_master`、`dead_replication_group_member`。无流量的结果 Counter 预置零；未产生样本的 Histogram 不伪造零延迟。

其他旧标量事件保持原触发点，映射为 `orchestrator_<旧名称的点改下划线>_total`：`instance.access_denied/read_topology/read/write`、`instance_tls.read/write/read_cache/write_cache`、`audit.write`、`analysis.change.write.attempt/write`、`resolve.write_resolved/write_unresolved/read_resolved/read_unresolved/read_resolved_all`。这些触发点中部分表示尝试而非成功，不应当作业务成功率分子。

## 基数、桶和资源开销

- 业务标签不包含 IP/hostname、MySQL cluster 名、SQL、错误全文、request ID、trace ID、任意 URL、用户输入。新增维度必须先说明有限取值范围。
- 每个 SDK instrument 使用硬上限 2000 个属性集合，超过部分进入 OTel overflow 聚合，不静默丢弃总量。正常数据不应依赖 overflow；部署方可检查 `otel_metric_overflow="true"`。
- 时间 Histogram 使用 14 个有限桶：0.001、0.005、0.01、0.025、0.05、0.1、0.25、0.5、1、2.5、5、10、30、60 秒；classic 展开后每个属性组合为 17 条时序（含 +Inf/count/sum）。单个 Histogram 的上限约为 34000 条时序，HTTP route×method 是主要来源，其余业务 Histogram 只有少量枚举。
- 基数 cap 是保护上限，不是容量目标。增加路由/标签时统计实际活跃时序与 RSS；可根据真实 SLO 调整 bucket，但须同步面板和验证。
- P50/P95/P99 使用 histogram_quantile 桶内估算，不能要求与旧 Collection 的精确样本分位数完全相等。确定事件数量、结果分类、sum 单位应精确对账。
- 没有全量内存样本、定期历史数组或 Graphite tick goroutine；延迟历史由 Prometheus 保存。

## Trace 范围

HTTP server span 使用匹配后的模板路径，向 leader 代理时注入 W3C traceparent。HTTP discovery 与后台 discovery 的 topology adapter、backend 读取和动态查询传递 context；GORM logger 为调用上下文创建 SQL child span。后台任务使用自身生命周期。

已注册 recovery attempt、主要 recovery execute 和 hook 具有 spans；异步 hook 保留 parent 因果关系但不继承调用结束取消。恢复和拓扑操作原有错误/重试/命令语义保持不变。本卡不承诺将所有历史无 context 的 DAO 或拓扑命令全部重写，未贯通调用产生独立 span。

span 不记录原始 SQL、参数、命令、实例地址或错误全文；错误只标记类别/状态。HTTP 完成日志附加 trace_id/span_id。Prometheus exemplar 可跳转 Tempo；没有采样时不会产生 exemplar。

## Grafana 与告警

可导入文件：[orchestrator-grafana.json](../resources/metrics/orchestrator-grafana.json)。配套启动、认证和配置见 [示例说明](../resources/metrics/README.md)。

大盘含 38 个面板，覆盖总览、发现、backend/缓冲、恢复、HTTP、节点/Raft。变量为数据源、部署集群、节点、队列、恢复类型。Raft 面板用 enabled 标签状态过滤；零流量比例显示无数据，抓取失败显示 0，不能统一补零显示健康。

告警阈值为可执行示例，不是已经批准的生产 SLO。发现错误比例示例为至少 20 次探测且 5 分钟失败率 >10%，持续 5 分钟。恢复失败、flush 失败、无 leader、抓取失败等规则见 [alerts.yml](../resources/metrics/alerts.yml)。发送到外部通知渠道的 Alertmanager 配置由部署环境提供，示例不发送通知。

## 升级、删除与回滚

删除的接口：`/debug/metrics`；`/api/discovery-metrics-{raw,aggregated}/:seconds`；`/api/discovery-queue-metrics-{raw,aggregated}/:seconds` 与 `/:queue/:seconds`；`/api/backend-query-metrics-{raw,aggregated}/:seconds`；`/api/write-buffer-metrics-{raw,aggregated}/:seconds`。返回 404，不保留临时重定向或空 JSON。

升级前从配置中删除 `GraphiteAddr`、`GraphitePath`、`GraphiteConvertHostnameDotsToUnderscores`、`GraphitePollSeconds`、`DiscoveryCollectionRetentionSeconds`、`DiscoveryQueueMaxStatisticsSize`；即使空值/不同大小写也拒绝启动或 reload。其他未知字段的兼容行为不改变。

旧库、Graphite exporter、Collection、专用聚合算法及队列采样循环退出。HashiCorp Raft/Consul 仍依赖的 metrics 模块保留，不视为旧业务采集栈残留。

本卡不修改业务 schema、Raft FSM、日志或快照格式。回滚使用上一版二进制和匹配旧配置；已有 Graphite 历史不会自动转换成 Prometheus 时序。Raft 其他卡片的升级限制仍须单独遵守。

## 验证入口

- `make test-unit`：模块级单元/fixture；覆盖指标结果、单位、上限、健康陈旧失效、旧接口/配置删除、gzip、队列与 semaphore 释放。
- `go test -race -mod=readonly ./internal/observability ./internal/discovery ./internal/http ./internal/inst ./internal/app ./internal/config`：本次并发边界。
- `make test-observability`：Prometheus 告警语法与触发/恢复 fixture，需要现有 promtool。
- `make cve`：仓库固定工具版本的漏洞检查。
- `go test -mod=readonly ./internal/observability -run='^$' -bench=BenchmarkTelemetry -benchmem`：真实 SDK 启用下的记录和抓取微基准；不代表生产负载或旧版对照。
- 本地集成必须真正抓取 `/metrics`、导入 Grafana JSON、执行所有 PromQL、检查认证/URLPrefix/节点本地行为；HTTP 200 或 JSON 可解析本身不代表采集成功。
- OTLP fixture 与真实 Collector/Tempo 分开记录；SQLite fixture 与真实 MySQL/多节点 Raft/生产业务 E2E 分开记录。未运行项明确写 Not Run。
