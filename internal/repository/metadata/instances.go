package metadata

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	modeldo "github.com/openark/orchestrator/internal/models/do"
	modeldomain "github.com/openark/orchestrator/internal/models/domain"
	"github.com/openark/orchestrator/internal/repository/database"
)

func ReadClusterAliasOverride(ctx context.Context, clusterName string) ([]string, error) {
	rows, err := database.QueryOrchestratorRows[modeldo.ClusterAliasOverride](ctx, `
		select alias
		from cluster_alias_override
		where cluster_name = ?
	`, clusterName)
	result := make([]string, 0, len(rows))
	for _, row := range rows {
		result = append(result, row.Alias)
	}
	return result, err
}

func ReadReplicationGroupPrimary(ctx context.Context, groupName string) ([]modeldomain.InstanceIdentity, error) {
	rows, err := database.QueryOrchestratorRows[modeldo.ReplicationGroupPrimary](ctx, `
		select replication_group_primary_host, replication_group_primary_port
		from database_instance
		where replication_group_name = ? and replication_group_member_role = 'PRIMARY'
	`, groupName)
	result := make([]modeldomain.InstanceIdentity, 0, len(rows))
	for _, row := range rows {
		result = append(result, modeldomain.InstanceIdentity(row))
	}
	return result, err
}

func ReadInstanceClusterAttributes(ctx context.Context, hostname string, port int) ([]modeldomain.InstanceClusterAttributes, error) {
	rows, err := database.QueryOrchestratorRows[modeldo.InstanceClusterAttributes](ctx, `
		select
			cluster_name,
			suggested_cluster_alias,
			replication_depth,
			master_host,
			master_port,
			ancestry_uuid,
			executed_gtid_set
		from database_instance
		where hostname = ? and port = ?
	`, hostname, port)
	result := make([]modeldomain.InstanceClusterAttributes, 0, len(rows))
	for _, row := range rows {
		result = append(result, modeldomain.InstanceClusterAttributes(row))
	}
	return result, err
}

func ReadInstancePromotionRule(ctx context.Context, hostname string, port int) ([]string, error) {
	rows, err := database.QueryOrchestratorRows[modeldo.PromotionRule](ctx, `
		select ifnull(nullif(promotion_rule, ''), 'neutral') as promotion_rule
		from candidate_database_instance
		where hostname = ? and port = ?
	`, hostname, port)
	result := make([]string, 0, len(rows))
	for _, row := range rows {
		result = append(result, row.PromotionRule)
	}
	return result, err
}

// ReadInstanceRows is the shared metadata projection for the legacy instance
// read API. Conditions and ordering are assembled by the business read helpers,
// while repository owns the SQL execution and stable persistence projection.
func readInstanceRows(ctx context.Context, condition, orderBy string, args ...any) ([]modeldomain.BackendInstanceRecord, error) {
	if orderBy == "" {
		orderBy = "hostname, port"
	}
	rows, err := database.QueryOrchestratorRows[modeldo.BackendInstance](ctx, fmt.Sprintf(`
		select
			*,
			unix_timestamp() - unix_timestamp(last_checked) as seconds_since_last_checked,
			ifnull(last_checked <= last_seen, 0) as is_last_check_valid,
			unix_timestamp() - unix_timestamp(last_seen) as seconds_since_last_seen,
			candidate_database_instance.last_suggested is not null
				and candidate_database_instance.promotion_rule in ('must', 'prefer') as is_candidate,
			ifnull(nullif(candidate_database_instance.promotion_rule, ''), 'neutral') as promotion_rule,
			ifnull(unresolved_hostname, '') as unresolved_hostname,
			(database_instance_downtime.downtime_active is not null
				and ifnull(database_instance_downtime.end_timestamp, now()) > now()) as is_downtimed,
			ifnull(database_instance_downtime.reason, '') as downtime_reason,
			ifnull(database_instance_downtime.owner, '') as downtime_owner,
			ifnull(unix_timestamp() - unix_timestamp(begin_timestamp), 0) as elapsed_downtime_seconds,
			ifnull(database_instance_downtime.end_timestamp, '') as downtime_end_timestamp
		from database_instance
		left join candidate_database_instance using (hostname, port)
		left join hostname_unresolve using (hostname)
		left join database_instance_downtime using (hostname, port)
		where %s
		order by %s
	`, condition, orderBy), args...)
	result := make([]modeldomain.BackendInstanceRecord, 0, len(rows))
	for _, row := range rows {
		result = append(result, modeldomain.BackendInstanceRecord(row))
	}
	return result, err
}

