package domain

import "database/sql"

// ReplicationAnalysisRecord is the repository-neutral result consumed by the
// replication analysis engine.
type ReplicationAnalysisRecord struct {
	Hostname                           string
	Port                               int
	ReadOnly                           uint
	DataCenter                         string
	Region                             string
	PhysicalEnvironment                string
	MasterHost                         string
	MasterPort                         int
	ClusterName                        string
	BinaryLogFile                      string
	BinaryLogPos                       int64
	StaleBinlogCoordinates             bool
	ClusterAlias                       string
	ClusterDomain                      string
	LastCheckValid                     bool
	LastCheckPartialSuccess            bool
	Master                             bool
	ReplicationGroupMember             bool
	CoMaster                           bool
	GTIDMode                           string
	CountReplicas                      uint
	CountValidReplicas                 uint
	CountValidReplicatingReplicas      uint
	CountReplicasFailingToConnect      uint
	ReplicationDepth                   uint
	SlaveHosts                         sql.NullString
	FailingToConnectToMaster           bool
	Downtimed                          bool
	DowntimeEndTimestamp               string
	DowntimeRemainingSeconds           int
	BinlogServer                       bool
	PseudoGTID                         bool
	SemiSyncMasterEnabled              bool
	SemiSyncMasterWaitForReplicaCount  uint
	SemiSyncMasterClients              uint
	SemiSyncMasterStatus               bool
	CountCoMasterReplicas              sql.NullInt64
	CountValidOracleGTIDReplicas       uint
	CountValidBinlogServerReplicas     uint
	CountSemiSyncReplicas              sql.NullInt64
	CountValidMariaDBGTIDReplicas      uint
	CountLoggingReplicas               uint
	CountStatementBasedLoggingReplicas uint
	CountMixedBasedLoggingReplicas     uint
	CountRowBasedLoggingReplicas       uint
	CountDelayedReplicas               uint
	CountLaggingReplicas               uint
	MinReplicaGTIDMode                 string
	MaxReplicaGTIDMode                 string
	MaxReplicaGTIDErrant               string
	CountDowntimedReplicas             uint
	CountDistinctLoggingMajorVersions  uint
}

type AnalysisChangelogRecord struct {
	Hostname  string
	Port      int
	Timestamp string
	Analysis  string
}

type PeerAnalysisRecord struct {
	Hostname string
	Port     int
	Analysis string
}

// NonNegativeUint converts nullable database aggregates to their domain value.
func NonNegativeUint(value sql.NullInt64) uint {
	if !value.Valid || value.Int64 <= 0 {
		return 0
	}
	return uint(value.Int64)
}
