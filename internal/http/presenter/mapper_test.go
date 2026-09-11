package presenter

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/openark/orchestrator/internal/http/contract"
	instanalysis "github.com/openark/orchestrator/internal/inst/analysis"
	instcluster "github.com/openark/orchestrator/internal/inst/cluster"
	instmodel "github.com/openark/orchestrator/internal/inst/instance"
	logicrecovery "github.com/openark/orchestrator/internal/logic/recovery"
	"github.com/openark/orchestrator/internal/models/domain"
	"github.com/openark/orchestrator/internal/models/vo"
)

func assertSameJSON(t *testing.T, legacy, mapped any) {
	t.Helper()
	legacyJSON, err := json.Marshal(legacy)
	if err != nil {
		t.Fatalf("marshal legacy model: %v", err)
	}
	mappedJSON, err := json.Marshal(mapped)
	if err != nil {
		t.Fatalf("marshal mapped model: %v", err)
	}
	if !bytes.Equal(mappedJSON, legacyJSON) {
		t.Fatalf("mapped JSON changed\nlegacy: %s\nmapped: %s", legacyJSON, mappedJSON)
	}
}

func assertEquivalentJSON(t *testing.T, legacy, mapped any) {
	t.Helper()
	legacyJSON, err := json.Marshal(legacy)
	if err != nil {
		t.Fatalf("marshal legacy model: %v", err)
	}
	mappedJSON, err := json.Marshal(mapped)
	if err != nil {
		t.Fatalf("marshal mapped model: %v", err)
	}
	var legacyValue, mappedValue any
	if err := json.Unmarshal(legacyJSON, &legacyValue); err != nil {
		t.Fatalf("decode legacy JSON: %v", err)
	}
	if err := json.Unmarshal(mappedJSON, &mappedValue); err != nil {
		t.Fatalf("decode mapped JSON: %v", err)
	}
	if !reflect.DeepEqual(mappedValue, legacyValue) {
		t.Fatalf("mapped JSON changed\nlegacy: %s\nmapped: %s", legacyJSON, mappedJSON)
	}
}