// ReadAllInstanceRows reads every current instance in stable key order.
func ReadAllInstanceRows(ctx context.Context) ([]modeldomain.BackendInstanceRecord, error) {
	return readInstanceRows(ctx, "1 = 1", "")
}

// ReadAllInstanceRowsByTopology reads every current instance in topology display order.
func ReadAllInstanceRowsByTopology(ctx context.Context) ([]modeldomain.BackendInstanceRecord, error) {
	return readInstanceRows(ctx, "1 = 1", "replication_depth asc, num_slave_hosts desc, cluster_name, hostname, port")
}

// ReadInstanceRowsByKey reads the instance identified by hostname and port.
func ReadInstanceRowsByKey(ctx context.Context, hostname string, port int) ([]modeldomain.BackendInstanceRecord, error) {
	return readInstanceRows(ctx, "hostname = ? and port = ?", "", hostname, port)
}

// ReadClusterInstanceRows reads every instance in a cluster.
func ReadClusterInstanceRows(ctx context.Context, clusterName string) ([]modeldomain.BackendInstanceRecord, error) {
	return readInstanceRows(ctx, "cluster_name = ?", "", clusterName)
}

// ReadWritableClusterMasterRows reads writable root or co-master instances for a cluster.
func ReadWritableClusterMasterRows(ctx context.Context, clusterName string) ([]modeldomain.BackendInstanceRecord, error) {
	return readInstanceRows(ctx, `
		cluster_name = ?
		and read_only = 0
		and (replication_depth = 0 or is_co_master)
	`, "replication_depth asc", clusterName)
}

// ReadClusterMasterRows reads root or co-master instances for a cluster.
func ReadClusterMasterRows(ctx context.Context, clusterName string) ([]modeldomain.BackendInstanceRecord, error) {
	return readInstanceRows(ctx, `
		cluster_name = ?
		and (replication_depth = 0 or is_co_master)
	`, "read_only asc, replication_depth asc", clusterName)
}

// ReadWritableClusterMastersRows reads writable root or co-master instances for all clusters.
func ReadWritableClusterMastersRows(ctx context.Context) ([]modeldomain.BackendInstanceRecord, error) {
	return readInstanceRows(ctx, `
		read_only = 0
		and (replication_depth = 0 or is_co_master)
	`, "cluster_name asc, replication_depth asc")
}

// ReadReplicaInstanceRows reads direct replicas of one instance.
func ReadReplicaInstanceRows(ctx context.Context, masterHostname string, masterPort int) ([]modeldomain.BackendInstanceRecord, error) {
	return readInstanceRows(ctx, "master_host = ? and master_port = ?", "", masterHostname, masterPort)
}

// ReadBinlogServerReplicaRows reads direct binlog-server replicas of one instance.
func ReadBinlogServerReplicaRows(ctx context.Context, masterHostname string, masterPort int) ([]modeldomain.BackendInstanceRecord, error) {
	return readInstanceRows(ctx, `
		master_host = ?
		and master_port = ?
		and binlog_server = 1
	`, "", masterHostname, masterPort)
}

// ReadUnseenInstanceRows reads instances whose last check is newer than their last sighting.
func ReadUnseenInstanceRows(ctx context.Context) ([]modeldomain.BackendInstanceRecord, error) {
	return readInstanceRows(ctx, "last_seen < last_checked", "")
}

// ReadProblemInstanceRows reads instances that match persisted problem indicators.
func ReadProblemInstanceRows(ctx context.Context, clusterName string, staleCheckSeconds uint, reasonableLagSeconds int) ([]modeldomain.BackendInstanceRecord, error) {
	return readInstanceRows(ctx, `
		cluster_name LIKE (CASE WHEN ? = '' THEN '%' ELSE ? END)
		and (
			(last_seen < last_checked)
			or (unix_timestamp() - unix_timestamp(last_checked) > ?)
			or (replication_sql_thread_state not in (-1, 1))
			or (replication_io_thread_state not in (-1, 1))
			or (abs(cast(seconds_behind_master as signed) - cast(sql_delay as signed)) > ?)
			or (abs(cast(slave_lag_seconds as signed) - cast(sql_delay as signed)) > ?)
			or (gtid_errant != '')
			or (replication_group_name != '' and replication_group_member_state != 'ONLINE')
		)
	`, "", clusterName, clusterName, staleCheckSeconds, reasonableLagSeconds, reasonableLagSeconds)
}

