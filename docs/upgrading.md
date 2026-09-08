# Upgrading orchestrator

## 元数据库 Schema 规范化（TOO-429）

全新的空元数据库改由 [`docs/schema/mysql.sql`](schema/mysql.sql) 初始化，使用 MySQL 5.7–8.0、TiDB 与 OceanBase MySQL 模式的共同语法子集，并由同一权威源转换 SQLite 结构。新库写入 `canonical-v1`；已有库继续原有基础 DDL 与历史补丁链，完成后写入 `legacy-v1`。本次不会自动重建存量大表、转换字符集、重命名索引或删除兼容表。

升级前备份每个 Raft 节点的独立元数据库，并在相同产品和精确版本的隔离空库运行 Schema 测试。升级后回读 `orchestrator_schema_migrations`、`orchestrator_db_deployments`、受管表数和关键业务读写。不要把 `mysql.sql` 直接导入非空库，也不要把测试 DSN 指向生产或共享数据库。

存量库回退只需停止新进程并恢复旧二进制与配置。对新建的 `canonical-v1` 库回退时，旧二进制不理解新标记，可能重新执行历史补丁并增加旧索引名；应恢复升级前备份，或在确认结构完整后使用 `SkipOrchestratorDatabaseUpdate`。完整步骤、兼容保留表和 DDL 风险见[迁移指南](schema/migration-guide.md)。

## 仅支持 Raft（TOO-428）

服务端移除非 Raft 单机、共享数据库选主及半高可用路径。MySQL 和 SQLite 仍可作为每个节点的独立元数据后端。单节点开发也使用 Raft，并显式 bootstrap。

- 删除所有配置层中的 `RaftEnabled`，无论原值为 true、false 或 null，新版本都会拒绝；配置稳定、唯一的 `RaftNodeID`、`RaftDataDir`、`RaftBind` / `RaftAdvertise`。这些参数的含义见 [Raft 配置](configuration-raft.md)。
- 服务统一使用 `orchestrator server`。删除 `continuous`、`--grab-election` 调用；旧 `/api/grab-election`、`/api/reelect` 已删除，领导权转移使用 `orch raft-transfer-leadership` 或 `POST /api/raft/leadership/transfer`。
- `server --discovery=false` 只关闭自动发现，仍启动 Raft。暂停发现的测试也必须 bootstrap 并等待 Leader；未就绪不再允许业务写入。
- 健康返回移除 `raftEnabled`，指标移除 `orchestrator_raft_enabled`。更新 Grafana 面板与自定义规则，直接读取 Raft 就绪、领导角色和日志进度。
- 新库不再创建 `active_node`，对应历史建表和补丁引用已一并移除。已有库中的旧表不自动删除，避免升级时销毁数据。

已有 Raft 集群保留节点身份、Raft 日志/快照和独立元数据库，修改配置与启动入口后重启；不重复 bootstrap。本变更不引入 Raft 日志或快照格式变化。回滚时同时恢复原二进制、原配置与启动脚本。

现存非 Raft 部署不能直接改配置后继续共用数据库。先停止旧系统的发现、自动恢复和业务写入，备份需要保留的数据，另行准备独立元数据库与节点身份，建立新 Raft 集群后验收再切换客户端。本变更不自动复制、删除或迁移现存环境数据；不得让新旧系统同时执行故障恢复。

## 独立 Go HTTP 客户端（TOO-426）

一次性移除旧 Shell 客户端、直连业务 CLI、`-c`/`cli`、旧参数别名和环境变量。安装 `orch` 并使用 `ORCH_ENDPOINT` 等新配置。服务启动从 `http` 改为 `server`；本地维护迁入 `orchestrator admin`。不需要迁移数据库数据；不能仅替换二进制而保留旧启动脚本。回滚需同时恢复上一版本的服务端、客户端、启动配置及脚本。新 `set-general-attribute`、`delete-all-instance-tags` 及标签删除结果契约要求集群节点使用同一新版本，首次使用前完成全节点升级；本卡不承诺旧节点混跑。

