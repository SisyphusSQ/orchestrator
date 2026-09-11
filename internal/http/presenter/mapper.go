package presenter

import (
	"github.com/openark/orchestrator/internal/http/contract"
	"github.com/openark/orchestrator/internal/http/transport"
	instanalysis "github.com/openark/orchestrator/internal/inst/analysis"
	instcluster "github.com/openark/orchestrator/internal/inst/cluster"
	instmodel "github.com/openark/orchestrator/internal/inst/instance"
	logicrecovery "github.com/openark/orchestrator/internal/logic/recovery"
	"github.com/openark/orchestrator/internal/models/domain"
	"github.com/openark/orchestrator/internal/models/vo"
)

func instanceKeyVO(value instmodel.InstanceKey) vo.InstanceKey {
	return vo.InstanceKey{Hostname: value.Hostname, Port: value.Port}
}

func instanceKeysVO(values []instmodel.InstanceKey) []vo.InstanceKey {
	if values == nil {
		return nil
	}
	result := make([]vo.InstanceKey, 0, len(values))
	for _, value := range values {
		result = append(result, instanceKeyVO(value))
	}
	return result
}

func instanceKeyMapVO(values instmodel.InstanceKeyMap) []vo.InstanceKey {
	return instanceKeysVO(values.GetInstanceKeys())
}

func binlogCoordinatesVO(value instmodel.BinlogCoordinates) vo.BinlogCoordinates {
	return vo.BinlogCoordinates{LogFile: value.LogFile, LogPos: value.LogPos, Type: int(value.Type)}
}

func clusterInfoVO(value instcluster.ClusterInfo) vo.ClusterInfo {
	return vo.ClusterInfo{
		ClusterName:                            value.ClusterName,
		ClusterAlias:                           value.ClusterAlias,
		ClusterDomain:                          value.ClusterDomain,
		CountInstances:                         value.CountInstances,
		HeuristicLag:                           value.HeuristicLag,
		HasAutomatedMasterRecovery:             value.HasAutomatedMasterRecovery,
		HasAutomatedIntermediateMasterRecovery: value.HasAutomatedIntermediateMasterRecovery,
	}
}

func stringSliceVO(values []string) []string {
	if values == nil {
		return nil
	}
	return append(make([]string, 0, len(values)), values...)
}