// SearchInstanceRows searches instance identity, cluster, version, alias, and server ID fields.
func SearchInstanceRows(ctx context.Context, search string) ([]modeldomain.BackendInstanceRecord, error) {
	return readInstanceRows(ctx, `
		instr(hostname, ?) > 0
		or instr(cluster_name, ?) > 0
		or instr(version, ?) > 0
		or instr(version_comment, ?) > 0
		or instr(concat(hostname, ':', port), ?) > 0
		or instr(suggested_cluster_alias, ?) > 0
		or concat(server_id, '') = ?
		or concat(port, '') = ?
	`, "replication_depth asc, num_slave_hosts desc, cluster_name, hostname, port",
		search, search, search, search, search, search, search, search)
}

// ReadFuzzyInstanceRows reads instances whose hostname contains the supplied fragment and whose port matches.
func ReadFuzzyInstanceRows(ctx context.Context, hostnameFragment string, port int) ([]modeldomain.BackendInstanceRecord, error) {
	return readInstanceRows(ctx, `
		hostname like concat('%%', ?, '%%')
		and port = ?
	`, "replication_depth asc, num_slave_hosts desc, cluster_name, hostname, port", hostnameFragment, port)
}

// ReadLostInRecoveryInstanceRows reads instances currently downtimed for a recovery-loss reason.
func ReadLostInRecoveryInstanceRows(ctx context.Context, reason, clusterName string) ([]modeldomain.BackendInstanceRecord, error) {
	return readInstanceRows(ctx, `
		ifnull(
			database_instance_downtime.downtime_active = 1
			and database_instance_downtime.end_timestamp > now()
			and database_instance_downtime.reason = ?, 0)
		and ? IN ('', cluster_name)
	`, "cluster_name asc, replication_depth asc", reason, clusterName)
}

// ReadDowntimedInstanceRows reads instances with a current downtime, optionally for one cluster.
func ReadDowntimedInstanceRows(ctx context.Context, clusterName string) ([]modeldomain.BackendInstanceRecord, error) {
	return readInstanceRows(ctx, `
		ifnull(
			database_instance_downtime.downtime_active = 1
			and database_instance_downtime.end_timestamp > now(), 0)
		and ? IN ('', cluster_name)
	`, "cluster_name asc, replication_depth asc", clusterName)
}

// ReadClusterCandidateInstanceRows reads preferred failover candidates in a cluster.
func ReadClusterCandidateInstanceRows(ctx context.Context, clusterName string) ([]modeldomain.BackendInstanceRecord, error) {
	return readInstanceRows(ctx, `
		cluster_name = ?
		and concat(hostname, ':', port) in (
			select concat(hostname, ':', port)
			from candidate_database_instance
			where promotion_rule in ('must', 'prefer')
		)
	`, "", clusterName)
}

// ReadClusterInstancesAtDepthRows reads instances at an exact replication depth in a cluster.
func ReadClusterInstancesAtDepthRows(ctx context.Context, clusterName string, replicationDepth uint) ([]modeldomain.BackendInstanceRecord, error) {
	return readInstanceRows(ctx, "replication_depth = ? and cluster_name = ?", "", replicationDepth, clusterName)
}

// ReadClusterGhostInstanceRows reads row-based replicas that may be suitable for gh-ost.
func ReadClusterGhostInstanceRows(ctx context.Context, clusterName string) ([]modeldomain.BackendInstanceRecord, error) {
	return readInstanceRows(ctx, `
		replication_depth > 0
		and binlog_format = 'ROW'
		and cluster_name = ?
	`, "num_slave_hosts asc", clusterName)
}

func UpdateInstanceClusterName(ctx context.Context, hostname string, port int, clusterName string) error {
	_, err := database.ExecOrchestratorContext(ctx, `
		update database_instance set cluster_name = ? where hostname = ? and port = ?
	`, clusterName, hostname, port)
	return err
}

