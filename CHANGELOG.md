# ChangeLog

## Unreleased

- raft
  - 将 Raft 收敛为唯一服务端运行模式，保留单节点与多节点部署，以及各节点独立的 MySQL/SQLite 元数据库；移除共享数据库选主、`RaftEnabled`、`continuous`、`--grab-election` 和旧选主 API。升级配置与新集群 bootstrap 步骤见[升级指南](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Upgrading)。
  - 让 Raft 生命周期独立于自动发现，关闭 discovery 后仍支持成员管理、状态复制和领导权转移；补齐运行时关闭、配置重载、就绪状态和 Web 操作入口。
  - Replace the 2017 openark Raft fork with official `github.com/hashicorp/raft` v1.7.3 and `github.com/hashicorp/raft-boltdb/v2` v2.3.1 for newly created clusters.
  - Require a durable `RaftNodeID` independent of bind/advertise/DNS, bootstrap a single seed voter, and manage membership through ID-aware HTTP APIs (`/api/raft/configuration`, `/bootstrap`, `/members`, `/leadership/transfer`, `/snapshot`).
  - Use official FileSnapshotStore plus one Bolt store for logs and stable state, remove Yield/peer/health-report control paths, and report readiness from VerifyLeader, configuration suffrage, and last-contact.
- optimization
  - 将全部 50 张元数据库表统一为自增 `id` 单列主键，保留业务唯一约束及旧编号值；存量库须停写、备份后执行 `orchestrator admin migrate-metadata-id`，普通启动不自动改表，详见 [Schema 迁移指南](docs/schema/migration-guide.md)。
  - 将服务端配置与运行时模型统一重构为按职责分组的 lowerCamel 分层结构，`dump-config` 同步输出分层 JSON；旧平铺字段不再兼容，部署前需按[升级指南](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Upgrading)同步迁移配置文件、脚本和生成模板。
  - 将当前项目文档收敛到双语 GitHub Wiki，以 `docs/wiki/` 作为版本化来源；删除重复的历史 Markdown 和孤立图片，仅保留 Wiki 源文件、元数据 schema 与专项验证记录。
  - Consolidate the executable DDL for 47 metadata tables in `docs/schema/mysql.sql`, with `utf8mb4`, Chinese comments, and `idx_` / `unq_` index names. New databases use syntax shared by MySQL 5.7–8.0, TiDB, and OceanBase MySQL mode; existing databases retain the historical patch stream with an explicit migration marker, and SQLite initialization uses the same source.
  - Raise the server and independent client build baseline to Go 1.27.0, align container build images, and adopt current standard-library idioms without changing third-party module versions.
  - Split the Go HTTP client into the independent `tools/orch-cli` module and `orch` binary. Remove the Shell client and direct business CLI without a compatibility layer; server startup is now `orchestrator server`, with local maintenance under `admin`. Add missing diagnostic APIs, explicit write-result uncertainty, independent builds and command/API coverage. See the [orch CLI](https://github.com/SisyphusSQ/orchestrator/wiki/EN-orch-CLI) and [upgrade guide](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Upgrading).
  - Move the executable to `cmd/orchestrator` and application packages to `internal`, merging the local golib implementation into the root Go module. Build the command package with `make build` and test all packages with `make test-unit`; the former `go/*` import paths are no longer supported. Runtime configuration and resource paths remain unchanged.
  - Replace Graphite, rcrowley/go-metrics and raw/aggregated Collection APIs with OpenTelemetry metrics and a node-local Prometheus endpoint; reject removed telemetry configuration keys and document the breaking upgrade.
  - Add bounded OTLP tracing, layered local health checks, and an importable Prometheus Grafana dashboard with collection, alert and Collector/Tempo examples.
  - Correct discovery queue/dead-instance gauges and backend wait/flush timing semantics while retaining discovery and recovery decisions.
  - Replace Martini and its auth, gzip, and render extensions with Gin v1.12.0 behind a project-owned HTTP transport adapter, preserving 306 API, Web, debug, and agent route contracts plus authentication, templates, static assets, Raft proxy termination, URL prefixes, and HTTP/HTTPS/Unix listener behavior.
  - Migrate stable orchestrator backend DAO reads and writes to GORM v1.31.2 with MySQL and SQLite drivers, while reusing the process-owned pool, retaining ordered SQL schema migrations, and keeping topology, snapshot, Raft, and `LastInsertId` paths explicitly scoped.
  - Remove the local `sqlutils` package and replace its remaining dynamic topology and snapshot helpers with context-aware, null-preserving adapters.
  - Build MySQL backend and topology connections from typed driver configuration, move pool ownership and shutdown into the process database runtime, separate discovery and topology-operation pools, and apply `MySQLTopologyMaxAllowedPacket` to the correct connections.
  - Replace the local application logger implementation with Uber Zap v1.28.0 while preserving the compatibility API, stderr routing, legacy syslog priorities, and explicit logger shutdown. The default text format is now `time\t[LEVEL]\t[caller]\tmessage`; deployments that parse logs must update their patterns before upgrading.
  - Make configured application and audit syslog initialization failures fatal during startup, and make audit file/syslog write failures observable to callers instead of discarding them in per-entry goroutines.
  - Return database initialization, TLS setup, hostname resolution, and configuration parsing failures to process entry points instead of terminating from library packages. Failed configuration reloads leave the active configuration unchanged and return an API error or SIGHUP log entry.
  - Propagate HTTP listener, continuous discovery, and Raft monitor failures to the process entry point. Raft retains only the first pending fatal runtime error and no longer creates a goroutine for each report.
  - Replace `github.com/armon/consul-api` with a single official `github.com/hashicorp/consul/api` v1.34.3 client for ordinary KV and `consul-txn`, defaulting Consul TLS to certificate verification, sending ACL tokens only as `X-Consul-Token`, and failing CLI/continuous startup on Consul client or TLS file errors.
