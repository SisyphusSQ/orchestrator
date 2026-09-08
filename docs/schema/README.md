# 元数据库 Schema

本目录定义 orchestrator 元数据库的可执行目标结构。`mysql.sql` 既是供评审和测试使用的 DDL，也是空库初始化时由 Go 运行时嵌入并执行的权威源，不是从代码复制出来的示例。

## 设计目标

- MySQL 5.7–8.0、TiDB 和 OceanBase MySQL 模式共用同一份保守 DDL。
- 所有字符列统一使用 `utf8mb4`。表默认及业务文本使用 `utf8mb4_general_ci`；主机名、UUID、GTID、binlog 文件名和令牌等协议标识显式使用 `utf8mb4_bin`，保持逐字节比较且不引入 ASCII 字符集或排序规则。
- 保留运行时代码、Raft 数据快照和旧版本可能依赖的表名与字段名，包括历史拼写 `processcing_node_token`。
- 使用 `idx_` / `unq_` 命名二级索引，并通过[索引矩阵](query-index-matrix.md)记录现有访问目的。
- 不使用 MySQL 8.0 专属排序规则、函数索引、降序索引、生成列或数据库 `ENUM`。
- SQLite 继续由运行时把同一组语句转换为 SQLite 方言，测试会校验表集合和重复初始化。

## 文件职责

| 文件 | 职责 |
| --- | --- |
| [`mysql.sql`](mysql.sql) | 47 张表、字段注释、主键与 75 个二级索引的完整可执行结构 |
| [`schema.go`](schema.go) | 嵌入 SQL、拆分语句并暴露受管表清单 |
| [`compatibility.md`](compatibility.md) | 引擎兼容范围、共同语法和验证证据边界 |
| [`migration-guide.md`](migration-guide.md) | 空库、存量库、滚动升级和回退规则 |
| [`query-index-matrix.md`](query-index-matrix.md) | 二级索引与查询/唯一性目的映射 |

## 运行时选择

```text
无受管表
  -> 执行 mysql.sql
  -> 先写入 canonical-v1-pending
  -> 全部 DDL 成功后写入 canonical-v1 并清除 pending

已有表且没有 canonical pending/完成标记
  -> 执行 generateSQLBase + generateSQLPatches
  -> 写入 legacy-v1

已有 canonical-v1-pending 且没有 canonical-v1
  -> 幂等续跑 mysql.sql

已有 canonical-v1
  -> 跳过历史补丁
```

`orchestrator_schema_migrations` 记录结构路线；`orchestrator_db_deployments` 继续记录运行过初始化的应用版本。两者职责不同，不能互相替代。

## 表清单

以下 39 张表仍属于运行时数据契约：

- 实例与拓扑：`database_instance`、`database_instance_maintenance`、`database_instance_topology_history`、`database_instance_coordinates_history`、`database_instance_downtime`、`database_instance_last_analysis`、`database_instance_analysis_changelog`、`database_instance_peer_analysis`、`database_instance_pool`、`database_instance_stale_binlog_coordinates`、`database_instance_tags`、`database_instance_tls`、`candidate_database_instance`、`master_position_equivalence`。
- 故障恢复：`topology_failure_detection`、`topology_recovery`、`topology_recovery_steps`、`blocked_topology_recovery`、`global_recovery_disable`。
- 主机与集群元数据：`hostname_resolve`、`hostname_resolve_history`、`hostname_unresolve`、`hostname_unresolve_history`、`hostname_ips`、`host_attributes`、`cluster_alias`、`cluster_alias_override`、`cluster_domain_name`、`cluster_injected_pseudo_gtid`。
- Agent、节点与公共能力：`host_agent`、`agent_seed`、`agent_seed_state`、`node_health`、`node_health_history`、`audit`、`access_token`、`kv_store`。
- 生命周期：`orchestrator_schema_migrations`、`orchestrator_db_deployments`。

以下 8 张表在当前 Go 源码中没有运行时 SQL 引用，但为旧二进制、已有数据和跨版本 Raft 快照兼容而保留：`database_instance_long_running_queries`、`database_instance_binlog_files_history`、`database_instance_recent_relaylog_history`、`async_request`、`orchestrator_metadata`、`raft_store`、`raft_log`、`raft_snapshot`。本次不自动删除、重命名或迁移这些表；未来清理必须先验证真实数据和外部消费者。

## 验证入口

```bash
go test ./docs/schema ./internal/db
make test-docs
```

针对空的隔离 MySQL 兼容数据库，可显式提供测试 DSN：

```bash
ORCHESTRATOR_METADATA_SCHEMA_TEST_DSN='user:password@tcp(127.0.0.1:3306)/empty_schema?interpolateParams=true' \
  go test ./internal/db -run '^TestCanonicalMetadataSchemaExternalMySQL$' -count=1 -v
```

测试在写入前要求当前数据库一张表也没有；不要把生产库、共享库或含数据的库作为该入口目标。
