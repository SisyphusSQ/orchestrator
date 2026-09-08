CREATE TABLE IF NOT EXISTS `orchestrator_schema_migrations` (
  `migration_id` VARCHAR(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '稳定的结构迁移标识',
  `applied_at` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '结构迁移完成时间',
  PRIMARY KEY (`migration_id`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci COMMENT = '元数据库结构迁移记录';

INSERT IGNORE INTO `orchestrator_schema_migrations` (`migration_id`, `applied_at`)
VALUES ('canonical-v1-pending', CURRENT_TIMESTAMP);

CREATE TABLE IF NOT EXISTS `orchestrator_db_deployments` (
  `deployed_version` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '已初始化的 orchestrator 版本',
  `deployed_timestamp` TIMESTAMP NOT NULL DEFAULT '1971-01-01 00:00:00' COMMENT '版本初始化完成时间',
  PRIMARY KEY (`deployed_version`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci COMMENT = '应用版本与元数据库初始化记录';

CREATE TABLE IF NOT EXISTS `database_instance` (
  `hostname` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '数据库实例主机名',
  `port` SMALLINT UNSIGNED NOT NULL COMMENT '数据库实例端口',
  `last_checked` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '最近一次完成检查的时间',
  `last_attempted_check` TIMESTAMP NOT NULL DEFAULT '1971-01-01 00:00:00' COMMENT '最近一次发起检查的时间',
  `last_check_partial_success` TINYINT UNSIGNED NOT NULL COMMENT '最近检查是否仅部分成功',
  `last_seen` TIMESTAMP NULL DEFAULT NULL COMMENT '最近一次确认实例可见的时间',
  `uptime` INT UNSIGNED NOT NULL COMMENT '实例已运行秒数',
  `server_id` INT UNSIGNED NOT NULL COMMENT 'MySQL server_id',
  `server_uuid` VARCHAR(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT 'MySQL server_uuid',
  `version` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '数据库版本字符串',
  `binlog_server` TINYINT UNSIGNED NOT NULL COMMENT '是否为 binlog server',
  `read_only` TINYINT UNSIGNED NOT NULL COMMENT '实例是否启用只读',
  `binlog_format` VARCHAR(16) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT 'binlog 格式',
  `log_bin` TINYINT UNSIGNED NOT NULL COMMENT '是否启用 binlog',
  `log_slave_updates` TINYINT UNSIGNED NOT NULL COMMENT '是否记录复制线程更新',
  `binary_log_file` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '当前 binlog 文件名',
  `binary_log_pos` BIGINT UNSIGNED NOT NULL COMMENT '当前 binlog 位点',
  `master_host` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '上游实例主机名',
  `master_port` SMALLINT UNSIGNED NOT NULL COMMENT '上游实例端口',
  `slave_sql_running` TINYINT UNSIGNED NOT NULL COMMENT '复制 SQL 线程是否运行',
  `slave_io_running` TINYINT UNSIGNED NOT NULL COMMENT '复制 IO 线程是否运行',
  `replication_sql_thread_state` TINYINT NOT NULL DEFAULT 0 COMMENT '复制 SQL 线程状态编码',
  `replication_io_thread_state` TINYINT NOT NULL DEFAULT 0 COMMENT '复制 IO 线程状态编码',
  `has_replication_filters` TINYINT UNSIGNED NOT NULL COMMENT '是否配置复制过滤规则',
  `oracle_gtid` TINYINT UNSIGNED NOT NULL COMMENT '是否使用 Oracle MySQL GTID',
  `master_uuid` VARCHAR(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '上游实例 UUID',
  `ancestry_uuid` TEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '复制祖先 UUID 集合',
  `executed_gtid_set` TEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '已执行 GTID 集合',
  `gtid_purged` TEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '已清理 GTID 集合',
  `gtid_errant` TEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '游离 GTID 集合',
  `supports_oracle_gtid` TINYINT UNSIGNED NOT NULL COMMENT '实例是否支持 Oracle MySQL GTID',
  `mariadb_gtid` TINYINT UNSIGNED NOT NULL COMMENT '是否使用 MariaDB GTID',
  `pseudo_gtid` TINYINT UNSIGNED NOT NULL COMMENT '是否检测到伪 GTID',
  `master_log_file` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '复制 IO 线程读取的上游 binlog 文件',
  `read_master_log_pos` BIGINT UNSIGNED NOT NULL COMMENT '复制 IO 线程读取的上游 binlog 位点',
  `relay_master_log_file` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '复制 SQL 线程对应的上游 binlog 文件',
  `exec_master_log_pos` BIGINT UNSIGNED NOT NULL COMMENT '复制 SQL 线程执行的上游 binlog 位点',
  `relay_log_file` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '当前 relay log 文件名',
  `relay_log_pos` BIGINT UNSIGNED NOT NULL COMMENT '当前 relay log 位点',
  `last_sql_error` TEXT NOT NULL COMMENT '复制 SQL 线程最近错误',
  `last_io_error` TEXT NOT NULL COMMENT '复制 IO 线程最近错误',
  `seconds_behind_master` BIGINT UNSIGNED DEFAULT NULL COMMENT '数据库报告的复制延迟秒数',
  `slave_lag_seconds` BIGINT UNSIGNED DEFAULT NULL COMMENT 'orchestrator 计算的复制延迟秒数',
  `sql_delay` INT UNSIGNED NOT NULL COMMENT '配置的延迟复制秒数',
  `allow_tls` TINYINT UNSIGNED NOT NULL COMMENT '复制连接是否允许 TLS',
  `num_slave_hosts` INT UNSIGNED NOT NULL COMMENT '下游实例数量',
  `slave_hosts` TEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '下游实例列表',
  `cluster_name` VARCHAR(128) NOT NULL COMMENT '实例所属集群名称',
  `suggested_cluster_alias` VARCHAR(128) NOT NULL COMMENT '实例建议的集群别名',
  `data_center` VARCHAR(32) NOT NULL COMMENT '实例所在数据中心',
  `region` VARCHAR(32) NOT NULL COMMENT '实例所在地域',
  `physical_environment` VARCHAR(32) NOT NULL COMMENT '实例所在物理环境',
  `instance_alias` VARCHAR(128) NOT NULL COMMENT '实例展示别名',
  `semi_sync_enforced` TINYINT UNSIGNED NOT NULL COMMENT '是否强制半同步复制策略',
  `semi_sync_available` TINYINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '半同步复制能力是否可用',
  `replication_depth` TINYINT UNSIGNED NOT NULL COMMENT '实例在复制拓扑中的深度',
  `is_co_master` TINYINT UNSIGNED NOT NULL COMMENT '实例是否属于双主拓扑',
  `replication_credentials_available` TINYINT UNSIGNED NOT NULL COMMENT '复制凭据是否可获取',
  `has_replication_credentials` TINYINT UNSIGNED NOT NULL COMMENT '实例是否已配置复制凭据',
  `version_comment` VARCHAR(128) NOT NULL DEFAULT '' COMMENT '数据库发行版说明',
  `major_version` VARCHAR(16) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '数据库主版本',
  `binlog_row_image` VARCHAR(16) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT 'ROW 格式 binlog 镜像模式',
  `last_discovery_latency` BIGINT NOT NULL COMMENT '最近发现操作耗时',
  `semi_sync_master_enabled` TINYINT UNSIGNED NOT NULL COMMENT '半同步主库插件是否启用',
  `semi_sync_master_timeout` BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '半同步主库等待超时毫秒数',
  `semi_sync_master_wait_for_slave_count` INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '半同步主库要求的确认副本数',
  `semi_sync_master_status` TINYINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '半同步主库当前状态',
  `semi_sync_master_clients` INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '半同步主库客户端数量',
  `semi_sync_replica_status` TINYINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '半同步副本当前状态',
  `semi_sync_replica_enabled` TINYINT UNSIGNED NOT NULL COMMENT '半同步副本插件是否启用',
  `gtid_mode` VARCHAR(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT 'GTID 模式',
  `replication_group_name` VARCHAR(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL DEFAULT '' COMMENT 'Group Replication 组名',
  `replication_group_is_single_primary_mode` TINYINT UNSIGNED NOT NULL DEFAULT 1 COMMENT '复制组是否为单主模式',
  `replication_group_member_state` VARCHAR(16) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL DEFAULT '' COMMENT '复制组成员状态',
  `replication_group_member_role` VARCHAR(16) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL DEFAULT '' COMMENT '复制组成员角色',
  `replication_group_members` TEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '复制组成员集合',
  `replication_group_primary_host` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL DEFAULT '' COMMENT '复制组主节点主机名',
  `replication_group_primary_port` SMALLINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '复制组主节点端口',
  PRIMARY KEY (`hostname`, `port`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci COMMENT = '数据库实例发现状态与复制拓扑快照';

CREATE INDEX `idx_database_instance_cluster` ON `database_instance` (`cluster_name`);
CREATE INDEX `idx_database_instance_last_checked` ON `database_instance` (`last_checked`);
CREATE INDEX `idx_database_instance_last_seen` ON `database_instance` (`last_seen`);
CREATE INDEX `idx_database_instance_master` ON `database_instance` (`master_host`, `master_port`);
CREATE INDEX `idx_database_instance_suggested_alias` ON `database_instance` (`suggested_cluster_alias`);

CREATE TABLE IF NOT EXISTS `database_instance_maintenance` (
  `database_instance_maintenance_id` INT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '维护记录主键',
  `hostname` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '维护实例主机名',
  `port` SMALLINT UNSIGNED NOT NULL COMMENT '维护实例端口',
  `maintenance_active` TINYINT DEFAULT NULL COMMENT '维护窗口是否生效',
  `begin_timestamp` TIMESTAMP NULL DEFAULT NULL COMMENT '维护开始时间',
  `end_timestamp` TIMESTAMP NULL DEFAULT NULL COMMENT '维护结束时间',
  `owner` VARCHAR(128) NOT NULL COMMENT '维护发起人',
  `reason` TEXT NOT NULL COMMENT '维护原因',
  `processing_node_hostname` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '处理维护请求的节点主机名',
  `processing_node_token` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '处理维护请求的节点令牌',
  `explicitly_bounded` TINYINT UNSIGNED NOT NULL COMMENT '维护窗口是否显式设置结束时间',
  PRIMARY KEY (`database_instance_maintenance_id`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci COMMENT = '数据库实例维护窗口';

CREATE UNIQUE INDEX `unq_instance_maintenance_active_host_port` ON `database_instance_maintenance` (`maintenance_active`, `hostname`, `port`);
CREATE INDEX `idx_instance_maintenance_active_begin` ON `database_instance_maintenance` (`maintenance_active`, `begin_timestamp`);
CREATE INDEX `idx_instance_maintenance_active_end` ON `database_instance_maintenance` (`maintenance_active`, `end_timestamp`);

CREATE TABLE IF NOT EXISTS `database_instance_long_running_queries` (
  `hostname` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '数据库实例主机名',
  `port` SMALLINT UNSIGNED NOT NULL COMMENT '数据库实例端口',
  `process_id` BIGINT NOT NULL COMMENT '数据库会话标识',
  `process_started_at` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '会话开始时间',
  `process_user` VARCHAR(16) NOT NULL COMMENT '会话用户',
  `process_host` VARCHAR(128) NOT NULL COMMENT '会话来源主机',
  `process_db` VARCHAR(128) NOT NULL COMMENT '会话当前数据库',
  `process_command` VARCHAR(16) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '会话命令类型',
  `process_time_seconds` INT NOT NULL COMMENT '会话已运行秒数',
  `process_state` VARCHAR(128) NOT NULL COMMENT '会话执行状态',
  `process_info` VARCHAR(1024) NOT NULL COMMENT '会话语句摘要',
  PRIMARY KEY (`hostname`, `port`, `process_id`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci COMMENT = '数据库实例长时间运行会话快照';

CREATE INDEX `idx_long_query_started_at` ON `database_instance_long_running_queries` (`process_started_at`);

CREATE TABLE IF NOT EXISTS `audit` (
  `audit_id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '审计记录主键',
  `audit_timestamp` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '审计事件发生时间',
  `audit_type` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '审计事件类型',
  `hostname` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL DEFAULT '' COMMENT '关联实例主机名',
  `port` SMALLINT UNSIGNED NOT NULL COMMENT '关联实例端口',
  `cluster_name` VARCHAR(128) NOT NULL DEFAULT '' COMMENT '关联集群名称',
  `message` TEXT NOT NULL COMMENT '审计事件详情',
  PRIMARY KEY (`audit_id`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci COMMENT = 'orchestrator 操作审计日志';

CREATE INDEX `idx_audit_timestamp` ON `audit` (`audit_timestamp`);
CREATE INDEX `idx_audit_host_port_timestamp` ON `audit` (`hostname`, `port`, `audit_timestamp`);

CREATE TABLE IF NOT EXISTS `host_agent` (
  `hostname` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT 'Agent 主机名',
  `port` SMALLINT UNSIGNED NOT NULL COMMENT 'Agent 服务端口',
  `token` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT 'Agent 身份令牌',
  `last_submitted` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '最近一次提交状态的时间',
  `last_checked` TIMESTAMP NULL DEFAULT NULL COMMENT '最近一次检查 Agent 的时间',
  `last_seen` TIMESTAMP NULL DEFAULT NULL COMMENT '最近一次确认 Agent 在线的时间',
  `mysql_port` SMALLINT UNSIGNED DEFAULT NULL COMMENT 'Agent 管理的 MySQL 端口',
  `count_mysql_snapshots` SMALLINT UNSIGNED NOT NULL COMMENT 'Agent 持有的 MySQL 快照数量',
  PRIMARY KEY (`hostname`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci COMMENT = '主机 Agent 注册与健康状态';

CREATE INDEX `idx_host_agent_token` ON `host_agent` (`token`);
CREATE INDEX `idx_host_agent_last_submitted` ON `host_agent` (`last_submitted`);
CREATE INDEX `idx_host_agent_last_checked` ON `host_agent` (`last_checked`);
CREATE INDEX `idx_host_agent_last_seen` ON `host_agent` (`last_seen`);

CREATE TABLE IF NOT EXISTS `agent_seed` (
  `agent_seed_id` INT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '种子任务主键',
  `target_hostname` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '目标主机名',
  `source_hostname` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '来源主机名',
  `start_timestamp` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '种子任务开始时间',
  `end_timestamp` TIMESTAMP NOT NULL DEFAULT '1971-01-01 00:00:00' COMMENT '种子任务结束时间',
  `is_complete` TINYINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '种子任务是否完成',
  `is_successful` TINYINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '种子任务是否成功',
  PRIMARY KEY (`agent_seed_id`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci COMMENT = 'Agent 数据种子任务';

CREATE INDEX `idx_agent_seed_target_complete` ON `agent_seed` (`target_hostname`, `is_complete`);
CREATE INDEX `idx_agent_seed_source_complete` ON `agent_seed` (`source_hostname`, `is_complete`);
CREATE INDEX `idx_agent_seed_started_at` ON `agent_seed` (`start_timestamp`);
CREATE INDEX `idx_agent_seed_complete_started` ON `agent_seed` (`is_complete`, `start_timestamp`);
CREATE INDEX `idx_agent_seed_success_started` ON `agent_seed` (`is_successful`, `start_timestamp`);

CREATE TABLE IF NOT EXISTS `agent_seed_state` (
  `agent_seed_state_id` INT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '种子任务状态主键',
  `agent_seed_id` INT UNSIGNED NOT NULL COMMENT '关联的种子任务标识',
  `state_timestamp` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '状态发生时间',
  `state_action` VARCHAR(127) NOT NULL COMMENT '状态动作',
  `error_message` VARCHAR(255) NOT NULL COMMENT '状态错误信息',
  PRIMARY KEY (`agent_seed_state_id`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci COMMENT = 'Agent 种子任务状态流水';

CREATE INDEX `idx_agent_seed_state_seed_time` ON `agent_seed_state` (`agent_seed_id`, `state_timestamp`);

CREATE TABLE IF NOT EXISTS `host_attributes` (
  `hostname` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '主机名',
  `attribute_name` VARCHAR(128) NOT NULL COMMENT '属性名称',
  `attribute_value` VARCHAR(128) NOT NULL COMMENT '属性值',
  `submit_timestamp` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '属性提交时间',
  `expire_timestamp` TIMESTAMP NULL DEFAULT NULL COMMENT '属性过期时间',
  PRIMARY KEY (`hostname`, `attribute_name`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci COMMENT = '主机自定义属性';

CREATE INDEX `idx_host_attributes_name` ON `host_attributes` (`attribute_name`);
CREATE INDEX `idx_host_attributes_value` ON `host_attributes` (`attribute_value`);
CREATE INDEX `idx_host_attributes_submitted_at` ON `host_attributes` (`submit_timestamp`);
CREATE INDEX `idx_host_attributes_expires_at` ON `host_attributes` (`expire_timestamp`);

CREATE TABLE IF NOT EXISTS `hostname_resolve` (
  `hostname` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '待解析主机名',
  `resolved_hostname` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '解析后的规范主机名',
  `resolved_timestamp` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '最近解析时间',
  PRIMARY KEY (`hostname`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci COMMENT = '主机名正向解析缓存';

CREATE INDEX `idx_hostname_resolve_resolved_at` ON `hostname_resolve` (`resolved_timestamp`);

CREATE TABLE IF NOT EXISTS `cluster_alias` (
  `cluster_name` VARCHAR(128) NOT NULL COMMENT '集群名称',
  `alias` VARCHAR(128) NOT NULL COMMENT '集群展示别名',
  `last_registered` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '别名最近注册时间',
  PRIMARY KEY (`cluster_name`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci COMMENT = '自动发现的集群别名';

CREATE UNIQUE INDEX `unq_cluster_alias_alias` ON `cluster_alias` (`alias`);
CREATE INDEX `idx_cluster_alias_registered_at` ON `cluster_alias` (`last_registered`);

CREATE TABLE IF NOT EXISTS `node_health` (
  `hostname` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT 'orchestrator 节点主机名',
  `token` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '节点进程令牌',
  `last_seen_active` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '节点最近活跃时间',
  `extra_info` VARCHAR(128) NOT NULL COMMENT '节点附加状态',
  `command` VARCHAR(128) NOT NULL COMMENT '节点启动命令',
  `app_version` VARCHAR(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL DEFAULT '' COMMENT '节点应用版本',
  `first_seen_active` TIMESTAMP NOT NULL DEFAULT '1971-01-01 00:00:00' COMMENT '节点首次活跃时间',
  `db_backend` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL DEFAULT '' COMMENT '节点使用的元数据库后端',
  `incrementing_indicator` BIGINT NOT NULL DEFAULT 0 COMMENT '节点递增健康指示值',
  PRIMARY KEY (`hostname`, `token`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci COMMENT = 'orchestrator 节点健康状态';

CREATE INDEX `idx_node_health_last_seen_active` ON `node_health` (`last_seen_active`);

CREATE TABLE IF NOT EXISTS `topology_recovery` (
  `recovery_id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '恢复记录主键',
  `hostname` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '故障实例主机名',
  `port` SMALLINT UNSIGNED NOT NULL COMMENT '故障实例端口',
  `in_active_period` TINYINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '恢复是否处于活动窗口',
  `start_active_period` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '恢复活动窗口开始时间',
  `end_active_period_unixtime` INT UNSIGNED DEFAULT NULL COMMENT '恢复活动窗口结束 Unix 时间',
  `end_recovery` TIMESTAMP NULL DEFAULT NULL COMMENT '恢复执行结束时间',
  `processing_node_hostname` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '执行恢复的节点主机名',
  `processcing_node_token` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '执行恢复的节点令牌，保留历史拼写',
  `is_successful` TINYINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '恢复是否成功',
  `successor_hostname` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin DEFAULT NULL COMMENT '恢复后继实例主机名',
  `successor_port` SMALLINT UNSIGNED DEFAULT NULL COMMENT '恢复后继实例端口',
  `analysis` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '触发恢复的分析代码',
  `cluster_name` VARCHAR(128) NOT NULL COMMENT '故障集群名称',
  `cluster_alias` VARCHAR(128) NOT NULL COMMENT '故障集群别名',
  `count_affected_slaves` INT UNSIGNED NOT NULL COMMENT '受影响下游实例数量',
  `slave_hosts` TEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '受影响下游实例列表',
  `participating_instances` TEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '参与恢复的实例列表',
  `lost_slaves` TEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '恢复中丢失的下游实例列表',
  `all_errors` TEXT NOT NULL COMMENT '恢复过程错误汇总',
  `acknowledged` TINYINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '恢复记录是否已确认',
  `acknowledged_at` TIMESTAMP NULL DEFAULT NULL COMMENT '恢复记录确认时间',
  `acknowledged_by` VARCHAR(128) NOT NULL COMMENT '恢复记录确认人',
  `acknowledge_comment` TEXT NOT NULL COMMENT '恢复记录确认说明',
  `last_detection_id` BIGINT UNSIGNED NOT NULL COMMENT '关联的最近故障检测标识',
  `successor_alias` VARCHAR(128) DEFAULT NULL COMMENT '恢复后继实例别名',
  `uid` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '恢复流程稳定标识',
  PRIMARY KEY (`recovery_id`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci COMMENT = '拓扑故障恢复执行记录';

CREATE UNIQUE INDEX `unq_topology_recovery_active_instance` ON `topology_recovery` (`hostname`, `port`, `in_active_period`, `end_active_period_unixtime`);
CREATE INDEX `idx_topology_recovery_active_started` ON `topology_recovery` (`in_active_period`, `start_active_period`);
CREATE INDEX `idx_topology_recovery_started_at` ON `topology_recovery` (`start_active_period`);
CREATE INDEX `idx_topology_recovery_cluster_active` ON `topology_recovery` (`cluster_name`, `in_active_period`);
CREATE INDEX `idx_topology_recovery_ended_at` ON `topology_recovery` (`end_recovery`);
CREATE INDEX `idx_topology_recovery_acknowledged_at` ON `topology_recovery` (`acknowledged`, `acknowledged_at`);
CREATE INDEX `idx_topology_recovery_detection` ON `topology_recovery` (`last_detection_id`);
CREATE INDEX `idx_topology_recovery_uid` ON `topology_recovery` (`uid`);

CREATE TABLE IF NOT EXISTS `hostname_unresolve` (
  `hostname` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '规范主机名',
  `unresolved_hostname` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '规范化前的主机名',
  `last_registered` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '映射最近注册时间',
  PRIMARY KEY (`hostname`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci COMMENT = '主机名反向映射缓存';

CREATE INDEX `idx_hostname_unresolve_original` ON `hostname_unresolve` (`unresolved_hostname`);
CREATE INDEX `idx_hostname_unresolve_registered_at` ON `hostname_unresolve` (`last_registered`);

CREATE TABLE IF NOT EXISTS `database_instance_pool` (
  `hostname` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '数据库实例主机名',
  `port` SMALLINT UNSIGNED NOT NULL COMMENT '数据库实例端口',
  `pool` VARCHAR(128) NOT NULL COMMENT '实例所属资源池',
  `registered_at` TIMESTAMP NOT NULL DEFAULT '1971-01-01 00:00:00' COMMENT '资源池关系注册时间',
  PRIMARY KEY (`hostname`, `port`, `pool`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci COMMENT = '数据库实例资源池关系';

CREATE INDEX `idx_database_instance_pool_name` ON `database_instance_pool` (`pool`);

CREATE TABLE IF NOT EXISTS `database_instance_topology_history` (
  `snapshot_unix_timestamp` INT UNSIGNED NOT NULL COMMENT '拓扑快照 Unix 时间',
  `hostname` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '数据库实例主机名',
  `port` SMALLINT UNSIGNED NOT NULL COMMENT '数据库实例端口',
  `master_host` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '快照中的上游主机名',
  `master_port` SMALLINT UNSIGNED NOT NULL COMMENT '快照中的上游端口',
  `cluster_name` VARCHAR(128) NOT NULL COMMENT '快照中的集群名称',
  `version` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '快照中的数据库版本',
  PRIMARY KEY (`snapshot_unix_timestamp`, `hostname`, `port`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci COMMENT = '数据库复制拓扑历史快照';

CREATE INDEX `idx_topology_history_snapshot_cluster` ON `database_instance_topology_history` (`snapshot_unix_timestamp`, `cluster_name`);

CREATE TABLE IF NOT EXISTS `candidate_database_instance` (
  `hostname` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '候选实例主机名',
  `port` SMALLINT UNSIGNED NOT NULL COMMENT '候选实例端口',
  `last_suggested` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '最近一次候选建议时间',
  `priority` TINYINT NOT NULL DEFAULT 1 COMMENT '候选优先级，正值倾向提升，负值倾向回避',
  `promotion_rule` VARCHAR(16) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL DEFAULT 'neutral' COMMENT '实例提升规则',
  PRIMARY KEY (`hostname`, `port`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci COMMENT = '故障恢复候选实例策略';

CREATE INDEX `idx_candidate_instance_suggested_at` ON `candidate_database_instance` (`last_suggested`);

CREATE TABLE IF NOT EXISTS `database_instance_downtime` (
  `hostname` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '停机实例主机名',
  `port` SMALLINT UNSIGNED NOT NULL COMMENT '停机实例端口',
  `downtime_active` TINYINT DEFAULT NULL COMMENT '停机窗口是否生效',
  `begin_timestamp` TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP COMMENT '停机窗口开始时间',
  `end_timestamp` TIMESTAMP NULL DEFAULT NULL COMMENT '停机窗口结束时间',
  `owner` VARCHAR(128) NOT NULL COMMENT '停机登记人',
  `reason` TEXT NOT NULL COMMENT '停机原因',
  PRIMARY KEY (`hostname`, `port`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci COMMENT = '数据库实例计划停机窗口';

CREATE INDEX `idx_instance_downtime_end` ON `database_instance_downtime` (`end_timestamp`);

CREATE TABLE IF NOT EXISTS `topology_failure_detection` (
  `detection_id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '故障检测记录主键',
  `hostname` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '故障实例主机名',
  `port` SMALLINT UNSIGNED NOT NULL COMMENT '故障实例端口',
  `in_active_period` TINYINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '检测是否处于活动窗口',
  `start_active_period` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '检测活动窗口开始时间',
  `end_active_period_unixtime` INT UNSIGNED NOT NULL COMMENT '检测活动窗口结束 Unix 时间',
  `processing_node_hostname` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '执行检测的节点主机名',
  `processcing_node_token` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '执行检测的节点令牌，保留历史拼写',
  `analysis` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '故障分析代码',
  `cluster_name` VARCHAR(128) NOT NULL COMMENT '故障集群名称',
  `cluster_alias` VARCHAR(128) NOT NULL COMMENT '故障集群别名',
  `count_affected_slaves` INT UNSIGNED NOT NULL COMMENT '受影响下游实例数量',
  `slave_hosts` TEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '受影响下游实例列表',
  `is_actionable` TINYINT NOT NULL DEFAULT 0 COMMENT '检测结果是否可触发恢复',
  PRIMARY KEY (`detection_id`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci COMMENT = '拓扑故障检测记录';

CREATE UNIQUE INDEX `unq_failure_detection_active_instance` ON `topology_failure_detection` (`hostname`, `port`, `in_active_period`, `end_active_period_unixtime`, `is_actionable`);
CREATE INDEX `idx_failure_detection_active_started` ON `topology_failure_detection` (`in_active_period`, `start_active_period`);

CREATE TABLE IF NOT EXISTS `hostname_resolve_history` (
  `resolved_hostname` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '解析后的规范主机名',
  `hostname` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '解析前主机名',
  `resolved_timestamp` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '解析记录时间',
  PRIMARY KEY (`resolved_hostname`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci COMMENT = '主机名正向解析历史';

CREATE INDEX `idx_hostname_resolve_history_source` ON `hostname_resolve_history` (`hostname`);
CREATE INDEX `idx_hostname_resolve_history_at` ON `hostname_resolve_history` (`resolved_timestamp`);

CREATE TABLE IF NOT EXISTS `hostname_unresolve_history` (
  `unresolved_hostname` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '规范化前主机名',
  `hostname` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '规范主机名',
  `last_registered` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '映射最近注册时间',
  PRIMARY KEY (`unresolved_hostname`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci COMMENT = '主机名反向映射历史';

CREATE INDEX `idx_hostname_unresolve_history_host` ON `hostname_unresolve_history` (`hostname`);
CREATE INDEX `idx_hostname_unresolve_history_at` ON `hostname_unresolve_history` (`last_registered`);

CREATE TABLE IF NOT EXISTS `cluster_domain_name` (
  `cluster_name` VARCHAR(128) NOT NULL COMMENT '集群名称',
  `domain_name` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '集群对外域名',
  `last_registered` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '域名最近注册时间',
  PRIMARY KEY (`cluster_name`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci COMMENT = '集群域名映射';

CREATE INDEX `idx_cluster_domain_name_domain` ON `cluster_domain_name` (`domain_name`(32));
CREATE INDEX `idx_cluster_domain_name_registered_at` ON `cluster_domain_name` (`last_registered`);

CREATE TABLE IF NOT EXISTS `master_position_equivalence` (
  `equivalence_id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '等价位点记录主键',
  `master1_hostname` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '第一实例主机名',
  `master1_port` SMALLINT UNSIGNED NOT NULL COMMENT '第一实例端口',
  `master1_binary_log_file` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '第一实例 binlog 文件名',
  `master1_binary_log_pos` BIGINT UNSIGNED NOT NULL COMMENT '第一实例 binlog 位点',
  `master2_hostname` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '第二实例主机名',
  `master2_port` SMALLINT UNSIGNED NOT NULL COMMENT '第二实例端口',
  `master2_binary_log_file` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '第二实例 binlog 文件名',
  `master2_binary_log_pos` BIGINT UNSIGNED NOT NULL COMMENT '第二实例 binlog 位点',
  `last_suggested` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '等价关系最近建议时间',
  PRIMARY KEY (`equivalence_id`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci COMMENT = '跨实例 binlog 等价位点';

CREATE UNIQUE INDEX `unq_master_position_equivalence_pair` ON `master_position_equivalence` (`master1_hostname`, `master1_port`, `master1_binary_log_file`, `master1_binary_log_pos`, `master2_hostname`, `master2_port`);
CREATE INDEX `idx_master_position_equivalence_second` ON `master_position_equivalence` (`master2_hostname`, `master2_port`, `master2_binary_log_file`, `master2_binary_log_pos`);
CREATE INDEX `idx_master_position_equivalence_suggested` ON `master_position_equivalence` (`last_suggested`);

CREATE TABLE IF NOT EXISTS `async_request` (
  `request_id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '异步请求主键',
  `command` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '异步请求命令',
  `hostname` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '来源实例主机名',
  `port` SMALLINT UNSIGNED NOT NULL COMMENT '来源实例端口',
  `destination_hostname` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '目标实例主机名',
  `destination_port` SMALLINT UNSIGNED NOT NULL COMMENT '目标实例端口',
  `pattern` TEXT NOT NULL COMMENT '请求匹配模式',
  `gtid_hint` VARCHAR(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT 'GTID 执行提示',
  `begin_timestamp` TIMESTAMP NULL DEFAULT NULL COMMENT '请求开始时间',
  `end_timestamp` TIMESTAMP NULL DEFAULT NULL COMMENT '请求结束时间',
  `story` TEXT NOT NULL COMMENT '请求执行过程说明',
  PRIMARY KEY (`request_id`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci COMMENT = '兼容保留的异步拓扑请求';

CREATE INDEX `idx_async_request_begin` ON `async_request` (`begin_timestamp`);
CREATE INDEX `idx_async_request_end` ON `async_request` (`end_timestamp`);

CREATE TABLE IF NOT EXISTS `blocked_topology_recovery` (
  `hostname` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '被阻塞实例主机名',
  `port` SMALLINT UNSIGNED NOT NULL COMMENT '被阻塞实例端口',
  `cluster_name` VARCHAR(128) NOT NULL COMMENT '被阻塞集群名称',
  `analysis` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '被阻塞的故障分析代码',
  `last_blocked_timestamp` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '最近阻塞时间',
  `blocking_recovery_id` BIGINT UNSIGNED DEFAULT NULL COMMENT '造成阻塞的恢复记录标识',
  PRIMARY KEY (`hostname`, `port`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci COMMENT = '被其他恢复流程阻塞的拓扑恢复';

CREATE INDEX `idx_blocked_recovery_cluster_at` ON `blocked_topology_recovery` (`cluster_name`, `last_blocked_timestamp`);
CREATE INDEX `idx_blocked_recovery_at` ON `blocked_topology_recovery` (`last_blocked_timestamp`);

CREATE TABLE IF NOT EXISTS `database_instance_last_analysis` (
  `hostname` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '数据库实例主机名',
  `port` SMALLINT UNSIGNED NOT NULL COMMENT '数据库实例端口',
  `analysis_timestamp` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '最近分析时间',
  `analysis` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '最近分析结果代码',
  PRIMARY KEY (`hostname`, `port`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci COMMENT = '数据库实例最近分析结果';

CREATE INDEX `idx_instance_last_analysis_at` ON `database_instance_last_analysis` (`analysis_timestamp`);

CREATE TABLE IF NOT EXISTS `database_instance_analysis_changelog` (
  `changelog_id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '分析变更记录主键',
  `hostname` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '数据库实例主机名',
  `port` SMALLINT UNSIGNED NOT NULL COMMENT '数据库实例端口',
  `analysis_timestamp` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '分析结果发生时间',
  `analysis` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '分析结果代码',
  PRIMARY KEY (`changelog_id`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci COMMENT = '数据库实例分析结果变更流水';

CREATE INDEX `idx_instance_analysis_log_at` ON `database_instance_analysis_changelog` (`analysis_timestamp`);
CREATE INDEX `idx_instance_analysis_log_host_port_at` ON `database_instance_analysis_changelog` (`hostname`, `port`, `analysis_timestamp`);

CREATE TABLE IF NOT EXISTS `node_health_history` (
  `history_id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '节点健康历史主键',
  `hostname` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT 'orchestrator 节点主机名',
  `token` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '节点进程令牌',
  `first_seen_active` TIMESTAMP NOT NULL DEFAULT '1971-01-01 00:00:00' COMMENT '节点首次活跃时间',
  `extra_info` VARCHAR(128) NOT NULL COMMENT '节点附加状态',
  `command` VARCHAR(128) NOT NULL COMMENT '节点启动命令',
  `app_version` VARCHAR(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL DEFAULT '' COMMENT '节点应用版本',
  PRIMARY KEY (`history_id`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci COMMENT = 'orchestrator 节点健康历史';

CREATE UNIQUE INDEX `unq_node_health_history_host_token` ON `node_health_history` (`hostname`, `token`);
CREATE INDEX `idx_node_health_history_first_seen` ON `node_health_history` (`first_seen_active`);

CREATE TABLE IF NOT EXISTS `database_instance_coordinates_history` (
  `history_id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '复制位点历史主键',
  `hostname` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '数据库实例主机名',
  `port` SMALLINT UNSIGNED NOT NULL COMMENT '数据库实例端口',
  `recorded_timestamp` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '复制位点记录时间',
  `last_seen` TIMESTAMP NOT NULL DEFAULT '1971-01-01 00:00:00' COMMENT '实例最近可见时间',
  `binary_log_file` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT 'binlog 文件名',
  `binary_log_pos` BIGINT UNSIGNED NOT NULL COMMENT 'binlog 位点',
  `relay_log_file` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT 'relay log 文件名',
  `relay_log_pos` BIGINT UNSIGNED NOT NULL COMMENT 'relay log 位点',
  PRIMARY KEY (`history_id`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci COMMENT = '数据库实例复制位点历史';

CREATE INDEX `idx_instance_coordinates_host_port_at` ON `database_instance_coordinates_history` (`hostname`, `port`, `recorded_timestamp`);
CREATE INDEX `idx_instance_coordinates_recorded_at` ON `database_instance_coordinates_history` (`recorded_timestamp`);

CREATE TABLE IF NOT EXISTS `database_instance_binlog_files_history` (
  `history_id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT 'binlog 文件历史主键',
  `hostname` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '数据库实例主机名',
  `port` SMALLINT UNSIGNED NOT NULL COMMENT '数据库实例端口',
  `binary_log_file` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT 'binlog 文件名',
  `binary_log_pos` BIGINT UNSIGNED NOT NULL COMMENT 'binlog 文件最大已知位点',
  `first_seen` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '首次发现文件时间',
  `last_seen` TIMESTAMP NOT NULL DEFAULT '1971-01-01 00:00:00' COMMENT '最近发现文件时间',
  PRIMARY KEY (`history_id`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci COMMENT = '兼容保留的数据库实例 binlog 文件历史';

CREATE UNIQUE INDEX `unq_instance_binlog_files_host_port_file` ON `database_instance_binlog_files_history` (`hostname`, `port`, `binary_log_file`);
CREATE INDEX `idx_instance_binlog_files_last_seen` ON `database_instance_binlog_files_history` (`last_seen`);

CREATE TABLE IF NOT EXISTS `access_token` (
  `access_token_id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '访问令牌记录主键',
  `public_token` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '公开令牌标识',
  `secret_token` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '私密令牌内容',
  `generated_at` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '令牌生成时间',
  `generated_by` VARCHAR(128) NOT NULL COMMENT '令牌生成人',
  `is_acquired` TINYINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '令牌是否已领取',
  `is_reentrant` TINYINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '令牌是否允许重复领取',
  `acquired_at` TIMESTAMP NOT NULL DEFAULT '1971-01-01 00:00:00' COMMENT '令牌领取时间',
  PRIMARY KEY (`access_token_id`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci COMMENT = '短期访问令牌';

CREATE UNIQUE INDEX `unq_access_token_public_token` ON `access_token` (`public_token`);
CREATE INDEX `idx_access_token_generated_at` ON `access_token` (`generated_at`);

CREATE TABLE IF NOT EXISTS `database_instance_recent_relaylog_history` (
  `hostname` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '数据库实例主机名',
  `port` SMALLINT UNSIGNED NOT NULL COMMENT '数据库实例端口',
  `current_relay_log_file` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '当前 relay log 文件名',
  `current_relay_log_pos` BIGINT UNSIGNED NOT NULL COMMENT '当前 relay log 位点',
  `current_seen` TIMESTAMP NOT NULL DEFAULT '1971-01-01 00:00:00' COMMENT '当前位点观察时间',
  `prev_relay_log_file` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '上一 relay log 文件名',
  `prev_relay_log_pos` BIGINT UNSIGNED NOT NULL COMMENT '上一 relay log 位点',
  `prev_seen` TIMESTAMP NOT NULL DEFAULT '1971-01-01 00:00:00' COMMENT '上一位点观察时间',
  PRIMARY KEY (`hostname`, `port`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci COMMENT = '兼容保留的最近 relay log 位点';

CREATE INDEX `idx_recent_relaylog_current_seen` ON `database_instance_recent_relaylog_history` (`current_seen`);

CREATE TABLE IF NOT EXISTS `orchestrator_metadata` (
  `anchor` TINYINT UNSIGNED NOT NULL COMMENT '单行记录锚点',
  `last_deployed_version` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '最近部署的应用版本',
  `last_deployed_timestamp` TIMESTAMP NOT NULL DEFAULT '1971-01-01 00:00:00' COMMENT '最近部署完成时间',
  PRIMARY KEY (`anchor`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci COMMENT = '兼容保留的旧版部署元数据';

CREATE TABLE IF NOT EXISTS `global_recovery_disable` (
  `disable_recovery` TINYINT UNSIGNED NOT NULL COMMENT '值为 1 时全局禁用恢复',
  PRIMARY KEY (`disable_recovery`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci COMMENT = '全局恢复禁用开关';

CREATE TABLE IF NOT EXISTS `cluster_alias_override` (
  `cluster_name` VARCHAR(128) NOT NULL COMMENT '集群名称',
  `alias` VARCHAR(128) NOT NULL COMMENT '人工覆盖的集群别名',
  PRIMARY KEY (`cluster_name`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci COMMENT = '集群别名人工覆盖';

CREATE TABLE IF NOT EXISTS `topology_recovery_steps` (
  `recovery_step_id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '恢复步骤记录主键',
  `recovery_uid` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '关联的恢复流程标识',
  `audit_at` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '恢复步骤记录时间',
  `message` TEXT NOT NULL COMMENT '恢复步骤说明',
  PRIMARY KEY (`recovery_step_id`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci COMMENT = '拓扑恢复步骤审计流水';

CREATE INDEX `idx_topology_recovery_steps_uid` ON `topology_recovery_steps` (`recovery_uid`);

CREATE TABLE IF NOT EXISTS `raft_store` (
  `store_id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '旧版 Raft 键值记录主键',
  `store_key` VARBINARY(512) NOT NULL COMMENT '旧版 Raft 键',
  `store_value` BLOB NOT NULL COMMENT '旧版 Raft 值',
  PRIMARY KEY (`store_id`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci COMMENT = '兼容保留的旧版 SQL Raft 存储';

CREATE INDEX `idx_raft_store_key` ON `raft_store` (`store_key`);

CREATE TABLE IF NOT EXISTS `raft_log` (
  `log_index` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '旧版 Raft 日志索引',
  `term` BIGINT NOT NULL COMMENT '旧版 Raft 任期',
  `log_type` INT NOT NULL COMMENT '旧版 Raft 日志类型',
  `data` BLOB NOT NULL COMMENT '旧版 Raft 日志内容',
  PRIMARY KEY (`log_index`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci COMMENT = '兼容保留的旧版 SQL Raft 日志';

CREATE TABLE IF NOT EXISTS `raft_snapshot` (
  `snapshot_id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '旧版 Raft 快照主键',
  `snapshot_name` VARCHAR(128) NOT NULL COMMENT '旧版 Raft 快照名称',
  `snapshot_meta` VARCHAR(4096) NOT NULL COMMENT '旧版 Raft 快照元数据',
  `created_at` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '旧版 Raft 快照创建时间',
  PRIMARY KEY (`snapshot_id`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci COMMENT = '兼容保留的旧版 SQL Raft 快照';

CREATE UNIQUE INDEX `unq_raft_snapshot_name` ON `raft_snapshot` (`snapshot_name`);

CREATE TABLE IF NOT EXISTS `database_instance_peer_analysis` (
  `peer` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '执行分析的对等节点',
  `hostname` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '数据库实例主机名',
  `port` SMALLINT UNSIGNED NOT NULL COMMENT '数据库实例端口',
  `analysis_timestamp` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '对等节点分析时间',
  `analysis` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '对等节点分析结果代码',
  PRIMARY KEY (`peer`, `hostname`, `port`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci COMMENT = '多节点数据库实例分析结果';

CREATE TABLE IF NOT EXISTS `database_instance_tls` (
  `hostname` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '数据库实例主机名',
  `port` SMALLINT UNSIGNED NOT NULL COMMENT '数据库实例端口',
  `required` TINYINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '实例连接是否要求 TLS',
  PRIMARY KEY (`hostname`, `port`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci COMMENT = '数据库实例 TLS 要求缓存';

CREATE TABLE IF NOT EXISTS `kv_store` (
  `store_key` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '键值条目键名',
  `store_value` TEXT NOT NULL COMMENT '键值条目内容',
  `last_updated` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '键值条目最近更新时间',
  PRIMARY KEY (`store_key`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci COMMENT = 'orchestrator 内部键值存储';

CREATE TABLE IF NOT EXISTS `cluster_injected_pseudo_gtid` (
  `cluster_name` VARCHAR(128) NOT NULL COMMENT '集群名称',
  `time_injected` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '最近注入伪 GTID 的时间',
  PRIMARY KEY (`cluster_name`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci COMMENT = '集群伪 GTID 注入时间';

CREATE TABLE IF NOT EXISTS `hostname_ips` (
  `hostname` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '主机名',
  `ipv4` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT 'IPv4 地址',
  `ipv6` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT 'IPv6 地址',
  `last_updated` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '地址映射最近更新时间',
  PRIMARY KEY (`hostname`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci COMMENT = '主机名与 IP 地址缓存';

CREATE TABLE IF NOT EXISTS `database_instance_tags` (
  `hostname` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '数据库实例主机名',
  `port` SMALLINT UNSIGNED NOT NULL COMMENT '数据库实例端口',
  `tag_name` VARCHAR(128) NOT NULL COMMENT '标签名称',
  `tag_value` VARCHAR(128) NOT NULL COMMENT '标签值',
  `last_updated` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '标签最近更新时间',
  PRIMARY KEY (`hostname`, `port`, `tag_name`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci COMMENT = '数据库实例标签';

CREATE INDEX `idx_database_instance_tags_name` ON `database_instance_tags` (`tag_name`);

CREATE TABLE IF NOT EXISTS `database_instance_stale_binlog_coordinates` (
  `hostname` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '数据库实例主机名',
  `port` SMALLINT UNSIGNED NOT NULL COMMENT '数据库实例端口',
  `binary_log_file` VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '长期未变化的 binlog 文件名',
  `binary_log_pos` BIGINT UNSIGNED NOT NULL COMMENT '长期未变化的 binlog 位点',
  `first_seen` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '首次确认位点未变化的时间',
  PRIMARY KEY (`hostname`, `port`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci COMMENT = '数据库实例停滞 binlog 位点';

CREATE INDEX `idx_stale_binlog_coordinates_first_seen` ON `database_instance_stale_binlog_coordinates` (`first_seen`);

INSERT IGNORE INTO `orchestrator_schema_migrations` (`migration_id`, `applied_at`)
VALUES ('canonical-v1', CURRENT_TIMESTAMP);

DELETE FROM `orchestrator_schema_migrations`
WHERE `migration_id` = 'canonical-v1-pending';
