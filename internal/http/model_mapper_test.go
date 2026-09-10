package http

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/openark/orchestrator/internal/inst"
	"github.com/openark/orchestrator/internal/logic"
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

func TestInstanceVOIsConcreteAndPreservesLegacyJSON(t *testing.T) {
	instance := inst.NewInstance()
	instance.Key = inst.InstanceKey{Hostname: "db.example", Port: 3307}
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
	instance.SelfBinlogCoordinates = inst.BinlogCoordinates{LogFile: "mysql-bin.000010", LogPos: 120, Type: inst.BinaryLog}
	instance.MasterKey = inst.InstanceKey{Hostname: "primary.example", Port: 3306}
	instance.MasterUUID = "master-uuid"
	instance.AncestryUUID = "ancestry"
	instance.IsDetachedMaster = true
	instance.Slave_SQL_Running = false
	instance.ReplicationSQLThreadRuning = true
	instance.Slave_IO_Running = true
	instance.ReplicationIOThreadRuning = false
	instance.ReplicationSQLThreadState = inst.ReplicationThreadState(1)
	instance.ReplicationIOThreadState = inst.ReplicationThreadState(2)
	instance.HasReplicationFilters = true
	instance.GTIDMode = "ON"
	instance.SupportsOracleGTID = true
	instance.UsingOracleGTID = true
	instance.UsingMariaDBGTID = true
	instance.UsingPseudoGTID = true
	instance.ReadBinlogCoordinates = inst.BinlogCoordinates{LogFile: "mysql-bin.000009", LogPos: 90, Type: inst.BinaryLog}
	instance.ExecBinlogCoordinates = inst.BinlogCoordinates{LogFile: "mysql-bin.000009", LogPos: 80, Type: inst.BinaryLog}
	instance.IsDetached = true
	instance.RelaylogCoordinates = inst.BinlogCoordinates{LogFile: "relay.000001", LogPos: 70, Type: inst.RelayLog}
	instance.LastSQLError = "sql error"
	instance.LastIOError = "io error"
	instance.SecondsBehindMaster = domain.NullInt64{Int64: 4, Valid: true}
	instance.SQLDelay = 2
	instance.ExecutedGtidSet = "executed"
	instance.GtidPurged = "purged"
	instance.GtidErrant = "errant"
	instance.SlaveLagSeconds = domain.NullInt64{Int64: 99, Valid: true}
	instance.ReplicationLagSeconds = domain.NullInt64{Int64: 8, Valid: true}
	instance.SlaveHosts = *inst.NewInstanceKeyMap()
	instance.SlaveHosts.AddKey(inst.InstanceKey{Hostname: "stale.example", Port: 3310})
	instance.Replicas.AddKey(inst.InstanceKey{Hostname: "replica-b.example", Port: 3306})
	instance.Replicas.AddKey(inst.InstanceKey{Hostname: "replica-a.example", Port: 3306})
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
	instance.PromotionRule = inst.PreferPromoteRule
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
	instance.ReplicationGroupMembers.AddKey(inst.InstanceKey{Hostname: "group-b.example", Port: 3306})
	instance.ReplicationGroupPrimaryInstanceKey = inst.InstanceKey{Hostname: "group-a.example", Port: 3306}

	mapped, ok := toHTTPModel(instance).(*vo.Instance)
	if !ok {
		t.Fatalf("toHTTPModel(*inst.Instance) type = %T; want *vo.Instance", toHTTPModel(instance))
	}
	assertSameJSON(t, instance, mapped)
	assertSameJSON(t, &APIResponse{Code: OK, Message: "instance", Details: instance}, toHTTPModel(&APIResponse{Code: OK, Message: "instance", Details: instance}))
}