func ReplaceInstanceClusterName(ctx context.Context, oldClusterName, newClusterName string) error {
	_, err := database.ExecOrchestratorContext(ctx, `
		update database_instance set cluster_name = ? where cluster_name = ?
	`, newClusterName, oldClusterName)
	return err
}

func ReadUnseenMasterKeys(ctx context.Context) ([]modeldomain.InstanceIdentity, error) {
	rows, err := database.QueryOrchestratorRows[modeldo.MasterKey](ctx, `
		select distinct slave_instance.master_host, slave_instance.master_port
		from database_instance slave_instance
		left join hostname_resolve on slave_instance.master_host = hostname_resolve.hostname
		left join database_instance master_instance on (
			coalesce(hostname_resolve.resolved_hostname, slave_instance.master_host) = master_instance.hostname
			and slave_instance.master_port = master_instance.port
		)
		where
			master_instance.last_checked is null
			and slave_instance.master_host != ''
			and slave_instance.master_host != '_'
			and slave_instance.master_port > 0
			and slave_instance.slave_io_running = 1
	`)
	result := make([]modeldomain.InstanceIdentity, 0, len(rows))
	for _, row := range rows {
		result = append(result, modeldomain.InstanceIdentity(row))
	}
	return result, err
}

func ForgetUnseenDifferentlyResolvedInstances(ctx context.Context) (int64, error) {
	rows, err := database.QueryOrchestratorRows[modeldo.InstanceKey](ctx, `
		select database_instance.hostname, database_instance.port
		from hostname_resolve
		join database_instance on hostname_resolve.hostname = database_instance.hostname
		where
			hostname_resolve.hostname != hostname_resolve.resolved_hostname
			and ifnull(last_checked <= last_seen, 0) = 0
	`)
	if err != nil {
		return 0, err
	}
	var affected int64
	for _, row := range rows {
		result, err := database.ExecOrchestratorContext(ctx, `
			delete from database_instance where hostname = ? and port = ?
		`, row.Hostname, row.Port)
		if err != nil {
			return affected, err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return affected, err
		}
		affected += count
	}
	return affected, nil
}

func ReadUnknownMasterHostnameResolves(ctx context.Context) ([]modeldomain.MasterHostnameResolve, error) {
	rows, err := database.QueryOrchestratorRows[modeldo.MasterHostnameResolve](ctx, `
		select distinct slave_instance.master_host, hostname_resolve_history.resolved_hostname
		from database_instance slave_instance
		left join hostname_resolve on slave_instance.master_host = hostname_resolve.hostname
		left join database_instance master_instance on (
			coalesce(hostname_resolve.resolved_hostname, slave_instance.master_host) = master_instance.hostname
			and slave_instance.master_port = master_instance.port
		)
		left join hostname_resolve_history on slave_instance.master_host = hostname_resolve_history.hostname
		where
			master_instance.last_checked is null
			and slave_instance.master_host != ''
			and slave_instance.master_host != '_'
			and slave_instance.master_port > 0
	`)
	result := make([]modeldomain.MasterHostnameResolve, 0, len(rows))
	for _, row := range rows {
		result = append(result, modeldomain.MasterHostnameResolve(row))
	}
	return result, err
}

func ReadHostSnapshotCounts(ctx context.Context, hostnames []string) ([]modeldomain.SnapshotCount, error) {
	if len(hostnames) == 0 {
		return nil, nil
	}
	args := make([]any, len(hostnames))
	for index, hostname := range hostnames {
		args[index] = hostname
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(hostnames)), ",")
	rows, err := database.QueryOrchestratorRows[modeldo.SnapshotCount](ctx, fmt.Sprintf(`
		select hostname, count_mysql_snapshots
		from host_agent
		where hostname in (%s)
		order by hostname
	`, placeholders), args...)
	result := make([]modeldomain.SnapshotCount, 0, len(rows))
	for _, row := range rows {
		result = append(result, modeldomain.SnapshotCount(row))
	}
	return result, err
}

func ReadInstanceClusterName(ctx context.Context, hostname string, port int) ([]string, error) {
	rows, err := database.QueryOrchestratorRows[modeldo.InstanceClusterName](ctx, `
		select ifnull(max(cluster_name), '') as cluster_name
		from database_instance
		where hostname = ? and port = ?
	`, hostname, port)
	result := make([]string, 0, len(rows))
	for _, row := range rows {
		result = append(result, row.ClusterName)
	}
	return result, err
}