func instanceVO(value *instmodel.Instance) *vo.Instance {
	if value == nil {
		return nil
	}
	return &vo.Instance{
		Key:                                instanceKeyVO(value.Key),
		InstanceAlias:                      value.InstanceAlias,
		Uptime:                             value.Uptime,
		ServerID:                           value.ServerID,
		ServerUUID:                         value.ServerUUID,
		Version:                            value.Version,
		VersionComment:                     value.VersionComment,
		FlavorName:                         value.FlavorName,
		ReadOnly:                           value.ReadOnly,
		Binlog_format:                      value.Binlog_format,
		BinlogRowImage:                     value.BinlogRowImage,
		LogBinEnabled:                      value.LogBinEnabled,
		LogSlaveUpdatesEnabled:             value.LogReplicationUpdatesEnabled,
		LogReplicationUpdatesEnabled:       value.LogReplicationUpdatesEnabled,
		SelfBinlogCoordinates:              binlogCoordinatesVO(value.SelfBinlogCoordinates),
		MasterKey:                          instanceKeyVO(value.MasterKey),
		MasterUUID:                         value.MasterUUID,
		AncestryUUID:                       value.AncestryUUID,
		IsDetachedMaster:                   value.IsDetachedMaster,
		Slave_SQL_Running:                  value.ReplicationSQLThreadRuning,
		ReplicationSQLThreadRuning:         value.ReplicationSQLThreadRuning,
		Slave_IO_Running:                   value.ReplicationIOThreadRuning,
		ReplicationIOThreadRuning:          value.ReplicationIOThreadRuning,
		ReplicationSQLThreadState:          int(value.ReplicationSQLThreadState),
		ReplicationIOThreadState:           int(value.ReplicationIOThreadState),
		HasReplicationFilters:              value.HasReplicationFilters,
		GTIDMode:                           value.GTIDMode,
		SupportsOracleGTID:                 value.SupportsOracleGTID,
		UsingOracleGTID:                    value.UsingOracleGTID,
		UsingMariaDBGTID:                   value.UsingMariaDBGTID,
		UsingPseudoGTID:                    value.UsingPseudoGTID,
		ReadBinlogCoordinates:              binlogCoordinatesVO(value.ReadBinlogCoordinates),
		ExecBinlogCoordinates:              binlogCoordinatesVO(value.ExecBinlogCoordinates),
		IsDetached:                         value.IsDetached,
		RelaylogCoordinates:                binlogCoordinatesVO(value.RelaylogCoordinates),
		LastSQLError:                       value.LastSQLError,
		LastIOError:                        value.LastIOError,
		SecondsBehindMaster:                value.SecondsBehindMaster,
		SQLDelay:                           value.SQLDelay,
		ExecutedGtidSet:                    value.ExecutedGtidSet,
		GtidPurged:                         value.GtidPurged,
		GtidErrant:                         value.GtidErrant,
		SlaveLagSeconds:                    value.ReplicationLagSeconds,
		ReplicationLagSeconds:              value.ReplicationLagSeconds,
		SlaveHosts:                         instanceKeyMapVO(value.Replicas),
		Replicas:                           instanceKeyMapVO(value.Replicas),
		ClusterName:                        value.ClusterName,
		SuggestedClusterAlias:              value.SuggestedClusterAlias,
		DataCenter:                         value.DataCenter,
		Region:                             value.Region,
		PhysicalEnvironment:                value.PhysicalEnvironment,
		ReplicationDepth:                   value.ReplicationDepth,
		IsCoMaster:                         value.IsCoMaster,
		HasReplicationCredentials:          value.HasReplicationCredentials,
		ReplicationCredentialsAvailable:    value.ReplicationCredentialsAvailable,
		SemiSyncAvailable:                  value.SemiSyncAvailable,
		SemiSyncPriority:                   value.SemiSyncPriority,
		SemiSyncMasterPluginNewVersion:     value.SemiSyncMasterPluginNewVersion,
		SemiSyncReplicaPluginNewVersion:    value.SemiSyncReplicaPluginNewVersion,
		SemiSyncMasterEnabled:              value.SemiSyncMasterEnabled,
		SemiSyncReplicaEnabled:             value.SemiSyncReplicaEnabled,
		SemiSyncMasterTimeout:              value.SemiSyncMasterTimeout,
		SemiSyncMasterWaitForReplicaCount:  value.SemiSyncMasterWaitForReplicaCount,
		SemiSyncMasterStatus:               value.SemiSyncMasterStatus,
		SemiSyncMasterClients:              value.SemiSyncMasterClients,
		SemiSyncReplicaStatus:              value.SemiSyncReplicaStatus,
		LastSeenTimestamp:                  value.LastSeenTimestamp,
		IsLastCheckValid:                   value.IsLastCheckValid,
		IsUpToDate:                         value.IsUpToDate,
		IsRecentlyChecked:                  value.IsRecentlyChecked,
		SecondsSinceLastSeen:               value.SecondsSinceLastSeen,
		CountMySQLSnapshots:                value.CountMySQLSnapshots,
		IsCandidate:                        value.IsCandidate,
		PromotionRule:                      string(value.PromotionRule),
		IsDowntimed:                        value.IsDowntimed,
		DowntimeReason:                     value.DowntimeReason,
		DowntimeOwner:                      value.DowntimeOwner,
		DowntimeEndTimestamp:               value.DowntimeEndTimestamp,
		ElapsedDowntime:                    value.ElapsedDowntime,
		UnresolvedHostname:                 value.UnresolvedHostname,
		AllowTLS:                           value.AllowTLS,
		ReplicationSSLCAFile:               value.ReplicationSSLCAFile,
		ReplicationSSLCAPath:               value.ReplicationSSLCAPath,
		ReplicationSSLCert:                 value.ReplicationSSLCert,
		ReplicationSSLCipher:               value.ReplicationSSLCipher,
		ReplicationSSLCRLFile:              value.ReplicationSSLCRLFile,
		ReplicationSSLCRLPath:              value.ReplicationSSLCRLPath,
		ReplicationSSLKey:                  value.ReplicationSSLKey,
		ReplicationSSLVerifyServerCert:     value.ReplicationSSLVerifyServerCert,
		ReplicationTLSVersion:              value.ReplicationTLSVersion,
		ReplicationTLSCiphersuites:         value.ReplicationTLSCiphersuites,
		ReplicationSourcePublicKeyPath:     value.ReplicationSourcePublicKeyPath,
		ReplicationGetSourcePublicKey:      value.ReplicationGetSourcePublicKey,
		Problems:                           stringSliceVO(value.Problems),
		LastDiscoveryLatency:               value.LastDiscoveryLatency,
		ReplicationGroupName:               value.ReplicationGroupName,
		ReplicationGroupIsSinglePrimary:    value.ReplicationGroupIsSinglePrimary,
		ReplicationGroupMemberState:        value.ReplicationGroupMemberState,
		ReplicationGroupMemberRole:         value.ReplicationGroupMemberRole,
		ReplicationGroupMembers:            instanceKeyMapVO(value.ReplicationGroupMembers),
		ReplicationGroupPrimaryInstanceKey: instanceKeyVO(value.ReplicationGroupPrimaryInstanceKey),
	}
}

