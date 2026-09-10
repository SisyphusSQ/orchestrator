package metadata

import (
	"context"
	"fmt"

	modeldo "github.com/openark/orchestrator/internal/models/do"
	modeldomain "github.com/openark/orchestrator/internal/models/domain"
	"github.com/openark/orchestrator/internal/repository/database"
)

type ReplicationAnalysisQuery struct {
	LockedSeconds         int
	ValidCheckSeconds     uint
	ReasonableLagSeconds  int
	ClusterName           string
	ReduceCount           bool
	ReductionCheckSeconds uint
}

func ReadReplicationAnalysisRows(ctx context.Context, input ReplicationAnalysisQuery) ([]modeldomain.ReplicationAnalysisRecord, error) {
	args := []any{input.LockedSeconds, input.ValidCheckSeconds, input.ReasonableLagSeconds, input.ClusterName}
	reductionClause := ""
	if input.ReduceCount {
		reductionClause = `
		HAVING
			(
				MIN(
					master_instance.last_checked <= master_instance.last_seen
					and master_instance.last_attempted_check <= master_instance.last_seen + interval ? second
				) = 1
			) = 0
			OR IFNULL(SUM(
				replica_instance.last_checked <= replica_instance.last_seen
				AND replica_instance.slave_io_running = 0
				AND (
					replica_instance.last_io_error like '%error %connecting to master%'
					OR replica_instance.last_io_error like '%error %connecting to source%'
				)
				AND replica_instance.slave_sql_running = 1
			), 0) > 0
			OR IFNULL(SUM(replica_instance.last_checked <= replica_instance.last_seen), 0) < COUNT(replica_instance.server_id)
			OR IFNULL(SUM(
				replica_instance.last_checked <= replica_instance.last_seen
				AND replica_instance.slave_io_running != 0
				AND replica_instance.slave_sql_running != 0
			), 0) < COUNT(replica_instance.server_id)
			OR MIN(
				master_instance.slave_sql_running = 1
				AND master_instance.slave_io_running = 0
				AND (
					master_instance.last_io_error like '%error %connecting to master%'
					OR master_instance.last_io_error like '%error %connecting to source%'
				)
			)
			OR COUNT(replica_instance.server_id) > 0
		`
		args = append(args, input.ReductionCheckSeconds)
	}

	query := fmt.Sprintf(`
		SELECT
			master_instance.hostname,
			master_instance.port,
			master_instance.read_only AS read_only,
			MIN(master_instance.data_center) AS data_center,
			MIN(master_instance.region) AS region,
			MIN(master_instance.physical_environment) AS physical_environment,
			MIN(master_instance.master_host) AS master_host,
			MIN(master_instance.master_port) AS master_port,
			MIN(master_instance.cluster_name) AS cluster_name,
			MIN(master_instance.binary_log_file) AS binary_log_file,
			MIN(master_instance.binary_log_pos) AS binary_log_pos,
			MIN(IFNULL(
				master_instance.binary_log_file = database_instance_stale_binlog_coordinates.binary_log_file
				AND master_instance.binary_log_pos = database_instance_stale_binlog_coordinates.binary_log_pos
				AND database_instance_stale_binlog_coordinates.first_seen < NOW() - interval ? second,
				0
			)) AS is_stale_binlog_coordinates,
			MIN(IFNULL(cluster_alias.alias, master_instance.cluster_name)) AS cluster_alias,
			MIN(IFNULL(cluster_domain_name.domain_name, master_instance.cluster_name)) AS cluster_domain,
			MIN(
				master_instance.last_checked <= master_instance.last_seen
				and master_instance.last_attempted_check <= master_instance.last_seen + interval ? second
			) = 1 AS is_last_check_valid,
			MIN(master_instance.last_check_partial_success) as last_check_partial_success,
			MIN(
				(
					master_instance.master_host IN ('', '_')
					OR master_instance.master_port = 0
					OR substr(master_instance.master_host, 1, 2) = '//'
				)
				AND (
					master_instance.replication_group_name = ''
					OR master_instance.replication_group_member_role = 'PRIMARY'
				)
			) AS is_master,
			MIN(
				master_instance.replication_group_name != ''
				AND master_instance.replication_group_member_state != 'OFFLINE'
			) AS is_replication_group_member,
			MIN(master_instance.is_co_master) AS is_co_master,
			MIN(CONCAT(master_instance.hostname, ':', master_instance.port) = master_instance.cluster_name) AS is_cluster_master,
			MIN(master_instance.gtid_mode) AS gtid_mode,
			COUNT(replica_instance.server_id) AS count_replicas,
			IFNULL(SUM(replica_instance.last_checked <= replica_instance.last_seen), 0) AS count_valid_replicas,
			IFNULL(SUM(
				replica_instance.last_checked <= replica_instance.last_seen
				AND replica_instance.slave_io_running != 0
				AND replica_instance.slave_sql_running != 0
			), 0) AS count_valid_replicating_replicas,
			IFNULL(SUM(
				replica_instance.last_checked <= replica_instance.last_seen
				AND replica_instance.slave_io_running = 0
				AND (
					replica_instance.last_io_error like '%%error %%connecting to master%%'
					OR replica_instance.last_io_error like '%%error %%connecting to source%%'
				)
				AND replica_instance.slave_sql_running = 1
			), 0) AS count_replicas_failing_to_connect_to_master,
			MIN(master_instance.replication_depth) AS replication_depth,
			GROUP_CONCAT(concat(replica_instance.Hostname, ':', replica_instance.Port)) as slave_hosts,
			MIN(
				master_instance.slave_sql_running = 1
				AND master_instance.slave_io_running = 0
				AND (
					master_instance.last_io_error like '%%error %%connecting to master%%'
					OR master_instance.last_io_error like '%%error %%connecting to source%%'
				)
			) AS is_failing_to_connect_to_master,
			MIN(
				master_downtime.downtime_active is not null
				and ifnull(master_downtime.end_timestamp, now()) > now()
			) AS is_downtimed,
			MIN(IFNULL(master_downtime.end_timestamp, '')) AS downtime_end_timestamp,
			MIN(IFNULL(unix_timestamp() - unix_timestamp(master_downtime.end_timestamp), 0)) AS downtime_remaining_seconds,
			MIN(master_instance.binlog_server) AS is_binlog_server,
			MIN(master_instance.pseudo_gtid) AS is_pseudo_gtid,
			MIN(master_instance.supports_oracle_gtid) AS supports_oracle_gtid,
			MIN(master_instance.semi_sync_master_enabled) AS semi_sync_master_enabled,
			MIN(master_instance.semi_sync_master_wait_for_slave_count) AS semi_sync_master_wait_for_slave_count,
			MIN(master_instance.semi_sync_master_clients) AS semi_sync_master_clients,
			MIN(master_instance.semi_sync_master_status) AS semi_sync_master_status,
			SUM(replica_instance.is_co_master) AS count_co_master_replicas,
			SUM(replica_instance.oracle_gtid) AS count_oracle_gtid_replicas,
			IFNULL(SUM(
				replica_instance.last_checked <= replica_instance.last_seen
				AND replica_instance.oracle_gtid != 0
			), 0) AS count_valid_oracle_gtid_replicas,
			SUM(replica_instance.binlog_server) AS count_binlog_server_replicas,
			IFNULL(SUM(
				replica_instance.last_checked <= replica_instance.last_seen
				AND replica_instance.binlog_server != 0
			), 0) AS count_valid_binlog_server_replicas,
			SUM(replica_instance.semi_sync_replica_enabled) AS count_semi_sync_replicas,
			IFNULL(SUM(
				replica_instance.last_checked <= replica_instance.last_seen
				AND replica_instance.semi_sync_replica_enabled != 0
			), 0) AS count_valid_semi_sync_replicas,
			MIN(master_instance.mariadb_gtid) AS is_mariadb_gtid,
			SUM(replica_instance.mariadb_gtid) AS count_mariadb_gtid_replicas,
			IFNULL(SUM(
				replica_instance.last_checked <= replica_instance.last_seen
				AND replica_instance.mariadb_gtid != 0
			), 0) AS count_valid_mariadb_gtid_replicas,
			IFNULL(SUM(
				replica_instance.log_bin
				AND replica_instance.log_slave_updates
			), 0) AS count_logging_replicas,
			IFNULL(SUM(
				replica_instance.log_bin
				AND replica_instance.log_slave_updates
				AND replica_instance.binlog_format = 'STATEMENT'
			), 0) AS count_statement_based_logging_replicas,
			IFNULL(SUM(
				replica_instance.log_bin
				AND replica_instance.log_slave_updates
				AND replica_instance.binlog_format = 'MIXED'
			), 0) AS count_mixed_based_logging_replicas,
			IFNULL(SUM(
				replica_instance.log_bin
				AND replica_instance.log_slave_updates
				AND replica_instance.binlog_format = 'ROW'
			), 0) AS count_row_based_logging_replicas,
			IFNULL(SUM(replica_instance.sql_delay > 0), 0) AS count_delayed_replicas,
			IFNULL(SUM(replica_instance.slave_lag_seconds > ?), 0) AS count_lagging_replicas,
			IFNULL(MIN(replica_instance.gtid_mode), '') AS min_replica_gtid_mode,
			IFNULL(MAX(replica_instance.gtid_mode), '') AS max_replica_gtid_mode,
			IFNULL(MAX(
				case when replica_downtime.downtime_active is not null
				and ifnull(replica_downtime.end_timestamp, now()) > now()
				then '' else replica_instance.gtid_errant end
			), '') AS max_replica_gtid_errant,
			IFNULL(SUM(
				replica_downtime.downtime_active is not null
				and ifnull(replica_downtime.end_timestamp, now()) > now()
			), 0) AS count_downtimed_replicas,
			COUNT(DISTINCT case when
				replica_instance.log_bin AND replica_instance.log_slave_updates
				then replica_instance.major_version else NULL end
			) AS count_distinct_logging_major_versions
		FROM database_instance master_instance
		LEFT JOIN hostname_resolve ON master_instance.hostname = hostname_resolve.hostname
		LEFT JOIN database_instance replica_instance ON (
			COALESCE(hostname_resolve.resolved_hostname, master_instance.hostname) = replica_instance.master_host
			AND master_instance.port = replica_instance.master_port
		)
		LEFT JOIN database_instance_maintenance ON (
			master_instance.hostname = database_instance_maintenance.hostname
			AND master_instance.port = database_instance_maintenance.port
			AND database_instance_maintenance.maintenance_active = 1
		)
		LEFT JOIN database_instance_stale_binlog_coordinates ON (
			master_instance.hostname = database_instance_stale_binlog_coordinates.hostname
			AND master_instance.port = database_instance_stale_binlog_coordinates.port
		)
		LEFT JOIN database_instance_downtime as master_downtime ON (
			master_instance.hostname = master_downtime.hostname
			AND master_instance.port = master_downtime.port
			AND master_downtime.downtime_active = 1
		)
		LEFT JOIN database_instance_downtime as replica_downtime ON (
			replica_instance.hostname = replica_downtime.hostname
			AND replica_instance.port = replica_downtime.port
			AND replica_downtime.downtime_active = 1
		)
		LEFT JOIN cluster_alias ON cluster_alias.cluster_name = master_instance.cluster_name
		LEFT JOIN cluster_domain_name ON cluster_domain_name.cluster_name = master_instance.cluster_name
		WHERE
			database_instance_maintenance.id IS NULL
			AND ? IN ('', master_instance.cluster_name)
		GROUP BY master_instance.hostname, master_instance.port
		%s
		ORDER BY is_master DESC, is_cluster_master DESC, count_replicas DESC
	`, reductionClause)
	rows, err := database.QueryOrchestratorRows[modeldo.ReplicationAnalysis](ctx, query, args...)
	result := make([]modeldomain.ReplicationAnalysisRecord, 0, len(rows))
	for _, row := range rows {
		result = append(result, modeldomain.ReplicationAnalysisRecord(row))
	}
	return result, err
}