func ReadClusterInfoRows(ctx context.Context, clusterName string) ([]modeldomain.ClusterInfoRecord, error) {
	where := ""
	args := []any{}
	if clusterName != "" {
		where, args = "where cluster_name = ?", append(args, clusterName)
	}
	rows, err := database.QueryOrchestratorRows[modeldo.ClusterInfo](ctx, fmt.Sprintf(`
		select
			cluster_name,
			count(*) as count_instances,
			ifnull(min(alias), cluster_name) as alias,
			ifnull(min(domain_name), '') as domain_name
		from database_instance
		left join cluster_alias using (cluster_name)
		left join cluster_domain_name using (cluster_name)
		%s
		group by cluster_name
	`, where), args...)
	result := make([]modeldomain.ClusterInfoRecord, 0, len(rows))
	for _, row := range rows {
		result = append(result, modeldomain.ClusterInfoRecord(row))
	}
	return result, err
}

func ReadAllInstanceKeys(ctx context.Context) ([]modeldomain.InstanceIdentity, error) {
	rows, err := database.QueryOrchestratorRows[modeldo.InstanceKey](ctx, `select hostname, port from database_instance`)
	result := make([]modeldomain.InstanceIdentity, 0, len(rows))
	for _, row := range rows {
		result = append(result, modeldomain.InstanceIdentity(row))
	}
	return result, err
}

func ReadAllMinimalInstances(ctx context.Context) ([]modeldomain.MinimalInstanceRecord, error) {
	rows, err := database.QueryOrchestratorRows[modeldo.MinimalInstance](ctx, `
		select hostname, port, master_host, master_port, cluster_name from database_instance
	`)
	result := make([]modeldomain.MinimalInstanceRecord, 0, len(rows))
	for _, row := range rows {
		result = append(result, modeldomain.MinimalInstanceRecord(row))
	}
	return result, err
}

func ReadOutdatedInstanceKeys(ctx context.Context, pollSeconds uint) ([]modeldomain.InstanceIdentity, error) {
	rows, err := database.QueryOrchestratorRows[modeldo.InstanceKey](ctx, `
		select hostname, port
		from database_instance
		where case
			when last_attempted_check <= last_checked then last_checked < now() - interval ? second
			else last_checked < now() - interval ? second
		end
	`, pollSeconds, 2*pollSeconds)
	result := make([]modeldomain.InstanceIdentity, 0, len(rows))
	for _, row := range rows {
		result = append(result, modeldomain.InstanceIdentity(row))
	}
	return result, err
}

func buildInsertOnDuplicateKeyUpdate(table string, columns, values []string, rowCount int, insertIgnore bool) (string, error) {
	if len(columns) == 0 {
		return "", errors.New("column list cannot be empty")
	}
	if rowCount < 1 {
		return "", errors.New("nrRows must be a positive number")
	}
	if len(columns) != len(values) {
		return "", errors.New("number of values must be equal to number of columns")
	}
	ignore := ""
	if insertIgnore {
		ignore = "ignore"
	}
	valueRow := fmt.Sprintf("(%s)", strings.Join(values, ", "))
	var valueRows bytes.Buffer
	valueRows.WriteString(valueRow)
	for index := 1; index < rowCount; index++ {
		valueRows.WriteString(",\n                ")
		valueRows.WriteString(valueRow)
	}
	assignments := make([]string, 0, len(columns))
	for _, column := range columns {
		assignments = append(assignments, fmt.Sprintf("%s=VALUES(%s)", column, column))
	}
	return fmt.Sprintf(`INSERT %s INTO %s
		(%s)
	VALUES
		%s
	ON DUPLICATE KEY UPDATE
		%s
	`, ignore, table, strings.Join(columns, ", "), valueRows.String(), strings.Join(assignments, ", ")), nil
}