func instancesVO(values []*instmodel.Instance) []*vo.Instance {
	if values == nil {
		return nil
	}
	result := make([]*vo.Instance, 0, len(values))
	for _, value := range values {
		result = append(result, instanceVO(value))
	}
	return result
}

func instanceValuesVO(values []instmodel.Instance) []vo.Instance {
	if values == nil {
		return nil
	}
	result := make([]vo.Instance, 0, len(values))
	for i := range values {
		result = append(result, *instanceVO(&values[i]))
	}
	return result
}

func clusterInfosVO(values []instcluster.ClusterInfo) []vo.ClusterInfo {
	if values == nil {
		return nil
	}
	result := make([]vo.ClusterInfo, 0, len(values))
	for _, value := range values {
		result = append(result, clusterInfoVO(value))
	}
	return result
}

func replicationAnalysisVO(value instanalysis.ReplicationAnalysis) vo.ReplicationAnalysis {
	structure := make([]string, len(value.StructureAnalysis))
	for i, analysis := range value.StructureAnalysis {
		structure[i] = string(analysis)
	}
	if value.StructureAnalysis == nil {
		structure = nil
	}
	replicas := instanceKeyMapVO(value.Replicas)
	return vo.ReplicationAnalysis{
		AnalyzedInstanceKey:                       instanceKeyVO(value.AnalyzedInstanceKey),
		AnalyzedInstanceMasterKey:                 instanceKeyVO(value.AnalyzedInstanceMasterKey),
		ClusterDetails:                            clusterInfoVO(value.ClusterDetails),
		AnalyzedInstanceDataCenter:                value.AnalyzedInstanceDataCenter,
		AnalyzedInstanceRegion:                    value.AnalyzedInstanceRegion,
		AnalyzedInstancePhysicalEnvironment:       value.AnalyzedInstancePhysicalEnvironment,
		AnalyzedInstanceBinlogCoordinates:         binlogCoordinatesVO(value.AnalyzedInstanceBinlogCoordinates),
		IsMaster:                                  value.IsMaster,
		IsReplicationGroupMember:                  value.IsReplicationGroupMember,
		IsCoMaster:                                value.IsCoMaster,
		LastCheckValid:                            value.LastCheckValid,
		LastCheckPartialSuccess:                   value.LastCheckPartialSuccess,
		CountReplicas:                             value.CountReplicas,
		CountValidReplicas:                        value.CountValidReplicas,
		CountValidReplicatingReplicas:             value.CountValidReplicatingReplicas,
		CountReplicasFailingToConnectToMaster:     value.CountReplicasFailingToConnectToMaster,
		CountDowntimedReplicas:                    value.CountDowntimedReplicas,
		ReplicationDepth:                          value.ReplicationDepth,
		Replicas:                                  replicas,
		SlaveHosts:                                replicas,
		IsFailingToConnectToMaster:                value.IsFailingToConnectToMaster,
		Analysis:                                  string(value.Analysis),
		Description:                               value.Description,
		StructureAnalysis:                         structure,
		IsDowntimed:                               value.IsDowntimed,
		IsReplicasDowntimed:                       value.IsReplicasDowntimed,
		DowntimeEndTimestamp:                      value.DowntimeEndTimestamp,
		DowntimeRemainingSeconds:                  value.DowntimeRemainingSeconds,
		IsBinlogServer:                            value.IsBinlogServer,
		PseudoGTIDImmediateTopology:               value.PseudoGTIDImmediateTopology,
		OracleGTIDImmediateTopology:               value.OracleGTIDImmediateTopology,
		MariaDBGTIDImmediateTopology:              value.MariaDBGTIDImmediateTopology,
		BinlogServerImmediateTopology:             value.BinlogServerImmediateTopology,
		SemiSyncMasterEnabled:                     value.SemiSyncMasterEnabled,
		SemiSyncMasterStatus:                      value.SemiSyncMasterStatus,
		SemiSyncMasterWaitForReplicaCount:         value.SemiSyncMasterWaitForReplicaCount,
		SemiSyncMasterClients:                     value.SemiSyncMasterClients,
		CountSemiSyncReplicasEnabled:              value.CountSemiSyncReplicasEnabled,
		CountLoggingReplicas:                      value.CountLoggingReplicas,
		CountStatementBasedLoggingReplicas:        value.CountStatementBasedLoggingReplicas,
		CountMixedBasedLoggingReplicas:            value.CountMixedBasedLoggingReplicas,
		CountRowBasedLoggingReplicas:              value.CountRowBasedLoggingReplicas,
		CountDistinctMajorVersionsLoggingReplicas: value.CountDistinctMajorVersionsLoggingReplicas,
		CountDelayedReplicas:                      value.CountDelayedReplicas,
		CountLaggingReplicas:                      value.CountLaggingReplicas,
		IsActionableRecovery:                      value.IsActionableRecovery,
		ProcessingNodeHostname:                    value.ProcessingNodeHostname,
		ProcessingNodeToken:                       value.ProcessingNodeToken,
		CountAdditionalAgreeingNodes:              value.CountAdditionalAgreeingNodes,
		StartActivePeriod:                         value.StartActivePeriod,
		SkippableDueToDowntime:                    value.SkippableDueToDowntime,
		GTIDMode:                                  value.GTIDMode,
		MinReplicaGTIDMode:                        value.MinReplicaGTIDMode,
		MaxReplicaGTIDMode:                        value.MaxReplicaGTIDMode,
		MaxReplicaGTIDErrant:                      value.MaxReplicaGTIDErrant,
		CommandHint:                               value.CommandHint,
		IsReadOnly:                                value.IsReadOnly,
	}
}