// WriteInstanceAnalysis records a changelog entry only when an existing last
// analysis row changed. The initial insert keeps the historical behavior and
// does not create a changelog row.
func WriteInstanceAnalysis(ctx context.Context, hostname string, port int, analysis string) (bool, error) {
	result, err := database.ExecOrchestratorContext(ctx, `
		update database_instance_last_analysis set
			analysis = ?,
			analysis_timestamp = now()
		where hostname = ? and port = ? and analysis != ?
	`, analysis, hostname, port, analysis)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	changed := rows > 0
	if !changed {
		_, err = database.ExecOrchestratorContext(ctx, `
			insert ignore into database_instance_last_analysis (
				hostname, port, analysis_timestamp, analysis
			) values (?, ?, now(), ?)
		`, hostname, port, analysis)
		return false, err
	}
	_, err = database.ExecOrchestratorContext(ctx, `
		insert into database_instance_analysis_changelog (
			hostname, port, analysis_timestamp, analysis
		) values (?, ?, now(), ?)
	`, hostname, port, analysis)
	return true, err
}

func ExpireInstanceAnalysisChangelog(ctx context.Context, unseenForgetHours uint) error {
	_, err := database.ExecOrchestratorContext(ctx, `
		delete from database_instance_analysis_changelog
		where analysis_timestamp < now() - interval ? hour
	`, unseenForgetHours)
	return err
}

func ReadReplicationAnalysisChangelogRows(ctx context.Context) ([]modeldomain.AnalysisChangelogRecord, error) {
	rows, err := database.QueryOrchestratorRows[modeldo.AnalysisChangelog](ctx, `
		select hostname, port, analysis_timestamp, analysis
		from database_instance_analysis_changelog
		order by hostname, port, changelog_id
	`)
	result := make([]modeldomain.AnalysisChangelogRecord, 0, len(rows))
	for _, row := range rows {
		result = append(result, modeldomain.AnalysisChangelogRecord(row))
	}
	return result, err
}

func ReadPeerAnalysisRows(ctx context.Context) ([]modeldomain.PeerAnalysisRecord, error) {
	rows, err := database.QueryOrchestratorRows[modeldo.PeerAnalysis](ctx, `
		select hostname, port, analysis
		from database_instance_peer_analysis
		order by peer, hostname, port
	`)
	result := make([]modeldomain.PeerAnalysisRecord, 0, len(rows))
	for _, row := range rows {
		result = append(result, modeldomain.PeerAnalysisRecord(row))
	}
	return result, err
}
