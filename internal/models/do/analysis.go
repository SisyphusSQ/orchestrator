package do

import "database/sql"

// ReplicationAnalysis is the typed persistence projection used by the
// replication-analysis query. It deliberately contains no business rules.
type ReplicationAnalysis struct {
	Hostname                           string         `gorm:"column:hostname"`
	Port                               int            `gorm:"column:port"`
	ReadOnly                           uint           `gorm:"column:read_only"`
	DataCenter                         string         `gorm:"column:data_center"`
	Region                             string         `gorm:"column:region"`
	PhysicalEnvironment                string         `gorm:"column:physical_environment"`
	MasterHost                         string         `gorm:"column:master_host"`
	MasterPort                         int            `gorm:"column:master_port"`
	ClusterName                        string         `gorm:"column:cluster_name"`
	BinaryLogFile                      string         `gorm:"column:binary_log_file"`
	BinaryLogPos                       int64          `gorm:"column:binary_log_pos"`
	StaleBinlogCoordinates             bool           `gorm:"column:is_stale_binlog_coordinates"`
	ClusterAlias                       string         `gorm:"column:cluster_alias"`
	ClusterDomain                      string         `gorm:"column:cluster_domain"`
	LastCheckValid                     bool           `gorm:"column:is_last_check_valid"`
	LastCheckPartialSuccess            bool           `gorm:"column:last_check_partial_success"`
	Master                             bool           `gorm:"column:is_master"`
	ReplicationGroupMember             bool           `gorm:"column:is_replication_group_member"`
	CoMaster                           bool           `gorm:"column:is_co_master"`
	GTIDMode                           string         `gorm:"column:gtid_mode"`
	CountReplicas                      uint           `gorm:"column:count_replicas"`
	CountValidReplicas                 uint           `gorm:"column:count_valid_replicas"`
	CountValidReplicatingReplicas      uint           `gorm:"column:count_valid_replicating_replicas"`
	CountReplicasFailingToConnect      uint           `gorm:"column:count_replicas_failing_to_connect_to_master"`
	ReplicationDepth                   uint           `gorm:"column:replication_depth"`
	SlaveHosts                         sql.NullString `gorm:"column:slave_hosts"`
	FailingToConnectToMaster           bool           `gorm:"column:is_failing_to_connect_to_master"`
	Downtimed                          bool           `gorm:"column:is_downtimed"`
	DowntimeEndTimestamp               string         `gorm:"column:downtime_end_timestamp"`
	DowntimeRemainingSeconds           int            `gorm:"column:downtime_remaining_seconds"`
	BinlogServer                       bool           `gorm:"column:is_binlog_server"`
	PseudoGTID                         bool           `gorm:"column:is_pseudo_gtid"`
	SemiSyncMasterEnabled              bool           `gorm:"column:semi_sync_master_enabled"`
	SemiSyncMasterWaitForReplicaCount  uint           `gorm:"column:semi_sync_master_wait_for_slave_count"`
	SemiSyncMasterClients              uint           `gorm:"column:semi_sync_master_clients"`
	SemiSyncMasterStatus               bool           `gorm:"column:semi_sync_master_status"`
	CountCoMasterReplicas              sql.NullInt64  `gorm:"column:count_co_master_replicas"`
	CountValidOracleGTIDReplicas       uint           `gorm:"column:count_valid_oracle_gtid_replicas"`
	CountValidBinlogServerReplicas     uint           `gorm:"column:count_valid_binlog_server_replicas"`
	CountSemiSyncReplicas              sql.NullInt64  `gorm:"column:count_semi_sync_replicas"`
	CountValidMariaDBGTIDReplicas      uint           `gorm:"column:count_valid_mariadb_gtid_replicas"`
	CountLoggingReplicas               uint           `gorm:"column:count_logging_replicas"`
	CountStatementBasedLoggingReplicas uint           `gorm:"column:count_statement_based_logging_replicas"`
	CountMixedBasedLoggingReplicas     uint           `gorm:"column:count_mixed_based_logging_replicas"`
	CountRowBasedLoggingReplicas       uint           `gorm:"column:count_row_based_logging_replicas"`
	CountDelayedReplicas               uint           `gorm:"column:count_delayed_replicas"`
	CountLaggingReplicas               uint           `gorm:"column:count_lagging_replicas"`
	MinReplicaGTIDMode                 string         `gorm:"column:min_replica_gtid_mode"`
	MaxReplicaGTIDMode                 string         `gorm:"column:max_replica_gtid_mode"`
	MaxReplicaGTIDErrant               string         `gorm:"column:max_replica_gtid_errant"`
	CountDowntimedReplicas             uint           `gorm:"column:count_downtimed_replicas"`
	CountDistinctLoggingMajorVersions  uint           `gorm:"column:count_distinct_logging_major_versions"`
}

type AnalysisChangelog struct {
	Hostname  string `gorm:"column:hostname"`
	Port      int    `gorm:"column:port"`
	Timestamp string `gorm:"column:analysis_timestamp"`
	Analysis  string `gorm:"column:analysis"`
}

type PeerAnalysis struct {
	Hostname string `gorm:"column:hostname"`
	Port     int    `gorm:"column:port"`
	Analysis string `gorm:"column:analysis"`
}
