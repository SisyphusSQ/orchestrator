# TOO-412 本地验证记录

验证日期：2026-09-08。分支：`suqing/too-412-observability`，基线：`b1604d5753e82abb5f327434bfd331dc026d29ed`。记录对应本次工作区代码，尚未提交、发布或部署生产。

## 已完成

| 验证 | 结果与边界 |
| --- | --- |
| `make test-unit` | 根模块及 `go/golib` 通过；后续边界修复另运行涉及包的单元与 race 测试 |
| race | observability、discovery、HTTP、inst、app、config、logic、db、raft 涉及包通过 |
| 指标契约 fixture | 结果 Counter、秒单位及 histogram sum、Gauge 回落、2100 个属性值的 overflow 保总量、恢复提前返回终态通过 |
| 抓取与认证 | 真实 HTTP 客户端验证 basic auth、URLPrefix、GET/HEAD、旧路由 404；验证 gzip 可被标准客户端解码；业务队列锁被占用时仍可完成 Prometheus 抓取 |
| OTLP HTTP fixture | 解码真实 protobuf 请求，验证上游 W3C → HTTP → SQL 父子关系、敏感文本未进入 span；503 导出失败可观测且不影响 readiness；flush 与重复关闭通过 |
| 配置 | 6 个旧字段即使为空或大小写变化也拒绝；OTLP URL/采样校验及配置重载限制通过 |
| 健康 | 过期快照不再 ready；最终二进制在 SQLite backend 可用、Raft 已配置但未启动时返回 503，`backend=true, raftEnabled=true, ready=false` |
| 构建与依赖 | `make binary`、`make fmt-check`、`make deps`、`git diff --check` 通过 |
| 文档 | `make test-docs` 通过，索引与本地链接一致 |
| 告警 | `make test-observability` 通过：8 条规则语法检查、抓取失败/恢复和 readiness/leader 告警 fixture |
| CVE | `make cve` 返回 WARNING、退出码 0：可达漏洞 0、导入包漏洞 0；模块级 1 条 `GO-2026-5932`，涉及未被本项目调用的 OpenPGP，无修复版本 |

检查中将 `quic-go` 升到 0.59.1、`x/crypto` 升到 0.56.0，修复扫描发现的可修复项；没有将模块级 WARNING 写成全绿。

## Prometheus 与 Grafana 实际联调

- 环境：macOS arm64 / Apple M4 Pro，Go 1.27.0（模块声明 1.26.5），Prometheus 3.11.3，Grafana 13.0.1。
- 独立 SQLite fixture 使用 `--discovery=false`、`RaftEnabled=false`、trace export 关闭；只监听 `127.0.0.1:13006`。临时文件、SQLite、日志和 Prometheus/Grafana 数据均在 `/tmp/too412-runtime`。
- Prometheus 本次验证每秒抓取一次，最终 target 为 `health=up`、`lastError=""`；交付示例配置使用 15 秒抓取。
- Grafana 已通过 provisioning 导入 UID `orchestrator-observability`，实际浏览器显示 38 个面板、6 个分组，在线及服务就绪比例均为 100%。
- 将大盘 43 条 PromQL 中的数据源变量替换为 fixture 的部署集群、节点及时间窗口后，逐条向真实 Prometheus 执行：43 条成功、0 条查询错误，25 条返回时序。18 条空结果来自没有 discovery/flush/recovery 负载、没有业务错误、Raft 未启用等条件；部分比例返回 NaN，不算业务健康证明。
- 最终二进制空闲快照：123 条目标时序、31 个 goroutine、RSS 约 46.55 MiB。这是包含整个应用和 SQLite 的瞬时状态，不是遥测增量内存或生产负载上限。
- 联调发现并修复二次 gzip：Gin 统一负责压缩，Prometheus handler 禁用自身压缩。仅检查 HTTP 200 无法发现此类问题。

## SDK 微基准

命令：

```sh
go test -mod=readonly ./go/observability -run='^$' -bench=BenchmarkTelemetry -benchmem
```

| 操作 | 时间 | 分配 |
| --- | --- | --- |
| `RecordDiscovery`：一次完成 Counter + 三个阶段 Histogram | 811.6 ns/op | 672 B/op，16 allocs/op |
| 低基数 SDK `/metrics` 抓取，含 Go/process collector | 220357 ns/op | 310954 B/op，2003 allocs/op |

真实 SDK 已安装，trace export 关闭；该基准避免使用已关闭 provider 或 no-op provider。没有与旧版同负载对照，也没有测量生产高并发、大量路由或启用全采样的容量，不能据此声称性能提升。

## 未运行

- **Not Run**：Compose 的 Collector → Tempo 完整链路、Tempo exemplar 跳转。配置已提供，本机 Podman 引擎未启动；没有启动 VM 或安装容器运行环境。OTLP fixture 不代替这项证据。
- **Not Run**：真实 MySQL 拓扑故障、成功提升及多节点 Raft 切主的业务 E2E；本次复用了仓库单元/fixture，并完成本地 SQLite 与监控链路验证。
- **Not Run**：生产部署、生产告警阈值验收、旧版同负载对照及长时间稳定性测试。

本轮验证结束后关闭临时 orchestrator、Prometheus、Grafana 进程；可按 [示例说明](README.md) 在自己的环境重新启动。临时日志不是长期构建产物，正式 CI 应归档各自运行结果。
