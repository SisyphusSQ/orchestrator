# 元数据库 Schema

本目录定义 orchestrator 元数据库的可执行目标结构。`mysql.sql` 是空库初始化时由 Go 运行时嵌入执行的权威 DDL，不是示例。

## 设计目标

- 全部 **50 张表**均使用 `id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT` 和单列 `PRIMARY KEY (id)`；不再使用联合主键或业务字段主键。
- 33 张原业务主键表保留原列顺序、非空和唯一性约束，转为 `unq_<table>_identity` 唯一索引。联合唯一索引继续防止同一业务身份出现重复记录。
- 17 张原自增主键表统一列名为 `id`，迁移保留原编号值。`last_detection_id`、`blocking_recovery_id`、`agent_seed_state.agent_seed_id` 等关联字段及 API 业务标识保持原语义。
- MySQL 5.7–8.0、TiDB 和 OceanBase MySQL 模式共用保守 DDL；SQLite 由同一份语句转换。
- 字符列统一 `utf8mb4`，表默认和业务文本使用 `utf8mb4_general_ci`；协议标识使用 `utf8mb4_bin`。不改变历史拼写 `processcing_node_token`。
- 保留现有二级索引访问路径；新库共 108 个二级索引，详见[索引矩阵](query-index-matrix.md)。不使用外键、分区、生成列或 MySQL 8.0 专属语法。

## 文件职责

| 文件 | 职责 |
| --- | --- |
| [mysql.sql](mysql.sql) | 当前 canonical-v2 的完整目标结构 |
| [schema.go](schema.go) | SQL 嵌入、版本标记、受管表及历史编号/业务唯一键映射 |
| [migrations/mysql-v1.sql](migrations/mysql-v1.sql) | 冻结的旧版 DDL，仅用于续跑旧版未完成初始化及迁移验证；禁止用于新库 |
| [migration-guide.md](migration-guide.md) | 停写、备份、显式迁移、续跑和回退 |
| [compatibility.md](compatibility.md) | 引擎边界及验证入口 |
| [query-index-matrix.md](query-index-matrix.md) | 二级索引与业务唯一性契约 |

## 初始化与升级

```text
空库 / canonical-v2-pending
  -> 幂等执行 mysql.sql
  -> 回读全部表的 id 主键及业务唯一约束
  -> 写 canonical-v2，清除 pending

canonical-v2
  -> 检查表集合和主键/业务唯一约束
  -> 不执行历史补丁

legacy-v1 / canonical-v1 / canonical-v1-pending / auto-id-v2-pending
  -> 普通启动拒绝，提示显式迁移命令
  -> 停写并备份后运行 admin migrate-metadata-id
  -> 逐表迁移及回读
  -> 全部完成才写 canonical-v2，清除 pending
```

`orchestrator_schema_migrations` 记录结构版本和迁移进度；`orchestrator_db_deployments` 记录应用版本，不能用应用版本代替结构校验。`metadata.schema.skipUpdate` 仍表示由运维外部管理结构；使用时必须先保证库已经符合 v2，不能用它绕过升级后继续操作旧结构。

## 表清单

以下 42 张表属于运行时契约：

- 实例与拓扑：`database_instance`、`database_instance_maintenance`、`database_instance_topology_history`、`database_instance_coordinates_history`、`database_instance_downtime`、`database_instance_last_analysis`、`database_instance_analysis_changelog`、`database_instance_peer_analysis`、`database_instance_pool`、`database_instance_stale_binlog_coordinates`、`database_instance_tags`、`database_instance_tls`、`candidate_database_instance`、`master_position_equivalence`。
- 故障恢复：`topology_failure_detection`、`topology_recovery`、`topology_recovery_steps`、`blocked_topology_recovery`、`global_recovery_disable`、`recovery_policy`、`recovery_hook_profile`、`recovery_hook_assignment`。
- 主机与集群：`hostname_resolve`、`hostname_resolve_history`、`hostname_unresolve`、`hostname_unresolve_history`、`hostname_ips`、`host_attributes`、`cluster_alias`、`cluster_alias_override`、`cluster_domain_name`、`cluster_injected_pseudo_gtid`。
- Agent、节点与公共能力：`host_agent`、`agent_seed`、`agent_seed_state`、`node_health`、`node_health_history`、`audit`、`access_token`、`kv_store`。
- 生命周期：`orchestrator_schema_migrations`、`orchestrator_db_deployments`。

以下 8 张表为历史兼容保留，本次同样统一主键，不删除表或数据：`database_instance_long_running_queries`、`database_instance_binlog_files_history`、`database_instance_recent_relaylog_history`、`async_request`、`orchestrator_metadata`、`raft_store`、`raft_log`、`raft_snapshot`。其中 `raft_log.log_index` 在新结构中改名为 `id`，保留原值；旧 SQL Raft 外部消费者需要同时升级。

## 验证入口

```bash
go test ./docs/schema ./internal/db
make test-unit
make test-docs
```

真实数据库入口和证据边界见[兼容矩阵](compatibility.md)。全部迁移测试使用隔离库，不能指定生产或共享数据库。
