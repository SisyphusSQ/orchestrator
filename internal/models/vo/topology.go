package vo

import (
	"time"

	modeldomain "github.com/openark/orchestrator/internal/models/domain"
)

// InstanceKey is the stable HTTP representation of a topology address.
type InstanceKey struct {
	Hostname string
	Port     int
}

// BinlogCoordinates is the stable HTTP representation of binary or relay-log coordinates.
type BinlogCoordinates struct {
	LogFile string
	LogPos  int64
	Type    int
}

// ClusterInfo is the stable HTTP summary of a cluster.
type ClusterInfo struct {
	ClusterName                            string
	ClusterAlias                           string
	ClusterDomain                          string
	CountInstances                         uint
	HeuristicLag                           int64
	HasAutomatedMasterRecovery             bool
	HasAutomatedIntermediateMasterRecovery bool
}

// Instance is the stable HTTP representation of a managed topology instance.
// Legacy Slave fields remain present and are populated from their Replica peers.
type Instance struct {
	Key                          InstanceKey
	InstanceAlias                string
	Uptime                       uint
	ServerID                     uint
	ServerUUID                   string
	Version                      string
	VersionComment               string
	FlavorName                   string
	ReadOnly                     bool
	Binlog_format                string
	BinlogRowImage               string
	LogBinEnabled                bool
	LogSlaveUpdatesEnabled       bool
	LogReplicationUpdatesEnabled bool
	SelfBinlogCoordinates        BinlogCoordinates
	MasterKey                    InstanceKey
	MasterUUID                   string
	AncestryUUID                 string
	IsDetachedMaster             bool

	Slave_SQL_Running          bool
	ReplicationSQLThreadRuning bool
	Slave_IO_Running           bool
	ReplicationIOThreadRuning  bool
	ReplicationSQLThreadState  int
	ReplicationIOThreadState   int

	HasReplicationFilters bool
	GTIDMode              string
	SupportsOracleGTID    bool
	UsingOracleGTID       bool
	UsingMariaDBGTID      bool
	UsingPseudoGTID       bool
	ReadBinlogCoordinates BinlogCoordinates
	ExecBinlogCoordinates BinlogCoordinates
	IsDetached            bool
	RelaylogCoordinates   BinlogCoordinates
	LastSQLError          string
	LastIOError           string
	SecondsBehindMaster   modeldomain.NullInt64
	SQLDelay              uint
	ExecutedGtidSet       string
	GtidPurged            string
	GtidErrant            string

	SlaveLagSeconds                   modeldomain.NullInt64
	ReplicationLagSeconds             modeldomain.NullInt64
	SlaveHosts                        []InstanceKey
	Replicas                          []InstanceKey
	ClusterName                       string
	SuggestedClusterAlias             string
	DataCenter                        string
	Region                            string
	PhysicalEnvironment               string
	ReplicationDepth                  uint
	IsCoMaster                        bool
	HasReplicationCredentials         bool
	ReplicationCredentialsAvailable   bool
	SemiSyncAvailable                 bool
	SemiSyncPriority                  uint
	SemiSyncMasterPluginNewVersion    bool
	SemiSyncReplicaPluginNewVersion   bool
	SemiSyncMasterEnabled             bool
	SemiSyncReplicaEnabled            bool
	SemiSyncMasterTimeout             uint64
	SemiSyncMasterWaitForReplicaCount uint
	SemiSyncMasterStatus              bool
	SemiSyncMasterClients             uint
	SemiSyncReplicaStatus             bool

	LastSeenTimestamp    string
	IsLastCheckValid     bool
	IsUpToDate           bool
	IsRecentlyChecked    bool
	SecondsSinceLastSeen modeldomain.NullInt64
	CountMySQLSnapshots  int

	IsCandidate          bool
	PromotionRule        string
	IsDowntimed          bool
	DowntimeReason       string
	DowntimeOwner        string
	DowntimeEndTimestamp string
	ElapsedDowntime      time.Duration
	UnresolvedHostname   string
	AllowTLS             bool

	ReplicationSSLCAFile           string
	ReplicationSSLCAPath           string
	ReplicationSSLCert             string
	ReplicationSSLCipher           string
	ReplicationSSLCRLFile          string
	ReplicationSSLCRLPath          string
	ReplicationSSLKey              string
	ReplicationSSLVerifyServerCert modeldomain.NullBool
	ReplicationTLSVersion          string
	ReplicationTLSCiphersuites     string
	ReplicationSourcePublicKeyPath string
	ReplicationGetSourcePublicKey  modeldomain.NullBool

	Problems             []string
	LastDiscoveryLatency time.Duration

	ReplicationGroupName               string
	ReplicationGroupIsSinglePrimary    bool
	ReplicationGroupMemberState        string
	ReplicationGroupMemberRole         string
	ReplicationGroupMembers            []InstanceKey
	ReplicationGroupPrimaryInstanceKey InstanceKey

	QSP struct{}
}