// WriteInstanceRows upserts discovery results without changing the legacy
// insert-ignore and last-seen semantics.
func WriteInstanceRows(ctx context.Context, rows []modeldomain.BackendInstanceRecord, instanceWasActuallyFound, updateLastSeen bool) (int, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	columns := []string{
		"hostname",
		"port",
		"last_checked",
		"last_attempted_check",
		"last_check_partial_success",
		"uptime",
		"server_id",
		"server_uuid",
		"version",
		"major_version",
		"version_comment",
		"binlog_server",
		"read_only",
		"binlog_format",
		"binlog_row_image",
		"log_bin",
		"log_slave_updates",
		"binary_log_file",
		"binary_log_pos",
		"master_host",
		"master_port",
		"slave_sql_running",
		"slave_io_running",
		"replication_sql_thread_state",
		"replication_io_thread_state",
		"has_replication_filters",
		"supports_oracle_gtid",
		"oracle_gtid",
		"master_uuid",
		"ancestry_uuid",
		"executed_gtid_set",
		"gtid_mode",
		"gtid_purged",
		"gtid_errant",
		"mariadb_gtid",
		"pseudo_gtid",
		"master_log_file",
		"read_master_log_pos",
		"relay_master_log_file",
		"exec_master_log_pos",
		"relay_log_file",
		"relay_log_pos",
		"last_sql_error",
		"last_io_error",
		"seconds_behind_master",
		"slave_lag_seconds",
		"sql_delay",
		"num_slave_hosts",
		"slave_hosts",
		"cluster_name",
		"suggested_cluster_alias",
		"data_center",
		"region",
		"physical_environment",
		"replication_depth",
		"is_co_master",
		"replication_credentials_available",
		"has_replication_credentials",
		"allow_tls",
		"semi_sync_enforced",
		"semi_sync_available",
		"semi_sync_master_enabled",
		"semi_sync_master_timeout",
		"semi_sync_master_wait_for_slave_count",
		"semi_sync_replica_enabled",
		"semi_sync_master_status",
		"semi_sync_master_clients",
		"semi_sync_replica_status",
		"instance_alias",
		"last_discovery_latency",
		"replication_group_name",
		"replication_group_is_single_primary_mode",
		"replication_group_member_state",
		"replication_group_member_role",
		"replication_group_members",
		"replication_group_primary_host",
		"replication_group_primary_port",
	}
	values := make([]string, len(columns))
	for index := range values {
		values[index] = "?"
	}
	values[2] = "NOW()"
	values[3] = "NOW()"
	values[4] = "1"
	if updateLastSeen {
		columns = append(columns, "last_seen")
		values = append(values, "NOW()")
	}

	args := make([]any, 0, len(rows)*(len(columns)-3))
	for _, row := range rows {
		args = append(args,
			row.Hostname,
			row.Port,
			row.Uptime,
			row.ServerID,
			row.ServerUUID,
			row.Version,
			row.MajorVersion,
			row.VersionComment,
			row.BinlogServer,
			row.ReadOnly,
			row.BinlogFormat,
			row.BinlogRowImage,
			row.LogBin,
			row.LogSlaveUpdates,
			row.BinaryLogFile,
			row.BinaryLogPos,
			row.MasterHost,
			row.MasterPort,
			row.SlaveSQLRunning,
			row.SlaveIORunning,
			row.ReplicationSQLThreadState,
			row.ReplicationIOThreadState,
			row.HasReplicationFilters,
			row.SupportsOracleGTID,
			row.OracleGTID,
			row.MasterUUID,
			row.AncestryUUID,
			row.ExecutedGTIDSet,
			row.GTIDMode,
			row.GTIDPurged,
			row.GTIDErrant,
			row.MariaDBGTID,
			row.PseudoGTID,
			row.MasterLogFile,
			row.ReadMasterLogPos,
			row.RelayMasterLogFile,
			row.ExecMasterLogPos,
			row.RelayLogFile,
			row.RelayLogPos,
			row.LastSQLError,
			row.LastIOError,
			row.SecondsBehindMaster,
			row.SlaveLagSeconds,
			row.SQLDelay,
			row.NumSlaveHosts,
			row.SlaveHosts,
			row.ClusterName,
			row.SuggestedClusterAlias,
			row.DataCenter,
			row.Region,
			row.PhysicalEnvironment,
			row.ReplicationDepth,
			row.CoMaster,
			row.ReplicationCredentialsAvailable,
			row.HasReplicationCredentials,
			row.AllowTLS,
			row.SemiSyncEnforced,
			row.SemiSyncAvailable,
			row.SemiSyncMasterEnabled,
			row.SemiSyncMasterTimeout,
			row.SemiSyncMasterWaitForReplicaCount,
			row.SemiSyncReplicaEnabled,
			row.SemiSyncMasterStatus,
			row.SemiSyncMasterClients,
			row.SemiSyncReplicaStatus,
			row.InstanceAlias,
			row.LastDiscoveryLatency,
			row.ReplicationGroupName,
			row.ReplicationGroupSinglePrimary,
			row.ReplicationGroupMemberState,
			row.ReplicationGroupMemberRole,
			row.ReplicationGroupMembers,
			row.ReplicationGroupPrimaryHost,
			row.ReplicationGroupPrimaryPort,
		)
	}
	statement, err := buildInsertOnDuplicateKeyUpdate(
		"database_instance", columns, values, len(rows), !instanceWasActuallyFound,
	)
	if err != nil {
		return len(args), err
	}
	_, err = database.ExecOrchestratorContext(ctx, statement, args...)
	return len(args), err
}