func replicationAnalysesVO(values []instanalysis.ReplicationAnalysis) []vo.ReplicationAnalysis {
	if values == nil {
		return nil
	}
	result := make([]vo.ReplicationAnalysis, 0, len(values))
	for _, value := range values {
		result = append(result, replicationAnalysisVO(value))
	}
	return result
}

func topologyRecoveryVO(value *logicrecovery.TopologyRecovery) *vo.TopologyRecovery {
	if value == nil {
		return nil
	}
	var successorKey *vo.InstanceKey
	if value.SuccessorKey != nil {
		mapped := instanceKeyVO(*value.SuccessorKey)
		successorKey = &mapped
	}
	var successorCoordinates *vo.BinlogCoordinates
	if value.SuccessorBinlogCoordinates != nil {
		mapped := binlogCoordinatesVO(*value.SuccessorBinlogCoordinates)
		successorCoordinates = &mapped
	}
	return &vo.TopologyRecovery{
		Id:                         value.Id,
		UID:                        value.UID,
		AnalysisEntry:              replicationAnalysisVO(value.AnalysisEntry),
		SuccessorKey:               successorKey,
		SuccessorAlias:             value.SuccessorAlias,
		SuccessorBinlogCoordinates: successorCoordinates,
		IsActive:                   value.IsActive,
		IsSuccessful:               value.IsSuccessful,
		LostReplicas:               instanceKeyMapVO(value.LostReplicas),
		ParticipatingInstanceKeys:  instanceKeyMapVO(value.ParticipatingInstanceKeys),
		AllErrors:                  stringSliceVO(value.AllErrors),
		RecoveryStartTimestamp:     value.RecoveryStartTimestamp,
		RecoveryEndTimestamp:       value.RecoveryEndTimestamp,
		ProcessingNodeHostname:     value.ProcessingNodeHostname,
		ProcessingNodeToken:        value.ProcessingNodeToken,
		PolicyRevision:             value.PolicyRevision,
		HookAssignmentRevision:     value.HookAssignmentRevision,
		Acknowledged:               value.Acknowledged,
		AcknowledgedAt:             value.AcknowledgedAt,
		AcknowledgedBy:             value.AcknowledgedBy,
		AcknowledgedComment:        value.AcknowledgedComment,
		LastDetectionId:            value.LastDetectionId,
		RelatedRecoveryId:          value.RelatedRecoveryId,
		Type:                       string(value.Type),
		RecoveryType:               string(value.RecoveryType),
	}
}