func TestInstanceVOIsConcreteAndPreservesLegacyJSON(t *testing.T) {
	instance := instmodel.NewInstance()
	instance.Key = instmodel.InstanceKey{Hostname: "db.example", Port: 3307}
	instance.InstanceAlias = "writer"
	instance.Uptime = 12
	instance.ServerID = 44
	instance.ServerUUID = "server-uuid"
	instance.Version = "8.0.42"
	instance.VersionComment = "MySQL Community"
	instance.FlavorName = "MySQL"
	instance.ReadOnly = true
	instance.Binlog_format = "ROW"
	instance.BinlogRowImage = "FULL"
	instance.LogBinEnabled = true
	instance.LogSlaveUpdatesEnabled = false
	instance.LogReplicationUpdatesEnabled = true
	instance.SelfBinlogCoordinates = instmodel.BinlogCoordinates{LogFile: "mysql-bin.000010", LogPos: 120, Type: instmodel.BinaryLog}
	instance.MasterKey = instmodel.InstanceKey{Hostname: "primary.example", Port: 3306}
	instance.MasterUUID = "master-uuid"
	instance.AncestryUUID = "ancestry"
	instance.IsDetachedMaster = true
	instance.Slave_SQL_Running = false
	instance.ReplicationSQLThreadRuning = true
	instance.Slave_IO_Running = true
	instance.ReplicationIOThreadRuning = false
	instance.ReplicationSQLThreadState = instmodel.ReplicationThreadState(1)
	instance.ReplicationIOThreadState = instmodel.ReplicationThreadState(2)
	instance.HasReplicationFilters = true
	instance.GTIDMode = "ON"
	instance.SupportsOracleGTID = true
	instance.UsingOracleGTID = true
	instance.UsingMariaDBGTID = true
	instance.UsingPseudoGTID = true
	instance.ReadBinlogCoordinates = instmodel.BinlogCoordinates{LogFile: "mysql-bin.000009", LogPos: 90, Type: instmodel.BinaryLog}
	instance.ExecBinlogCoordinates = instmodel.BinlogCoordinates{LogFile: "mysql-bin.000009", LogPos: 80, Type: instmodel.BinaryLog}
	instance.IsDetached = true
	instance.RelaylogCoordinates = instmodel.BinlogCoordinates{LogFile: "relay.000001", LogPos: 70, Type: instmodel.RelayLog}
	instance.LastSQLError = "sql error"
	instance.LastIOError = "io error"
	instance.SecondsBehindMaster = domain.NullInt64{Int64: 4, Valid: true}
	instance.SQLDelay = 2
	instance.ExecutedGtidSet = "executed"
	instance.GtidPurged = "purged"
	instance.GtidErrant = "errant"
	instance.SlaveLagSeconds = domain.NullInt64{Int64: 99, Valid: true}
	instance.ReplicationLagSeconds = domain.NullInt64{Int64: 8, Valid: true}
	instance.SlaveHosts = *instmodel.NewInstanceKeyMap()
	instance.SlaveHosts.AddKey(instmodel.InstanceKey{Hostname: "stale.example", Port: 3310})
	instance.Replicas.AddKey(instmodel.InstanceKey{Hostname: "replica-b.example", Port: 3306})
	instance.Replicas.AddKey(instmodel.InstanceKey{Hostname: "replica-a.example", Port: 3306})
	instance.ClusterName = "cluster-a"
	instance.SuggestedClusterAlias = "payments"
	instance.DataCenter = "dc-a"
	instance.Region = "region-a"
	instance.PhysicalEnvironment = "prod"
	instance.ReplicationDepth = 2
	instance.IsCoMaster = true
	instance.HasReplicationCredentials = true
	instance.ReplicationCredentialsAvailable = true
	instance.SemiSyncAvailable = true
	instance.SemiSyncPriority = 3
	instance.SemiSyncMasterPluginNewVersion = true
	instance.SemiSyncReplicaPluginNewVersion = true
	instance.SemiSyncMasterEnabled = true
	instance.SemiSyncReplicaEnabled = true
	instance.SemiSyncMasterTimeout = 1000
	instance.SemiSyncMasterWaitForReplicaCount = 2
	instance.SemiSyncMasterStatus = true
	instance.SemiSyncMasterClients = 2
	instance.SemiSyncReplicaStatus = true
	instance.LastSeenTimestamp = "2026-09-10 12:00:00"
	instance.IsLastCheckValid = true
	instance.IsUpToDate = true
	instance.IsRecentlyChecked = true
	instance.SecondsSinceLastSeen = domain.NullInt64{Int64: 3, Valid: true}
	instance.CountMySQLSnapshots = 5
	instance.IsCandidate = true
	instance.PromotionRule = instmodel.PreferPromoteRule
	instance.IsDowntimed = true
	instance.DowntimeReason = "maintenance"
	instance.DowntimeOwner = "dba"
	instance.DowntimeEndTimestamp = "2026-09-10 13:00:00"
	instance.ElapsedDowntime = 3 * time.Second
	instance.UnresolvedHostname = "db"
	instance.AllowTLS = true
	instance.ReplicationSSLCAFile = "ca.pem"
	instance.ReplicationSSLCAPath = "/ca"
	instance.ReplicationSSLCert = "cert.pem"
	instance.ReplicationSSLCipher = "cipher"
	instance.ReplicationSSLCRLFile = "crl.pem"
	instance.ReplicationSSLCRLPath = "/crl"
	instance.ReplicationSSLKey = "key.pem"
	instance.ReplicationSSLVerifyServerCert = domain.NullBool{Bool: true, Valid: true}
	instance.ReplicationTLSVersion = "TLSv1.3"
	instance.ReplicationTLSCiphersuites = "suite"
	instance.ReplicationSourcePublicKeyPath = "public.pem"
	instance.ReplicationGetSourcePublicKey = domain.NullBool{Bool: true, Valid: true}
	instance.Problems = []string{"replication_lag"}
	instance.LastDiscoveryLatency = 250 * time.Millisecond
	instance.ReplicationGroupName = "group-a"
	instance.ReplicationGroupIsSinglePrimary = true
	instance.ReplicationGroupMemberState = "ONLINE"
	instance.ReplicationGroupMemberRole = "SECONDARY"
	instance.ReplicationGroupMembers.AddKey(instmodel.InstanceKey{Hostname: "group-b.example", Port: 3306})
	instance.ReplicationGroupPrimaryInstanceKey = instmodel.InstanceKey{Hostname: "group-a.example", Port: 3306}

	mapped, ok := ToHTTPModel(instance).(*vo.Instance)
	if !ok {
		t.Fatalf("ToHTTPModel(*inst.Instance) type = %T; want *vo.Instance", ToHTTPModel(instance))
	}
	// Instance no longer owns the runtime query provider, but the HTTP contract
	// keeps the historical empty QSP object.
	legacyBytes, err := json.Marshal(instance)
	if err != nil {
		t.Fatalf("marshal legacy instance: %v", err)
	}
	legacyInstance := map[string]any{}
	if err := json.Unmarshal(legacyBytes, &legacyInstance); err != nil {
		t.Fatalf("decode legacy instance: %v", err)
	}
	legacyInstance["QSP"] = map[string]any{}
	assertEquivalentJSON(t, legacyInstance, mapped)
	assertEquivalentJSON(t, &contract.Response{Code: contract.OK, Message: "instance", Details: legacyInstance}, ToHTTPModel(&contract.Response{Code: contract.OK, Message: "instance", Details: instance}))
}

