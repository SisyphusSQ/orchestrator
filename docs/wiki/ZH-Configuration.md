# 配置

[English](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Configuration) · **中文** · [Wiki 首页](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

服务端可从显式 `--config` 路径读取 JSON 或 YAML，解析格式不依赖扩展名。未指定时依次检查 `/etc/orchestrator.conf`、`conf/orchestrator.conf` 和 `orchestrator.conf`，每个位置允许一个 `.yaml`、`.yml` 或 `.json` 文件；后加载的位置覆盖先加载的位置。同一位置存在多种格式会直接报错。未知字段、重复键、多份 YAML document 及尾随内容都会被拒绝；`dump-config` 仍输出 JSON。配置内含数据库与认证凭据，不应进入公开产物，并应限制文件访问权限。

## 必需的节点身份

每次启动服务端都需要：

```yaml
RaftNodeID: node-1
RaftDataDir: /var/lib/orchestrator/raft
RaftBind: 10.0.0.1:10008
RaftAdvertise: 10.0.0.1:10008
ListenAddress: ":3000"
```

`RaftNodeID` 是持久身份，不是网络地址。`RaftAdvertise` 默认使用规范化后的 `RaftBind`；位于 NAT 后时应显式设置。节点身份、Raft 路径与地址、HTTP 监听、已打开的数据库连接池以及 OpenTelemetry exporter 配置发生变化时都需要重启。

## 元数据库

每个 Raft 节点独立选择一个后端：

- MySQL：配置 `MySQLOrchestratorHost`、端口、数据库、账号和密码，或使用凭据文件。
- SQLite：将 `BackendDB` 设为 `sqlite`，并提供绝对且可写的 `SQLite3DataFile`。

不要让多个 Raft 节点共用同一个元数据库。元数据库和 Raft 数据目录有不同的一致性边界，应分别制定备份方案。

## 拓扑访问与策略

每个节点都要配置 `MySQLTopologyUser` 及密码或凭据文件。该账号必须能读取所有被发现实例的复制状态；执行拓扑变更时还需要对应权限。发现种子、主机名解析、实例过滤、提升规则、恢复过滤、hooks、审计输出、Consul、认证、TLS 和 URL 前缀均属于独立策略，需要按环境决定。

仓库在 [`conf/`](https://github.com/SisyphusSQ/orchestrator/tree/main/conf) 提供 MySQL 与 SQLite 示例。完整字段定义与校验逻辑以 [`internal/config/config.go`](https://github.com/SisyphusSQ/orchestrator/blob/main/internal/config/config.go) 为准。

## 元数据库生命周期与 Schema

兼容的 MySQL 协议后端包括 MySQL 5.7–8.0、TiDB 和 OceanBase MySQL 模式。凭据可直接配置，也可通过 `MySQLOrchestratorCredentialsConfigFile` 提供；两类文件都需要限制访问，因为环境变量展开后密钥仍会进入进程内存。`MySQLOrchestratorMaxAllowedPacket` 与 `MySQLTopologyMaxAllowedPacket` 分别作用于元数据库和被管理实例连接。

进程持有一个元数据库连接池，并将拓扑发现与拓扑操作连接池分开。endpoint、凭据、TLS、超时、packet 限制、连接寿命或池大小变化后必须重启；reload 不会重建已打开的连接池。SQLite 使用一个进程级连接池，并要求绝对且可写的数据文件路径。

空元数据库由可执行的 [`docs/schema/mysql.sql`](https://github.com/SisyphusSQ/orchestrator/blob/main/docs/schema/mysql.sql) 契约初始化；存量库继续走有序兼容补丁链。GORM 复用进程级连接池，不拥有 Schema 迁移。替换二进制或导入 DDL 前先阅读 [`迁移指南`](https://github.com/SisyphusSQ/orchestrator/blob/main/docs/schema/migration-guide.md)。

## 发现、分类与过滤

- 发现周期、并发、实例过期、主机名解析和 seed 决定拓扑状态的新鲜度。启用恢复前应稳定 DNS 与 `report_host` 行为。
- `DiscoveryIgnoreReplicaHostnameFilters` 与 `DiscoveryIgnoreMasterHostnameFilters` 会从特定发现路径排除匹配实例。过滤器属于策略而不是连通性诊断，需要用代表性主机名验证。
- 集群别名、域名、机房、区域、环境标签、提升规则、延迟阈值和半同步状态都会影响候选分类，依赖自动恢复前应保持一致。
- 拓扑兼容时优先使用 GTID。Pseudo-GTID 需要在相关可写主库上明确配置注入、保留时间与权限；缺少标记会降低调整和恢复能力。

## 恢复、hooks、KV 与日志

恢复由全局开关、集群过滤、忽略主机过滤、候选资格和 Raft Leader/多数派状态共同决定。hooks 必须限制运行时间并显式处理失败。maintenance、downtime、audit 和 recovery 记录需要明确保留策略。

Consul KV 继续通过官方 SDK 支持。必须配置 `ConsulAddress`；HTTPS 默认校验证书，并可配置 CA、server name 和成对客户端证书。`ConsulTLSSkipVerify` 只用于限时兼容。跨机房写入超时或部分成功时不会自动重试或回滚。内建 ZooKeeper 发布已删除。

应用日志以 `time<TAB>[LEVEL]<TAB>[caller]<TAB>message` 文本格式写入 stderr。`EnableSyslog` 和 `AuditToSyslog` 初始化失败会阻止启动；审计文件/syslog 写入失败保持可见。日志解析器和 sink 延迟必须在真实服务沙箱中验证。

## 已移除配置

配置解析现在是严格模式：所有无法识别的字段都会被拒绝，包括已移除配置。请分别用 `ReplicationLagQuery`、`RecoveryPeriodBlockSeconds`、`DetachLostReplicasAfterMasterFailover`、`MasterFailoverDetachReplicaMasterHost` 和 `PostponeReplicaRecoveryOnLagMinutes` 替换 `SlaveLagQuery`、`RecoveryPeriodBlockMinutes`、`DetachLostSlavesAfterMasterFailover`、`MasterFailoverDetachSlaveMasterHost` 和 `PostponeSlaveRecoveryOnLagMinutes`。

`OAuthClientId`、`OAuthClientSecret`、`OAuthScopes`、`ExpectFailureAnalysisConcensus`、`SeedAcceptableBytesDiff` 和 `MasterFailoverLostInstancesDowntimeMinutes` 没有替代项，应直接删除。OAuth 认证已不受支持；`AuthenticationMethod` 只接受 `basic`、`multi`、`proxy`、`token` 或空值。`RaftEnabled`、`ZkAddress`、Graphite 配置、旧内存指标保留配置，以及当前 `Configuration` 定义中不存在的其他字段也必须删除。
