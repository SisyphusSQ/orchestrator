package do

import "database/sql"

// BackendInstance is the persistence projection of database_instance and its
// joined metadata tables. The metadata repository maps it to a domain record.
type BackendInstance struct {
	Hostname                          string         `gorm:"column:hostname"`
	Port                              int            `gorm:"column:port"`
	Uptime                            uint           `gorm:"column:uptime"`
	ServerID                          uint           `gorm:"column:server_id"`
	ServerUUID                        string         `gorm:"column:server_uuid"`
	Version                           string         `gorm:"column:version"`
	MajorVersion                      string         `gorm:"column:major_version"`
	VersionComment                    string         `gorm:"column:version_comment"`
	BinlogServer                      bool           `gorm:"column:binlog_server"`
	ReadOnly                          bool           `gorm:"column:read_only"`
	BinlogFormat                      string         `gorm:"column:binlog_format"`
	BinlogRowImage                    string         `gorm:"column:binlog_row_image"`
	LogBin                            bool           `gorm:"column:log_bin"`
	LogSlaveUpdates                   bool           `gorm:"column:log_slave_updates"`
	MasterHost                        string         `gorm:"column:master_host"`
	MasterPort                        int            `gorm:"column:master_port"`
	SlaveSQLRunning                   bool           `gorm:"column:slave_sql_running"`
	SlaveIORunning                    bool           `gorm:"column:slave_io_running"`
	ReplicationSQLThreadState         int            `gorm:"column:replication_sql_thread_state"`
	ReplicationIOThreadState          int            `gorm:"column:replication_io_thread_state"`
	HasReplicationFilters             bool           `gorm:"column:has_replication_filters"`
	SupportsOracleGTID                bool           `gorm:"column:supports_oracle_gtid"`
	OracleGTID                        bool           `gorm:"column:oracle_gtid"`
	MasterUUID                        string         `gorm:"column:master_uuid"`
	AncestryUUID                      string         `gorm:"column:ancestry_uuid"`
	ExecutedGTIDSet                   string         `gorm:"column:executed_gtid_set"`
	GTIDMode                          string         `gorm:"column:gtid_mode"`
	GTIDPurged                        string         `gorm:"column:gtid_purged"`
	GTIDErrant                        string         `gorm:"column:gtid_errant"`
	MariaDBGTID                       bool           `gorm:"column:mariadb_gtid"`
	PseudoGTID                        bool           `gorm:"column:pseudo_gtid"`
	BinaryLogFile                     string         `gorm:"column:binary_log_file"`
	BinaryLogPos                      int64          `gorm:"column:binary_log_pos"`
	MasterLogFile                     string         `gorm:"column:master_log_file"`
	ReadMasterLogPos                  int64          `gorm:"column:read_master_log_pos"`
	RelayMasterLogFile                string         `gorm:"column:relay_master_log_file"`
	ExecMasterLogPos                  int64          `gorm:"column:exec_master_log_pos"`
	RelayLogFile                      string         `gorm:"column:relay_log_file"`
	RelayLogPos                       int64          `gorm:"column:relay_log_pos"`
	LastSQLError                      string         `gorm:"column:last_sql_error"`
	LastIOError                       string         `gorm:"column:last_io_error"`
	SecondsBehindMaster               sql.NullInt64  `gorm:"column:seconds_behind_master"`
	SlaveLagSeconds                   sql.NullInt64  `gorm:"column:slave_lag_seconds"`
	SQLDelay                          uint           `gorm:"column:sql_delay"`
	SlaveHosts                        string         `gorm:"column:slave_hosts"`
	NumSlaveHosts                     int            `gorm:"column:num_slave_hosts"`
	ClusterName                       string         `gorm:"column:cluster_name"`
	SuggestedClusterAlias             string         `gorm:"column:suggested_cluster_alias"`
	DataCenter                        string         `gorm:"column:data_center"`
	Region                            string         `gorm:"column:region"`
	PhysicalEnvironment               string         `gorm:"column:physical_environment"`
	SemiSyncEnforced                  uint           `gorm:"column:semi_sync_enforced"`
	SemiSyncAvailable                 bool           `gorm:"column:semi_sync_available"`
	SemiSyncMasterEnabled             bool           `gorm:"column:semi_sync_master_enabled"`
	SemiSyncMasterTimeout             uint64         `gorm:"column:semi_sync_master_timeout"`
	SemiSyncMasterWaitForReplicaCount uint           `gorm:"column:semi_sync_master_wait_for_slave_count"`
	SemiSyncReplicaEnabled            bool           `gorm:"column:semi_sync_replica_enabled"`
	SemiSyncMasterStatus              bool           `gorm:"column:semi_sync_master_status"`
	SemiSyncMasterClients             uint           `gorm:"column:semi_sync_master_clients"`
	SemiSyncReplicaStatus             bool           `gorm:"column:semi_sync_replica_status"`
	ReplicationDepth                  uint           `gorm:"column:replication_depth"`
	CoMaster                          bool           `gorm:"column:is_co_master"`
	ReplicationCredentialsAvailable   bool           `gorm:"column:replication_credentials_available"`
	HasReplicationCredentials         bool           `gorm:"column:has_replication_credentials"`
	SecondsSinceLastChecked           sql.NullInt64  `gorm:"column:seconds_since_last_checked"`
	LastSeen                          sql.NullString `gorm:"column:last_seen"`
	LastCheckValid                    bool           `gorm:"column:is_last_check_valid"`
	LastCheckPartialSuccess           bool           `gorm:"column:last_check_partial_success"`
	SecondsSinceLastSeen              sql.NullInt64  `gorm:"column:seconds_since_last_seen"`
	Candidate                         bool           `gorm:"column:is_candidate"`
	PromotionRule                     string         `gorm:"column:promotion_rule"`
	Downtimed                         bool           `gorm:"column:is_downtimed"`
	DowntimeReason                    string         `gorm:"column:downtime_reason"`
	DowntimeOwner                     string         `gorm:"column:downtime_owner"`
	DowntimeEndTimestamp              string         `gorm:"column:downtime_end_timestamp"`
	ElapsedDowntimeSeconds            int            `gorm:"column:elapsed_downtime_seconds"`
	UnresolvedHostname                string         `gorm:"column:unresolved_hostname"`
	AllowTLS                          bool           `gorm:"column:allow_tls"`
	InstanceAlias                     string         `gorm:"column:instance_alias"`
	LastDiscoveryLatency              int64          `gorm:"column:last_discovery_latency"`
	ReplicationGroupName              string         `gorm:"column:replication_group_name"`
	ReplicationGroupSinglePrimary     bool           `gorm:"column:replication_group_is_single_primary_mode"`
	ReplicationGroupMemberState       string         `gorm:"column:replication_group_member_state"`
	ReplicationGroupMemberRole        string         `gorm:"column:replication_group_member_role"`
	ReplicationGroupPrimaryHost       string         `gorm:"column:replication_group_primary_host"`
	ReplicationGroupPrimaryPort       int            `gorm:"column:replication_group_primary_port"`
	ReplicationGroupMembers           string         `gorm:"column:replication_group_members"`
}