详见 [orch](orch.md) 和 [完整能力映射](orch-commands.md)。

Review the breaking changes on this page before replacing an existing `orchestrator` binary. Unreleased changes remain listed here until they are included in a release.

## Unreleased breaking changes


### Go 源码迁移到 cmd 与 internal

源码构建入口由 `go/cmd/orchestrator` 改为 `cmd/orchestrator`，推荐继续使用
`make build`。全部内部包迁入 `internal/`，旧 `github.com/openark/orchestrator/go/*`
导入路径不再提供兼容性；依赖旧包的外部 Go 项目需要调整集成方式。
本地 golib 已合并到根模块，移除针对 `go/golib` 的独立依赖下载和测试步骤，
使用 `make deps`、`make test-unit` 即可覆盖全部包。

服务端二进制名称和配置搜索路径保持不变；Web 资源通过 `make binary` 内嵌，
运行时不再依赖外置 `resources/` 前端目录。本次源码目录调整不新增数据库或
Raft 格式迁移。详见[源码布局](build.md)。

### Observability replaces Graphite and raw metric APIs

Graphite and the in-memory raw/aggregated metrics APIs are removed. Delete the
six obsolete Graphite/Collection configuration fields before upgrading; old
fields are rejected even when empty. Scrape each node's `/metrics` endpoint and
import the Prometheus Grafana dashboard. See [Observability](observability.md)
for the complete removal list, authentication, sampling, restart-only trace
configuration and rollback instructions. This change adds no schema or Raft
format migration.


### HTTP routing now uses Gin behind a compatibility adapter

The standard Web/API listener and the agent listener now use Gin v1.12.0
instead of Martini. No HTTP configuration keys or listener selection rules have
changed. Existing API synonyms, optional trailing slashes, GET-to-HEAD
compatibility, authentication modes, `URLPrefix`, custom `StatusEndpoint`,
template layout, static assets, debug endpoints, and HTTP/HTTPS/Unix socket
selection remain supported through a project-owned transport adapter.

Before rollout, exercise representative read and mutating requests with the
deployment's authentication mode and URL prefix. Deployments using mutual TLS
or Raft should additionally validate certificate OU rejection and follower
proxy behavior. Confirm the Web UI assets and debug endpoints through the same
reverse proxy used in production. A rollback requires only the prior binary and
matching configuration; this migration adds no persistent state or schema
change.

### Backend DAO now uses GORM without schema ownership

Backend DAO queries now use GORM for both MySQL and SQLite, while reusing the
single process-owned backend connection pool. The configured backend, schema,
configuration keys, and on-disk SQLite format are unchanged. Ordered SQL
bootstrap and patch statements remain the only schema migration mechanism;
GORM does not run `AutoMigrate` or other implicit DDL.

Before rollout, validate startup and representative backend reads and writes
against the deployment's configured backend. Pay particular attention to
recovery registration, maintenance registration, and agent seed operations,
whose generated identifiers continue to come from an explicit `database/sql`
adapter. A rollback requires only the prior binary and matching configuration;
this migration adds no database patch of its own.

### Database connections now have an explicit process lifecycle

Backend and topology connections are now created from typed MySQL driver
configuration and owned by the `orchestrator` process. Shutdown closes the
backend pool, the discovery pools, and the topology-operation pools. The
`MySQLTopologyMaxAllowedPacket` setting now applies to topology connections;
previous builds accidentally wrote that value to an unrelated cached backend
DSN, so deployments that set it may observe the intended topology packet limit
for the first time.

Database connection settings reloaded with `SIGHUP` do not rebuild pools that
are already open. Restart every `orchestrator` process after changing database
endpoints, credentials, TLS files or verification, connection timeouts, packet
limits, lifetime, or pool sizing. No configuration keys or JSON value formats
changed in this migration.

### Log output and syslog failure behavior changed