func topologyRecoveriesVO(values []*logicrecovery.TopologyRecovery) []*vo.TopologyRecovery {
	if values == nil {
		return nil
	}
	result := make([]*vo.TopologyRecovery, 0, len(values))
	for _, value := range values {
		result = append(result, topologyRecoveryVO(value))
	}
	return result
}

func blockedTopologyRecoveryVO(value logicrecovery.BlockedTopologyRecovery) vo.BlockedTopologyRecovery {
	return vo.BlockedTopologyRecovery{
		FailedInstanceKey:    instanceKeyVO(value.FailedInstanceKey),
		ClusterName:          value.ClusterName,
		Analysis:             string(value.Analysis),
		LastBlockedTimestamp: value.LastBlockedTimestamp,
		BlockingRecoveryId:   value.BlockingRecoveryId,
	}
}

func blockedTopologyRecoveriesVO(values []logicrecovery.BlockedTopologyRecovery) []vo.BlockedTopologyRecovery {
	if values == nil {
		return nil
	}
	result := make([]vo.BlockedTopologyRecovery, 0, len(values))
	for _, value := range values {
		result = append(result, blockedTopologyRecoveryVO(value))
	}
	return result
}

func topologyRecoveryStepVO(value logicrecovery.TopologyRecoveryStep) vo.TopologyRecoveryStep {
	return vo.TopologyRecoveryStep{Id: value.Id, RecoveryUID: value.RecoveryUID, AuditAt: value.AuditAt, Message: value.Message}
}

func topologyRecoveryStepsVO(values []logicrecovery.TopologyRecoveryStep) []vo.TopologyRecoveryStep {
	if values == nil {
		return nil
	}
	result := make([]vo.TopologyRecoveryStep, 0, len(values))
	for _, value := range values {
		result = append(result, topologyRecoveryStepVO(value))
	}
	return result
}

func logicalVolumeVO(value domain.LogicalVolume) vo.LogicalVolume {
	return vo.LogicalVolume{
		Name: value.Name, GroupName: value.GroupName, Path: value.Path,
		IsSnapshot: value.IsSnapshot, SnapshotPercent: value.SnapshotPercent,
	}
}

func logicalVolumesVO(values []domain.LogicalVolume) []vo.LogicalVolume {
	if values == nil {
		return nil
	}
	result := make([]vo.LogicalVolume, 0, len(values))
	for _, value := range values {
		result = append(result, logicalVolumeVO(value))
	}
	return result
}

func mountVO(value domain.Mount) vo.Mount {
	return vo.Mount{
		Path: value.Path, Device: value.Device, LVPath: value.LVPath, FileSystem: value.FileSystem,
		IsMounted: value.IsMounted, DiskUsage: value.DiskUsage, MySQLDataPath: value.MySQLDataPath, MySQLDiskUsage: value.MySQLDiskUsage,
	}
}

func agentVO(value domain.Agent) vo.Agent {
	return vo.Agent{
		Hostname: value.Hostname, Port: value.Port, Token: value.Token, LastSubmitted: value.LastSubmitted,
		AvailableLocalSnapshots: stringSliceVO(value.AvailableLocalSnapshots),
		AvailableSnapshots:      stringSliceVO(value.AvailableSnapshots),
		LogicalVolumes:          logicalVolumesVO(value.LogicalVolumes),
		MountPoint:              mountVO(value.MountPoint),
		MySQLRunning:            value.MySQLRunning,
		MySQLDiskUsage:          value.MySQLDiskUsage,
		MySQLPort:               value.MySQLPort,
		MySQLDatadirDiskFree:    value.MySQLDatadirDiskFree,
		MySQLErrorLogTail:       stringSliceVO(value.MySQLErrorLogTail),
	}
}

func agentsVO(values []domain.Agent) []vo.Agent {
	if values == nil {
		return nil
	}
	result := make([]vo.Agent, 0, len(values))
	for _, value := range values {
		result = append(result, agentVO(value))
	}
	return result
}