func UpdateInstanceLastChecked(ctx context.Context, hostname string, port int, partialSuccess bool) error {
	_, err := database.ExecOrchestratorContext(ctx, `
		update database_instance set
			last_checked = NOW(),
			last_check_partial_success = ?
		where hostname = ? and port = ?
	`, partialSuccess, hostname, port)
	return err
}

func UpdateInstanceLastAttemptedCheck(ctx context.Context, hostname string, port int) error {
	_, err := database.ExecOrchestratorContext(ctx, `
		update database_instance set last_attempted_check = NOW()
		where hostname = ? and port = ?
	`, hostname, port)
	return err
}

func ForgetInstance(ctx context.Context, hostname string, port int) (int64, error) {
	result, err := database.ExecOrchestratorContext(ctx, `
		delete from database_instance where hostname = ? and port = ?
	`, hostname, port)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func ForgetClusterInstances(ctx context.Context, clusterName string) error {
	_, err := database.ExecOrchestratorContext(ctx, `delete from database_instance where cluster_name = ?`, clusterName)
	return err
}

func ForgetLongUnseenInstances(ctx context.Context, unseenForgetHours uint) (int64, error) {
	result, err := database.ExecOrchestratorContext(ctx, `
		delete from database_instance where last_seen < NOW() - interval ? hour
	`, unseenForgetHours)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func SnapshotTopologies(ctx context.Context) error {
	_, err := database.ExecOrchestratorContext(ctx, `
		insert ignore into database_instance_topology_history (
			snapshot_unix_timestamp, hostname, port, master_host, master_port, cluster_name, version
		)
		select UNIX_TIMESTAMP(NOW()), hostname, port, master_host, master_port, cluster_name, version
		from database_instance
	`)
	return err
}

func ReadHistoryClusterInstanceRows(ctx context.Context, clusterName, timestampPattern string) ([]modeldomain.MinimalInstanceRecord, error) {
	rows, err := database.QueryOrchestratorRows[modeldo.HistoryInstance](ctx, `
		select *
		from database_instance_topology_history
		where snapshot_unix_timestamp rlike ? and cluster_name = ?
		order by hostname, port
	`, timestampPattern, clusterName)
	result := make([]modeldomain.MinimalInstanceRecord, 0, len(rows))
	for _, row := range rows {
		result = append(result, modeldomain.MinimalInstanceRecord(row))
	}
	return result, err
}

func PurgeInstanceCoordinatesHistory(ctx context.Context, retentionMinutes int) error {
	_, err := database.ExecOrchestratorContext(ctx, `
		delete from database_instance_coordinates_history
		where recorded_timestamp < NOW() - INTERVAL ? MINUTE
	`, retentionMinutes)
	return err
}

func SnapshotInstanceCoordinatesHistory(ctx context.Context) error {
	_, err := database.ExecOrchestratorContext(ctx, `
		insert into database_instance_coordinates_history (
			hostname, port, last_seen, recorded_timestamp,
			binary_log_file, binary_log_pos, relay_log_file, relay_log_pos
		)
		select
			hostname, port, last_seen, NOW(),
			binary_log_file, binary_log_pos, relay_log_file, relay_log_pos
		from database_instance
		where binary_log_file != '' or relay_log_file != ''
	`)
	return err
}

func ReadRecentInstanceCoordinates(ctx context.Context, hostname string, port int, ageMinutes int) ([]modeldomain.InstanceCoordinatesRecord, error) {
	rows, err := database.QueryOrchestratorRows[modeldo.InstanceCoordinates](ctx, `
		select binary_log_file, binary_log_pos, relay_log_file, relay_log_pos
		from database_instance_coordinates_history
		where
			hostname = ?
			and port = ?
			and recorded_timestamp <= NOW() - INTERVAL ? MINUTE
		order by recorded_timestamp desc
		limit 1
	`, hostname, port, ageMinutes)
	result := make([]modeldomain.InstanceCoordinatesRecord, 0, len(rows))
	for _, row := range rows {
		result = append(result, modeldomain.InstanceCoordinatesRecord(row))
	}
	return result, err
}

func RecordStaleInstanceBinlogCoordinates(ctx context.Context, hostname string, port int, binlogFile string, binlogPos int64) error {
	args := []any{hostname, port, binlogFile, binlogPos}
	if _, err := database.ExecOrchestratorContext(ctx, `
		delete from database_instance_stale_binlog_coordinates
		where hostname = ? and port = ?
			and (binary_log_file != ? or binary_log_pos != ?)
	`, args...); err != nil {
		return err
	}
	_, err := database.ExecOrchestratorContext(ctx, `
		insert ignore into database_instance_stale_binlog_coordinates (
			hostname, port, binary_log_file, binary_log_pos, first_seen
		) values (?, ?, ?, ?, NOW())
	`, args...)
	return err
}

func ExpireStaleInstanceBinlogCoordinates(ctx context.Context, expireSeconds int) error {
	_, err := database.ExecOrchestratorContext(ctx, `
		delete from database_instance_stale_binlog_coordinates
		where first_seen < NOW() - INTERVAL ? SECOND
	`, expireSeconds)
	return err
}

func ReadPreviousRelayLogCoordinates(ctx context.Context, hostname string, port int, relayFile string, relayPos int64) ([]modeldomain.RelayCoordinatesRecord, error) {
	rows, err := database.QueryOrchestratorRows[modeldo.RelayCoordinates](ctx, `
		select relay_log_file, relay_log_pos
		from database_instance_coordinates_history
		where
			hostname = ?
			and port = ?
			and (relay_log_file, relay_log_pos) < (?, ?)
			and relay_log_file != ''
			and relay_log_pos != 0
		order by recorded_timestamp desc
		limit 1
	`, hostname, port, relayFile, relayPos)
	result := make([]modeldomain.RelayCoordinatesRecord, 0, len(rows))
	for _, row := range rows {
		result = append(result, modeldomain.RelayCoordinatesRecord(row))
	}
	return result, err
}

func ResetInstanceRelayLogCoordinatesHistory(ctx context.Context, hostname string, port int) error {
	_, err := database.ExecOrchestratorContext(ctx, `
		update database_instance_coordinates_history
		set relay_log_file = '', relay_log_pos = 0
		where hostname = ? and port = ?
	`, hostname, port)
	return err
}

func RegisterInjectedPseudoGTID(ctx context.Context, clusterName string) error {
	_, err := database.ExecOrchestratorContext(ctx, `
		insert into cluster_injected_pseudo_gtid (cluster_name, time_injected)
		values (?, now())
		on duplicate key update
			cluster_name = values(cluster_name),
			time_injected = now()
	`, clusterName)
	return err
}

func ExpireInjectedPseudoGTID(ctx context.Context, expireMinutes int) error {
	_, err := database.ExecOrchestratorContext(ctx, `
		delete from cluster_injected_pseudo_gtid
		where time_injected < NOW() - INTERVAL ? MINUTE
	`, expireMinutes)
	return err
}

func IsInjectedPseudoGTID(ctx context.Context, clusterName string) (bool, error) {
	rows, err := database.QueryOrchestratorRows[modeldo.InjectedPseudoGTID](ctx, `
		select count(*) as is_injected
		from cluster_injected_pseudo_gtid
		where cluster_name = ?
	`, clusterName)
	if err != nil || len(rows) == 0 {
		return false, err
	}
	return rows[0].Injected, nil
}