// ReplicationAnalysis is the stable HTTP representation of one analysis result.
type ReplicationAnalysis struct {
	AnalyzedInstanceKey                       InstanceKey
	AnalyzedInstanceMasterKey                 InstanceKey
	ClusterDetails                            ClusterInfo
	AnalyzedInstanceDataCenter                string
	AnalyzedInstanceRegion                    string
	AnalyzedInstancePhysicalEnvironment       string
	AnalyzedInstanceBinlogCoordinates         BinlogCoordinates
	IsMaster                                  bool
	IsReplicationGroupMember                  bool
	IsCoMaster                                bool
	LastCheckValid                            bool
	LastCheckPartialSuccess                   bool
	CountReplicas                             uint
	CountValidReplicas                        uint
	CountValidReplicatingReplicas             uint
	CountReplicasFailingToConnectToMaster     uint
	CountDowntimedReplicas                    uint
	ReplicationDepth                          uint
	Replicas                                  []InstanceKey
	SlaveHosts                                []InstanceKey
	IsFailingToConnectToMaster                bool
	Analysis                                  string
	Description                               string
	StructureAnalysis                         []string
	IsDowntimed                               bool
	IsReplicasDowntimed                       bool
	DowntimeEndTimestamp                      string
	DowntimeRemainingSeconds                  int
	IsBinlogServer                            bool
	PseudoGTIDImmediateTopology               bool
	OracleGTIDImmediateTopology               bool
	MariaDBGTIDImmediateTopology              bool
	BinlogServerImmediateTopology             bool
	SemiSyncMasterEnabled                     bool
	SemiSyncMasterStatus                      bool
	SemiSyncMasterWaitForReplicaCount         uint
	SemiSyncMasterClients                     uint
	CountSemiSyncReplicasEnabled              uint
	CountLoggingReplicas                      uint
	CountStatementBasedLoggingReplicas        uint
	CountMixedBasedLoggingReplicas            uint
	CountRowBasedLoggingReplicas              uint
	CountDistinctMajorVersionsLoggingReplicas uint
	CountDelayedReplicas                      uint
	CountLaggingReplicas                      uint
	IsActionableRecovery                      bool
	ProcessingNodeHostname                    string
	ProcessingNodeToken                       string
	CountAdditionalAgreeingNodes              int
	StartActivePeriod                         string
	SkippableDueToDowntime                    bool
	GTIDMode                                  string
	MinReplicaGTIDMode                        string
	MaxReplicaGTIDMode                        string
	MaxReplicaGTIDErrant                      string
	CommandHint                               string
	IsReadOnly                                bool
}

// TopologyRecovery is the stable HTTP representation of a recovery audit row.
type TopologyRecovery struct {
	Id                         int64
	UID                        string
	AnalysisEntry              ReplicationAnalysis
	SuccessorKey               *InstanceKey
	SuccessorAlias             string
	SuccessorBinlogCoordinates *BinlogCoordinates
	IsActive                   bool
	IsSuccessful               bool
	LostReplicas               []InstanceKey
	ParticipatingInstanceKeys  []InstanceKey
	AllErrors                  []string
	RecoveryStartTimestamp     string
	RecoveryEndTimestamp       string
	ProcessingNodeHostname     string
	ProcessingNodeToken        string
	PolicyRevision             int64
	HookAssignmentRevision     int64
	Acknowledged               bool
	AcknowledgedAt             string
	AcknowledgedBy             string
	AcknowledgedComment        string
	LastDetectionId            int64
	RelatedRecoveryId          int64
	Type                       string
	RecoveryType               string
}

type BlockedTopologyRecovery struct {
	FailedInstanceKey    InstanceKey
	ClusterName          string
	Analysis             string
	LastBlockedTimestamp string
	BlockingRecoveryId   int64
}

type TopologyRecoveryStep struct {
	Id          int64
	RecoveryUID string
	AuditAt     string
	Message     string
}