func TestAnalysisRecoveryAndAgentVOsPreserveJSON(t *testing.T) {
	analysis := inst.ReplicationAnalysis{
		AnalyzedInstanceKey:               inst.InstanceKey{Hostname: "failed.example", Port: 3306},
		AnalyzedInstanceMasterKey:         inst.InstanceKey{Hostname: "master.example", Port: 3306},
		ClusterDetails:                    inst.ClusterInfo{ClusterName: "cluster-a", ClusterAlias: "payments", CountInstances: 3},
		AnalyzedInstanceDataCenter:        "dc-a",
		AnalyzedInstanceBinlogCoordinates: inst.BinlogCoordinates{LogFile: "mysql-bin.000100", LogPos: 500},
		IsMaster:                          true,
		LastCheckValid:                    true,
		CountReplicas:                     2,
		Analysis:                          inst.DeadMaster,
		Description:                       "master is unreachable",
		StructureAnalysis:                 []inst.AnalysisCode{inst.NoWriteableMasterStructureWarning},
		CountLoggingReplicas:              2,
		IsActionableRecovery:              true,
		ProcessingNodeHostname:            "orc-1",
		ProcessingNodeToken:               "token",
		CountAdditionalAgreeingNodes:      1,
		StartActivePeriod:                 "2026-09-10 12:00:00",
		GTIDMode:                          "ON",
		MinReplicaGTIDMode:                "ON_PERMISSIVE",
		MaxReplicaGTIDMode:                "ON",
		CommandHint:                       inst.ForceMasterFailoverCommandHint,
		SlaveHosts:                        *inst.NewInstanceKeyMap(),
	}
	analysis.SlaveHosts.AddKey(inst.InstanceKey{Hostname: "stale.example", Port: 3306})
	analysis.Replicas = *inst.NewInstanceKeyMap()
	analysis.Replicas.AddKey(inst.InstanceKey{Hostname: "replica-b.example", Port: 3306})
	analysis.Replicas.AddKey(inst.InstanceKey{Hostname: "replica-a.example", Port: 3306})
	assertSameJSON(t, &analysis, toHTTPModel(&analysis))

	successor := inst.InstanceKey{Hostname: "successor.example", Port: 3307}
	coordinates := inst.BinlogCoordinates{LogFile: "mysql-bin.000101", LogPos: 600}
	recovery := &logic.TopologyRecovery{
		AnalysisEntry:             analysis,
		LostReplicas:              *inst.NewInstanceKeyMap(),
		ParticipatingInstanceKeys: *inst.NewInstanceKeyMap(),
	}
	recovery.Id = 42
	recovery.UID = "recovery-42"
	recovery.SuccessorKey = &successor
	recovery.SuccessorAlias = "new-primary"
	recovery.SuccessorBinlogCoordinates = &coordinates
	recovery.IsActive = true
	recovery.IsSuccessful = true
	recovery.LostReplicas.AddKey(inst.InstanceKey{Hostname: "lost.example", Port: 3306})
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
	recovery.Type = logic.MasterRecovery
	recovery.RecoveryType = logic.MasterRecoveryGTID
	assertSameJSON(t, recovery, toHTTPModel(recovery))
	blocked := logic.BlockedTopologyRecovery{
		FailedInstanceKey: inst.InstanceKey{Hostname: "blocked.example", Port: 3306},
		ClusterName:       "cluster-a", Analysis: inst.DeadMaster,
		LastBlockedTimestamp: "2026-09-10 12:03:00", BlockingRecoveryId: 42,
	}
	assertSameJSON(t, blocked, toHTTPModel(blocked))
	step := logic.TopologyRecoveryStep{Id: 43, RecoveryUID: recovery.UID, AuditAt: "2026-09-10 12:04:00", Message: "promoted successor"}
	assertSameJSON(t, step, toHTTPModel(step))

	agent := domain.Agent{
		Hostname: "agent.example", Port: 3000, Token: "token", LastSubmitted: "now",
		AvailableLocalSnapshots: []string{"local"}, AvailableSnapshots: []string{"remote"},
		LogicalVolumes: []domain.LogicalVolume{{Name: "lv", GroupName: "vg", Path: "/dev/vg/lv", IsSnapshot: true, SnapshotPercent: 25.5}},
		MountPoint:     domain.Mount{Path: "/mysql", Device: "/dev/vg/lv", LVPath: "/dev/vg/lv", FileSystem: "xfs", IsMounted: true, DiskUsage: 10, MySQLDataPath: "/mysql/data", MySQLDiskUsage: 8},
		MySQLRunning:   true, MySQLDiskUsage: 8, MySQLPort: 3306, MySQLDatadirDiskFree: 20, MySQLErrorLogTail: []string{"ready"},
	}
	assertSameJSON(t, agent, toHTTPModel(agent))
	seed := domain.SeedOperation{SeedId: 7, TargetHostname: "target", SourceHostname: "source", StartTimestamp: "start", EndTimestamp: "end", IsComplete: true, IsSuccessful: true}
	assertSameJSON(t, seed, toHTTPModel(seed))
	seedState := domain.SeedOperationState{SeedStateId: 8, SeedId: seed.SeedId, StateTimestamp: "now", Action: "copy", ErrorMessage: ""}
	assertSameJSON(t, seedState, toHTTPModel(seedState))
}