func seedOperationVO(value domain.SeedOperation) vo.SeedOperation {
	return vo.SeedOperation{
		SeedId: value.SeedId, TargetHostname: value.TargetHostname, SourceHostname: value.SourceHostname,
		StartTimestamp: value.StartTimestamp, EndTimestamp: value.EndTimestamp,
		IsComplete: value.IsComplete, IsSuccessful: value.IsSuccessful,
	}
}

func seedOperationsVO(values []domain.SeedOperation) []vo.SeedOperation {
	if values == nil {
		return nil
	}
	result := make([]vo.SeedOperation, 0, len(values))
	for _, value := range values {
		result = append(result, seedOperationVO(value))
	}
	return result
}

func seedOperationStateVO(value domain.SeedOperationState) vo.SeedOperationState {
	return vo.SeedOperationState{
		SeedStateId: value.SeedStateId, SeedId: value.SeedId, StateTimestamp: value.StateTimestamp,
		Action: value.Action, ErrorMessage: value.ErrorMessage,
	}
}

func seedOperationStatesVO(values []domain.SeedOperationState) []vo.SeedOperationState {
	if values == nil {
		return nil
	}
	result := make([]vo.SeedOperationState, 0, len(values))
	for _, value := range values {
		result = append(result, seedOperationStateVO(value))
	}
	return result
}

func ToHTTPModel(value any) any {
	switch value := value.(type) {
	case *contract.Response:
		if value == nil {
			return (*contract.Response)(nil)
		}
		result := *value
		result.Details = ToHTTPModel(value.Details)
		return &result
	case contract.Response:
		value.Details = ToHTTPModel(value.Details)
		return value
	case *instmodel.Instance:
		return instanceVO(value)
	case instmodel.Instance:
		return *instanceVO(&value)
	case []*instmodel.Instance:
		return instancesVO(value)
	case []instmodel.Instance:
		return instanceValuesVO(value)
	case instmodel.InstanceKey:
		return instanceKeyVO(value)
	case *instmodel.InstanceKey:
		if value == nil {
			return (*vo.InstanceKey)(nil)
		}
		result := instanceKeyVO(*value)
		return &result
	case []instmodel.InstanceKey:
		return instanceKeysVO(value)
	case instcluster.ClusterInfo:
		return clusterInfoVO(value)
	case *instcluster.ClusterInfo:
		if value == nil {
			return (*vo.ClusterInfo)(nil)
		}
		result := clusterInfoVO(*value)
		return &result
	case []instcluster.ClusterInfo:
		return clusterInfosVO(value)
	case instanalysis.ReplicationAnalysis:
		return replicationAnalysisVO(value)
	case *instanalysis.ReplicationAnalysis:
		if value == nil {
			return (*vo.ReplicationAnalysis)(nil)
		}
		result := replicationAnalysisVO(*value)
		return &result
	case []instanalysis.ReplicationAnalysis:
		return replicationAnalysesVO(value)
	case *logicrecovery.TopologyRecovery:
		return topologyRecoveryVO(value)
	case []*logicrecovery.TopologyRecovery:
		return topologyRecoveriesVO(value)
	case logicrecovery.BlockedTopologyRecovery:
		return blockedTopologyRecoveryVO(value)
	case []logicrecovery.BlockedTopologyRecovery:
		return blockedTopologyRecoveriesVO(value)
	case logicrecovery.TopologyRecoveryStep:
		return topologyRecoveryStepVO(value)
	case []logicrecovery.TopologyRecoveryStep:
		return topologyRecoveryStepsVO(value)
	case domain.Agent:
		return agentVO(value)
	case *domain.Agent:
		if value == nil {
			return (*vo.Agent)(nil)
		}
		result := agentVO(*value)
		return &result
	case []domain.Agent:
		return agentsVO(value)
	case domain.SeedOperation:
		return seedOperationVO(value)
	case []domain.SeedOperation:
		return seedOperationsVO(value)
	case domain.SeedOperationState:
		return seedOperationStateVO(value)
	case []domain.SeedOperationState:
		return seedOperationStatesVO(value)
	default:
		return value
	}
}

func WriteJSON(r transport.Responder, status int, value any) {
	r.JSON(status, ToHTTPModel(value))
}
