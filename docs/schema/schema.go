// Package schema exposes the executable metadata schema documented in this directory.
package schema

import (
	_ "embed"
	"strings"
)

const (
	// CanonicalMigration marks databases bootstrapped from the canonical schema.
	CanonicalMigration = "canonical-v1"
	// CanonicalMigrationPending marks an interrupted or in-progress canonical bootstrap.
	CanonicalMigrationPending = "canonical-v1-pending"
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