func TestAnalysisRecoveryAndAgentVOsPreserveJSON(t *testing.T) {
	analysis := instanalysis.ReplicationAnalysis{
		AnalyzedInstanceKey:               instmodel.InstanceKey{Hostname: "failed.example", Port: 3306},
		AnalyzedInstanceMasterKey:         instmodel.InstanceKey{Hostname: "master.example", Port: 3306},
		ClusterDetails:                    instcluster.ClusterInfo{ClusterName: "cluster-a", ClusterAlias: "payments", CountInstances: 3},
		AnalyzedInstanceDataCenter:        "dc-a",
		AnalyzedInstanceBinlogCoordinates: instmodel.BinlogCoordinates{LogFile: "mysql-bin.000100", LogPos: 500},
		IsMaster:                          true,
		LastCheckValid:                    true,
		CountReplicas:                     2,
		Analysis:                          instanalysis.DeadMaster,
		Description:                       "master is unreachable",
		StructureAnalysis:                 []instanalysis.AnalysisCode{instanalysis.NoWriteableMasterStructureWarning},
		CountLoggingReplicas:              2,
		IsActionableRecovery:              true,
		ProcessingNodeHostname:            "orc-1",
		ProcessingNodeToken:               "token",
		CountAdditionalAgreeingNodes:      1,
		StartActivePeriod:                 "2026-09-10 12:00:00",
		GTIDMode:                          "ON",
		MinReplicaGTIDMode:                "ON_PERMISSIVE",
		MaxReplicaGTIDMode:                "ON",
		CommandHint:                       instanalysis.ForceMasterFailoverCommandHint,
		SlaveHosts:                        *instmodel.NewInstanceKeyMap(),
	}
	analysis.SlaveHosts.AddKey(instmodel.InstanceKey{Hostname: "stale.example", Port: 3306})
	analysis.Replicas = *instmodel.NewInstanceKeyMap()
	analysis.Replicas.AddKey(instmodel.InstanceKey{Hostname: "replica-b.example", Port: 3306})
	analysis.Replicas.AddKey(instmodel.InstanceKey{Hostname: "replica-a.example", Port: 3306})
	assertSameJSON(t, &analysis, ToHTTPModel(&analysis))

	successor := instmodel.InstanceKey{Hostname: "successor.example", Port: 3307}
	coordinates := instmodel.BinlogCoordinates{LogFile: "mysql-bin.000101", LogPos: 600}
	recovery := &logicrecovery.TopologyRecovery{
		AnalysisEntry:             analysis,
		LostReplicas:              *instmodel.NewInstanceKeyMap(),
		ParticipatingInstanceKeys: *instmodel.NewInstanceKeyMap(),
	}
	recovery.Id = 42
	recovery.UID = "recovery-42"
	recovery.SuccessorKey = &successor
	recovery.SuccessorAlias = "new-primary"
	recovery.SuccessorBinlogCoordinates = &coordinates
	recovery.IsActive = true
	recovery.IsSuccessful = true
	recovery.LostReplicas.AddKey(instmodel.InstanceKey{Hostname: "lost.example", Port: 3306})
	recovery.ParticipatingInstanceKeys.AddKey(successor)
	recovery.AllErrors = []string{"first", "second"}
	recovery.RecoveryStartTimestamp = "2026-09-10 12:00:00"
	recovery.RecoveryEndTimestamp = "2026-09-10 12:01:00"
	recovery.ProcessingNodeHostname = "orc-1"
	recovery.ProcessingNodeToken = "token"
	recovery.PolicyRevision = 11
	recovery.HookAssignmentRevision = 12
	recovery.Acknowledged = true
	recovery.AcknowledgedAt = "2026-09-10 12:02:00"
	recovery.AcknowledgedBy = "dba"
	recovery.AcknowledgedComment = "done"
	recovery.LastDetectionId = 40
	recovery.RelatedRecoveryId = 41
	recovery.Type = logicrecovery.MasterRecovery
	recovery.RecoveryType = logicrecovery.MasterRecoveryGTID
	assertSameJSON(t, recovery, ToHTTPModel(recovery))
	blocked := logicrecovery.BlockedTopologyRecovery{
		FailedInstanceKey: instmodel.InstanceKey{Hostname: "blocked.example", Port: 3306},
		ClusterName:       "cluster-a", Analysis: instanalysis.DeadMaster,
		LastBlockedTimestamp: "2026-09-10 12:03:00", BlockingRecoveryId: 42,
	}
	assertSameJSON(t, blocked, ToHTTPModel(blocked))
	step := logicrecovery.TopologyRecoveryStep{Id: 43, RecoveryUID: recovery.UID, AuditAt: "2026-09-10 12:04:00", Message: "promoted successor"}
	assertSameJSON(t, step, ToHTTPModel(step))

	agent := domain.Agent{
		Hostname: "agent.example", Port: 3000, Token: "token", LastSubmitted: "now",
		AvailableLocalSnapshots: []string{"local"}, AvailableSnapshots: []string{"remote"},
		LogicalVolumes: []domain.LogicalVolume{{Name: "lv", GroupName: "vg", Path: "/dev/vg/lv", IsSnapshot: true, SnapshotPercent: 25.5}},
		MountPoint:     domain.Mount{Path: "/mysql", Device: "/dev/vg/lv", LVPath: "/dev/vg/lv", FileSystem: "xfs", IsMounted: true, DiskUsage: 10, MySQLDataPath: "/mysql/data", MySQLDiskUsage: 8},
		MySQLRunning:   true, MySQLDiskUsage: 8, MySQLPort: 3306, MySQLDatadirDiskFree: 20, MySQLErrorLogTail: []string{"ready"},
	}
	assertSameJSON(t, agent, ToHTTPModel(agent))
	seed := domain.SeedOperation{SeedId: 7, TargetHostname: "target", SourceHostname: "source", StartTimestamp: "start", EndTimestamp: "end", IsComplete: true, IsSuccessful: true}
	assertSameJSON(t, seed, ToHTTPModel(seed))
	seedState := domain.SeedOperationState{SeedStateId: 8, SeedId: seed.SeedId, StateTimestamp: "now", Action: "copy", ErrorMessage: ""}
	assertSameJSON(t, seedState, ToHTTPModel(seedState))
}