type ClusterAliasOverride struct {
	Alias string `gorm:"column:alias"`
}

type ReplicationGroupPrimary struct {
	Hostname string `gorm:"column:replication_group_primary_host"`
	Port     int    `gorm:"column:replication_group_primary_port"`
}

type InstanceClusterAttributes struct {
	ClusterName           string `gorm:"column:cluster_name"`
	SuggestedClusterAlias string `gorm:"column:suggested_cluster_alias"`
	ReplicationDepth      uint   `gorm:"column:replication_depth"`
	MasterHost            string `gorm:"column:master_host"`
	MasterPort            int    `gorm:"column:master_port"`
	AncestryUUID          string `gorm:"column:ancestry_uuid"`
	ExecutedGTIDSet       string `gorm:"column:executed_gtid_set"`
}

type PromotionRule struct {
	PromotionRule string `gorm:"column:promotion_rule"`
}

type MasterKey struct {
	Hostname string `gorm:"column:master_host"`
	Port     int    `gorm:"column:master_port"`
}

type MasterHostnameResolve struct {
	Hostname         string `gorm:"column:master_host"`
	ResolvedHostname string `gorm:"column:resolved_hostname"`
}

type SnapshotCount struct {
	Hostname string `gorm:"column:hostname"`
	Count    int    `gorm:"column:count_mysql_snapshots"`
}

type InstanceClusterName struct {
	ClusterName string `gorm:"column:cluster_name"`
}

type ClusterInfo struct {
	ClusterName    string `gorm:"column:cluster_name"`
	CountInstances uint   `gorm:"column:count_instances"`
	Alias          string `gorm:"column:alias"`
	DomainName     string `gorm:"column:domain_name"`
}

type MinimalInstance struct {
	Hostname    string `gorm:"column:hostname"`
	Port        int    `gorm:"column:port"`
	MasterHost  string `gorm:"column:master_host"`
	MasterPort  int    `gorm:"column:master_port"`
	ClusterName string `gorm:"column:cluster_name"`
}

type HistoryInstance struct {
	Hostname    string `gorm:"column:hostname"`
	Port        int    `gorm:"column:port"`
	MasterHost  string `gorm:"column:master_host"`
	MasterPort  int    `gorm:"column:master_port"`
	ClusterName string `gorm:"column:cluster_name"`
}

type InstanceCoordinates struct {
	BinaryLogFile string `gorm:"column:binary_log_file"`
	BinaryLogPos  int64  `gorm:"column:binary_log_pos"`
	RelayLogFile  string `gorm:"column:relay_log_file"`
	RelayLogPos   int64  `gorm:"column:relay_log_pos"`
}

type RelayCoordinates struct {
	RelayLogFile string `gorm:"column:relay_log_file"`
	RelayLogPos  int64  `gorm:"column:relay_log_pos"`
}

type InjectedPseudoGTID struct {
	Injected bool `gorm:"column:is_injected"`
}
