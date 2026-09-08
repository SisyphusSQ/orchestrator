# Orchestrator 监控示例

此目录提供 Prometheus 大盘与告警、OTLP Collector、Tempo、Grafana provisioning。指标与升级契约见 [可观测性文档](../../docs/observability.md)。

## 使用已有 Prometheus / Grafana

1. 将 `prometheus.yml` 的 target 改为每个 orchestrator 节点地址，并设置 `orchestrator_cluster` 标签、正确的 `URLPrefix` 和既有认证/TLS。`/metrics` 不会自动跳过认证。
2. 将 `alerts.yml` 加入 Prometheus rule_files。先调整示例阈值，再连接环境自己的 Alertmanager；本目录不会发送外部通知。
3. 在 Grafana 导入 `orchestrator-grafana.json`，选择 Prometheus 数据源；部署集群、节点、队列、恢复类型支持多选。
4. 可选：配置 Tempo 数据源，在 Prometheus 数据源中映射 exemplar 的 `trace_id`。示例 provisioning 已配置 UID `prometheus` 和 `tempo`。

## 本地 Compose 示例

要求已有可用的 Docker Compose / 兼容容器引擎，以及本机运行在 3000 端口的 orchestrator。示例不启动 orchestrator、不连接生产拓扑。宿主机服务需允许容器网络访问；只有 loopback 监听时，应使用环境支持的转发方式或改为实际可达 target。

从仓库根目录执行：

```sh
docker compose -f resources/metrics/compose.yml up -d
```

启动四个独立容器，Prometheus 访问本机 `host.docker.internal:3000/metrics`；UI/接收端口只绑定本机：

- Grafana：[http://127.0.0.1:13000](http://127.0.0.1:13000)，匿名只读，仅用于本地示例。
- Prometheus：[http://127.0.0.1:19090](http://127.0.0.1:19090)。
- 应用 OTLP trace endpoint：`http://127.0.0.1:14318/v1/traces`。

Trace 链路为应用 → Collector → Tempo，Prometheus 仍直接抓应用。数据是临时示例存储；生产需单独配置容量、持久化、认证和 TLS。Collector/Tempo 容器间明文传输仅用于隔离示例网络。

关闭本例：

```sh
docker compose -f resources/metrics/compose.yml down
```

固定版本：Prometheus 3.11.3、Grafana 13.0.1、Collector 0.148.0、Tempo 2.10.0。Collector 和 Tempo 的发布来源：[Collector v0.148.0](https://github.com/open-telemetry/opentelemetry-collector-releases/releases/tag/v0.148.0)、[Tempo v2.10.0](https://github.com/grafana/tempo/releases/tag/v2.10.0)。不使用 `latest`。

## 验证与无数据排查

本次执行结果、资源测量和未运行项见 [本地验证记录](VALIDATION.md)。

```sh
promtool check rules resources/metrics/alerts.yml
promtool test rules resources/metrics/alerts.test.yml
```

先查 Prometheus Targets：确认 health=up 且 lastError 为空，再查 `orchestrator_ready`。若 response 200 但抓取失败，检查 Content-Type、gzip 与 URLPrefix。大盘当前有 38 个面板 / 43 条查询。

- 没发生恢复、flush 或 discovery 时，相应 histogram 没有样本是正常情况；不得伪造零延迟。
- Raft disabled 时相关面板过滤为无数据，顶部启用节点数为 0。
- `--discovery=false` 时不会启动队列，active/leader-ready 也可能为 0。此模式不适合直接套用无 leader 告警。
- process 指标随运行平台变化；本次 macOS 本地抓取已包含 CPU/RSS，其他平台应以实际 `/metrics` 输出为准。
- Trace 为采样数据，不保证每个请求都有 exemplar。指标总量不受 trace sample ratio 影响。