Application logging now uses Uber Zap v1.28.0 and writes text lines to stderr in the form `time\t[LEVEL]\t[caller]\tmessage`. The added caller field and Tab separators replace the previous space-separated format. Console output remains plain text, not JSON, and CLI data on stdout remains separate. Update log parsers, collection rules, and alerts that depend on the previous layout before replacing the binary.

When `EnableSyslog` is `true`, failure to initialize the local syslog writer now stops startup instead of silently continuing with stderr only. The same explicit startup failure applies to `AuditToSyslog`. Confirm syslog access from the actual host, container, or service sandbox before rollout.

Application and audit syslog writes no longer start an unbounded goroutine per entry. They are synchronous, and audit file/syslog errors are returned and logged. Verify local syslog latency under representative load and ensure the service is supervised before enabling these sinks.

### Built-in ZooKeeper KV publishing removed

`orchestrator` no longer includes a ZooKeeper adapter or client dependencies. The internal KV store, Consul KV providers, `submit-masters-to-kv-stores` CLI/API, and Raft `put-key-value` command remain supported.

`ZkAddress` has also been removed from the configuration schema. A configuration file containing that field now causes startup or configuration reload to exit with an error, even when its value is empty. This explicit failure prevents an old configuration from being accepted while ZooKeeper master publishing silently stops.

Before upgrading:

1. Check every configuration file or generated configuration layer for `ZkAddress`, including files loaded during a reload.
2. Migrate master discovery consumers to Consul KV or implement an [external recovery hook](configuration-recovery.md#hooks) that maintains ZooKeeper or another external store.
3. Remove `ZkAddress` from all configuration layers.
4. Validate master discovery after a manual `submit-masters-to-kv-stores` request and after a representative failover in an environment that contains the real external consumers.

This change does not migrate existing ZooKeeper data or consumers. If rollback is required before consumers have migrated, restore a pre-removal binary together with its matching configuration; do not add `ZkAddress` back to the new binary.

### Consul uses the official Go SDK with strict TLS by default

`orchestrator` no longer uses `github.com/armon/consul-api`. Ordinary Consul KV and `consul-txn` now share one official `github.com/hashicorp/consul/api` v1.34.3 client. ACL tokens are sent only as `X-Consul-Token`.

Previous HTTPS clients set `InsecureSkipVerify: true` unconditionally. The new default verifies certificates against the system trust store, or against `ConsulTLSCAFile` / `ConsulTLSCAPath` when configured. `CONSUL_HTTP_SSL_VERIFY` cannot turn verification off. Historical HTTPS deployments that relied on skipped verification must configure a trusted CA and `ConsulTLSServerName` before upgrading. Only a temporary compatibility path should set `"ConsulTLSSkipVerify": true`, which logs one non-sensitive startup warning.

Client construction and TLS file errors now fail server startup instead of logging and continuing with a nil Consul client. `ConsulCrossDataCenterDistribution` requires `ConsulAddress`. Cross-DC updates can still succeed in some datacenters and fail in others; the caller receives an aggregated error and successful datacenters are not rolled back.

Consul settings are not rebuilt on `SIGHUP` configuration reload. Restart every `orchestrator` process after changing Consul address, scheme, token, datacenter, TLS files, skip-verify, timeout, or provider. `ConsulHttpTimeoutSeconds` defaults to `60`; `0` means no overall deadline. Timed-out writes are not retried.

Before upgrading:

1. Confirm every Consul HTTPS endpoint presents a certificate that the process trust store, or an explicit CA setting, will accept.
2. Set `ConsulTLSServerName` when the certificate hostname does not match the configured address.
3. Configure `ConsulTLSCertFile` and `ConsulTLSPrivateKeyFile` together if Consul requires mTLS.
4. Keep `ConsulTLSSkipVerify` false unless a short-lived compatibility window is required.
5. Validate with unit/fixture tests, then an isolated Consul HTTP/HTTPS/ACL/mTLS environment. Existing system tests that only grep CLI output do not prove Consul KV contents.

Rollback restores the previous binary and its matching configuration. The new TLS fields do not change KV data already stored in Consul. Do not add the removed armon client back to the new binary.
