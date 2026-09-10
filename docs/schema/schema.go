// Package schema exposes the executable metadata schema documented in this directory.
package schema

import (
	_ "embed"
	"strings"
)

const (
	// CanonicalMigration marks databases bootstrapped from the canonical schema.
	CanonicalMigration = "canonical-v2"
	// CanonicalMigrationPending marks an interrupted or in-progress canonical bootstrap.
	CanonicalMigrationPending = "canonical-v2-pending"
	// LegacyMigration marks databases that continue to use the historical patch stream.
	LegacyMigration = "legacy-v1"
)

//go:embed mysql.sql
var mysqlSchema string

var managedTables = []string{
	"access_token",
	"agent_seed",
	"agent_seed_state",
	"async_request",
	"audit",
	"blocked_topology_recovery",
	"candidate_database_instance",
	"cluster_alias",
	"cluster_alias_override",
	"cluster_domain_name",
	"cluster_injected_pseudo_gtid",
	"database_instance",
	"database_instance_analysis_changelog",
	"database_instance_binlog_files_history",
	"database_instance_coordinates_history",
	"database_instance_downtime",
	"database_instance_last_analysis",
	"database_instance_long_running_queries",
	"database_instance_maintenance",
	"database_instance_peer_analysis",
	"database_instance_pool",
	"database_instance_recent_relaylog_history",
	"database_instance_stale_binlog_coordinates",
	"database_instance_tags",
	"database_instance_tls",
	"database_instance_topology_history",
	"global_recovery_disable",
	"host_agent",
	"host_attributes",
	"hostname_ips",
	"hostname_resolve",
	"hostname_resolve_history",
	"hostname_unresolve",
	"hostname_unresolve_history",
	"kv_store",
	"master_position_equivalence",
	"node_health",
	"node_health_history",
	"orchestrator_db_deployments",
	"orchestrator_metadata",
	"orchestrator_schema_migrations",
	"raft_log",
	"raft_snapshot",
	"raft_store",
	"recovery_hook_assignment",
	"recovery_hook_profile",
	"recovery_policy",
	"topology_failure_detection",
	"topology_recovery",
	"topology_recovery_steps",
}

// MySQL returns the canonical MySQL-compatible schema text.
func MySQL() string {
	return mysqlSchema
}

// Statements returns the canonical schema as individual executable statements.
func Statements() []string {
	parts := strings.Split(mysqlSchema, ";")
	statements := make([]string, 0, len(parts))
	for _, part := range parts {
		if statement := strings.TrimSpace(part); statement != "" {
			statements = append(statements, statement)
		}
	}
	return statements
}

// ManagedTables returns the tables owned by the orchestrator metadata schema.
func ManagedTables() []string {
	return append([]string(nil), managedTables...)
}

// PreviousCanonicalMigration 标记升级前的 canonical 布局。
const PreviousCanonicalMigration = "canonical-v1"

// UpgradeMigrationPending 标记显式主键迁移，区别于空库初始化。
const UpgradeMigrationPending = "auto-id-v2-pending"

//go:embed migrations/mysql-v1.sql
var mysqlSchemaV1 string

// StatementsV1 仅供续跑旧版初始化与迁移测试使用。
func StatementsV1() []string {
	var result []string
	for part := range strings.SplitSeq(mysqlSchemaV1, ";") {
		if statement := strings.TrimSpace(part); statement != "" {
			result = append(result, statement)
		}
	}
	return result
}

// LegacyAutoID 返回历史自增主键名称；空字符串表示 id 是新增加的节点本地代理键。
func LegacyAutoID(table string) string { return legacyAutoIDs[table] }

var legacyAutoIDs = map[string]string{
	"database_instance_maintenance":          "database_instance_maintenance_id",
	"audit":                                  "audit_id",
	"agent_seed":                             "agent_seed_id",
	"agent_seed_state":                       "agent_seed_state_id",
	"topology_recovery":                      "recovery_id",
	"topology_failure_detection":             "detection_id",
	"master_position_equivalence":            "equivalence_id",
	"async_request":                          "request_id",
	"database_instance_analysis_changelog":   "changelog_id",
	"node_health_history":                    "history_id",
	"database_instance_coordinates_history":  "history_id",
	"database_instance_binlog_files_history": "history_id",
	"access_token":                           "access_token_id",
	"topology_recovery_steps":                "recovery_step_id",
	"raft_store":                             "store_id",
	"raft_log":                               "log_index",
	"raft_snapshot":                          "snapshot_id",
}

// BusinessKey 返回从历史主键保留的业务唯一键。
func BusinessKey(table string) []string { return append([]string(nil), businessKeys[table]...) }

var businessKeys = map[string][]string{
	"orchestrator_schema_migrations":             {"migration_id"},
	"orchestrator_db_deployments":                {"deployed_version"},
	"database_instance":                          {"hostname", "port"},
	"database_instance_long_running_queries":     {"hostname", "port", "process_id"},
	"host_agent":                                 {"hostname"},
	"host_attributes":                            {"hostname", "attribute_name"},
	"hostname_resolve":                           {"hostname"},
	"cluster_alias":                              {"cluster_name"},
	"node_health":                                {"hostname", "token"},
	"hostname_unresolve":                         {"hostname"},
	"database_instance_pool":                     {"hostname", "port", "pool"},
	"database_instance_topology_history":         {"snapshot_unix_timestamp", "hostname", "port"},
	"candidate_database_instance":                {"hostname", "port"},
	"database_instance_downtime":                 {"hostname", "port"},
	"hostname_resolve_history":                   {"resolved_hostname"},
	"hostname_unresolve_history":                 {"unresolved_hostname"},
	"cluster_domain_name":                        {"cluster_name"},
	"blocked_topology_recovery":                  {"hostname", "port"},
	"database_instance_last_analysis":            {"hostname", "port"},
	"database_instance_recent_relaylog_history":  {"hostname", "port"},
	"orchestrator_metadata":                      {"anchor"},
	"global_recovery_disable":                    {"disable_recovery"},
	"cluster_alias_override":                     {"cluster_name"},
	"database_instance_peer_analysis":            {"peer", "hostname", "port"},
	"database_instance_tls":                      {"hostname", "port"},
	"kv_store":                                   {"store_key"},
	"cluster_injected_pseudo_gtid":               {"cluster_name"},
	"hostname_ips":                               {"hostname"},
	"database_instance_tags":                     {"hostname", "port", "tag_name"},
	"database_instance_stale_binlog_coordinates": {"hostname", "port"},
	"recovery_policy":                            {"scope_type", "scope_key"},
	"recovery_hook_profile":                      {"profile_id"},
	"recovery_hook_assignment":                   {"scope_type", "scope_key", "phase"},
}
