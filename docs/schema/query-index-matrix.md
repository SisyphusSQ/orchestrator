# 查询与索引矩阵

本页记录 [`mysql.sql`](mysql.sql) 中全部二级索引的契约目的。索引集合来自现有 DAO 的过滤、排序、去重和历史迁移约束；本次规范化只统一名称，不擅自删除访问路径。没有目标环境数据分布和 `EXPLAIN` 证据时，不把“可能冗余”直接等同于“可以删除”。

| 表 | 索引 | 访问或约束目的 |
| --- | --- | --- |
| `database_instance` | `idx_database_instance_cluster`、`idx_database_instance_last_checked`、`idx_database_instance_last_seen`、`idx_database_instance_master`、`idx_database_instance_suggested_alias` | 按集群/建议别名列举实例、清理过期发现状态、按上游定位副本 |
| `database_instance_maintenance` | `unq_instance_maintenance_active_host_port`、`idx_instance_maintenance_active_begin`、`idx_instance_maintenance_active_end` | 保证同一实例活动维护窗口唯一，并按开始/结束时间扫描活动窗口 |
| `database_instance_long_running_queries` | `idx_long_query_started_at` | 兼容保留的长会话时间清理与排序 |
| `audit` | `idx_audit_timestamp`、`idx_audit_host_port_timestamp` | 审计保留期清理，以及实例维度时间查询 |
| `host_agent` | `idx_host_agent_token`、`idx_host_agent_last_submitted`、`idx_host_agent_last_checked`、`idx_host_agent_last_seen` | Agent 身份定位及提交、检查、在线时间扫描 |
| `agent_seed` | `idx_agent_seed_target_complete`、`idx_agent_seed_source_complete`、`idx_agent_seed_started_at`、`idx_agent_seed_complete_started`、`idx_agent_seed_success_started` | 按源/目标查未完成任务，并按开始时间、完成状态和成功状态管理任务 |
| `agent_seed_state` | `idx_agent_seed_state_seed_time` | 按种子任务和时间读取状态流水 |
| `host_attributes` | `idx_host_attributes_name`、`idx_host_attributes_value`、`idx_host_attributes_submitted_at`、`idx_host_attributes_expires_at` | 属性名/值检索以及提交时间、过期时间清理 |
| `hostname_resolve` | `idx_hostname_resolve_resolved_at` | 清理过期解析缓存 |
| `cluster_alias` | `unq_cluster_alias_alias`、`idx_cluster_alias_registered_at` | 保证别名唯一并清理过期注册 |
| `node_health` | `idx_node_health_last_seen_active` | 识别过期节点健康记录 |
| `topology_recovery` | `unq_topology_recovery_active_instance`、`idx_topology_recovery_active_started`、`idx_topology_recovery_started_at`、`idx_topology_recovery_cluster_active`、`idx_topology_recovery_ended_at`、`idx_topology_recovery_acknowledged_at`、`idx_topology_recovery_detection`、`idx_topology_recovery_uid` | 防止同实例活动恢复冲突，并覆盖活动期、集群、结束、确认、检测关联和恢复 UID 查询 |
| `hostname_unresolve` | `idx_hostname_unresolve_original`、`idx_hostname_unresolve_registered_at` | 反向主机名定位和过期映射清理 |
| `database_instance_pool` | `idx_database_instance_pool_name` | 按资源池列举实例 |
| `database_instance_topology_history` | `idx_topology_history_snapshot_cluster` | 按快照时间和集群读取拓扑历史 |
| `candidate_database_instance` | `idx_candidate_instance_suggested_at` | 清理过期候选建议 |
| `database_instance_downtime` | `idx_instance_downtime_end` | 按结束时间清理或关闭停机窗口 |
| `topology_failure_detection` | `unq_failure_detection_active_instance`、`idx_failure_detection_active_started` | 保证可执行故障检测窗口唯一，并扫描活动检测 |
| `hostname_resolve_history` | `idx_hostname_resolve_history_source`、`idx_hostname_resolve_history_at` | 按原主机名查映射并清理历史 |
| `hostname_unresolve_history` | `idx_hostname_unresolve_history_host`、`idx_hostname_unresolve_history_at` | 按规范主机名查反向映射并清理历史 |
| `cluster_domain_name` | `idx_cluster_domain_name_domain`、`idx_cluster_domain_name_registered_at` | 按域名定位集群并清理过期注册 |
| `master_position_equivalence` | `unq_master_position_equivalence_pair`、`idx_master_position_equivalence_second`、`idx_master_position_equivalence_suggested` | 保证位点对唯一、反向查第二实例位点并清理过期关系 |
| `async_request` | `idx_async_request_begin`、`idx_async_request_end` | 兼容保留的异步请求开始/结束时间查询 |
| `blocked_topology_recovery` | `idx_blocked_recovery_cluster_at`、`idx_blocked_recovery_at` | 按集群或全局时间读取和清理阻塞记录 |
| `database_instance_last_analysis` | `idx_instance_last_analysis_at` | 清理过期最近分析结果 |
| `database_instance_analysis_changelog` | `idx_instance_analysis_log_at`、`idx_instance_analysis_log_host_port_at` | 按时间清理分析流水并读取实例分析历史 |
| `node_health_history` | `unq_node_health_history_host_token`、`idx_node_health_history_first_seen` | 保证节点进程历史唯一并按首次活跃时间清理 |
| `database_instance_coordinates_history` | `idx_instance_coordinates_host_port_at`、`idx_instance_coordinates_recorded_at` | 读取实例位点时间序列并按记录时间清理 |
| `database_instance_binlog_files_history` | `unq_instance_binlog_files_host_port_file`、`idx_instance_binlog_files_last_seen` | 兼容保留的文件唯一性和最近可见时间清理 |
| `access_token` | `unq_access_token_public_token`、`idx_access_token_generated_at` | 保证公开令牌唯一并清理过期令牌 |
| `database_instance_recent_relaylog_history` | `idx_recent_relaylog_current_seen` | 兼容保留的最近 relay log 时间扫描 |
| `topology_recovery_steps` | `idx_topology_recovery_steps_uid` | 按恢复 UID 读取步骤流水 |
| `raft_store` | `idx_raft_store_key` | 兼容保留的旧版 SQL Raft 键查找 |
| `raft_snapshot` | `unq_raft_snapshot_name` | 兼容保留的旧版 SQL Raft 快照名称唯一性 |
| `database_instance_tags` | `idx_database_instance_tags_name` | 按标签名筛选数据库实例 |
| `database_instance_stale_binlog_coordinates` | `idx_stale_binlog_coordinates_first_seen` | 按首次停滞时间清理位点记录 |

主键不在上表重复列出。所有主键和唯一索引的 `NULL` 语义保持现有契约；对存量库也不会在本次升级中重建或改名。
