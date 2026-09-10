/*
   Copyright 2015 Shlomi Noach, courtesy Booking.com

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package inst

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/openark/orchestrator/internal/config"
	modeldomain "github.com/openark/orchestrator/internal/models/domain"
	"github.com/openark/orchestrator/internal/models/dto"
	"github.com/openark/orchestrator/internal/process"
	"github.com/openark/orchestrator/internal/recoverypolicy"
	"github.com/openark/orchestrator/internal/repository/metadata"
	"github.com/openark/orchestrator/internal/util"

	"github.com/openark/orchestrator/internal/golib/log"
	"github.com/patrickmn/go-cache"

	"github.com/openark/orchestrator/internal/observability"
)

var analysisChangeWriteAttemptCounter = observability.NewCounter("orchestrator_analysis_change_write_attempt_total", "analysis.change.write.attempt events")
var analysisChangeWriteCounter = observability.NewCounter("orchestrator_analysis_change_write_total", "analysis.change.write events")

var recentInstantAnalysis *cache.Cache

func init() {

	go initializeAnalysisStorePostConfiguration()
}

func initializeAnalysisStorePostConfiguration() {
	config.WaitForConfigurationToBeLoaded()

	recentInstantAnalysis = cache.New(time.Duration(config.RecoveryPollSeconds*2)*time.Second, time.Second)
}

// GetReplicationAnalysis will check for replication problems (dead master; unreachable master; etc)
func GetReplicationAnalysis(clusterName string, hints *dto.ReplicationAnalysisHints) ([]ReplicationAnalysis, error) {
	result := []ReplicationAnalysis{}
	policy := recoverypolicy.Current(clusterName)

	lockedSeconds := policy.ReasonableLockedSemiSyncMasterSeconds
	if lockedSeconds == 0 {
		lockedSeconds = policy.ReasonableReplicationLagSeconds
	}
	validCheckSeconds := ValidSecondsFromSeenToLastAttemptedCheck()
	rows, err := metadata.ReadReplicationAnalysisRows(context.Background(), metadata.ReplicationAnalysisQuery{
		LockedSeconds:         lockedSeconds,
		ValidCheckSeconds:     validCheckSeconds,
		ReasonableLagSeconds:  policy.ReasonableReplicationLagSeconds,
		ClusterName:           clusterName,
		ReduceCount:           config.Config.Topology.Analysis.ReduceCount,
		ReductionCheckSeconds: validCheckSeconds,
	})
	if err != nil {
		return result, log.Errore(err)
	}
	for _, row := range rows {
		a := ReplicationAnalysis{
			Analysis:               NoProblem,
			ProcessingNodeHostname: process.ThisHostname,
			ProcessingNodeToken:    util.ProcessToken.Hash,
		}

		a.IsMaster = row.Master
		a.IsReplicationGroupMember = row.ReplicationGroupMember
		countCoMasterReplicas := modeldomain.NonNegativeUint(row.CountCoMasterReplicas)
		a.IsCoMaster = row.CoMaster || countCoMasterReplicas > 0
		a.AnalyzedInstanceKey = InstanceKey{Hostname: row.Hostname, Port: row.Port}
		a.AnalyzedInstanceMasterKey = InstanceKey{Hostname: row.MasterHost, Port: row.MasterPort}
		a.AnalyzedInstanceDataCenter = row.DataCenter
		a.AnalyzedInstanceRegion = row.Region
		a.AnalyzedInstancePhysicalEnvironment = row.PhysicalEnvironment
		a.AnalyzedInstanceBinlogCoordinates = BinlogCoordinates{
			LogFile: row.BinaryLogFile,
			LogPos:  row.BinaryLogPos,
			Type:    BinaryLog,
		}
		isStaleBinlogCoordinates := row.StaleBinlogCoordinates
		a.ClusterDetails.ClusterName = row.ClusterName
		a.ClusterDetails.ClusterAlias = row.ClusterAlias
		a.ClusterDetails.ClusterDomain = row.ClusterDomain
		a.GTIDMode = row.GTIDMode
		a.LastCheckValid = row.LastCheckValid
		a.LastCheckPartialSuccess = row.LastCheckPartialSuccess
		a.CountReplicas = row.CountReplicas
		a.CountValidReplicas = row.CountValidReplicas
		a.CountValidReplicatingReplicas = row.CountValidReplicatingReplicas
		a.CountReplicasFailingToConnectToMaster = row.CountReplicasFailingToConnect
		a.CountDowntimedReplicas = row.CountDowntimedReplicas
		a.ReplicationDepth = row.ReplicationDepth
		a.IsFailingToConnectToMaster = row.FailingToConnectToMaster
		a.IsDowntimed = row.Downtimed
		a.DowntimeEndTimestamp = row.DowntimeEndTimestamp
		a.DowntimeRemainingSeconds = row.DowntimeRemainingSeconds
		a.IsBinlogServer = row.BinlogServer
		a.ClusterDetails.ReadRecoveryInfo()

		a.Replicas = *NewInstanceKeyMap()
		a.Replicas.ReadCommaDelimitedList(row.SlaveHosts.String)

		countValidOracleGTIDReplicas := row.CountValidOracleGTIDReplicas
		a.OracleGTIDImmediateTopology = countValidOracleGTIDReplicas == a.CountValidReplicas && a.CountValidReplicas > 0
		countValidMariaDBGTIDReplicas := row.CountValidMariaDBGTIDReplicas
		a.MariaDBGTIDImmediateTopology = countValidMariaDBGTIDReplicas == a.CountValidReplicas && a.CountValidReplicas > 0
		countValidBinlogServerReplicas := row.CountValidBinlogServerReplicas
		a.BinlogServerImmediateTopology = countValidBinlogServerReplicas == a.CountValidReplicas && a.CountValidReplicas > 0
		a.PseudoGTIDImmediateTopology = row.PseudoGTID
		a.SemiSyncMasterEnabled = row.SemiSyncMasterEnabled
		a.SemiSyncMasterStatus = row.SemiSyncMasterStatus
		a.CountSemiSyncReplicasEnabled = modeldomain.NonNegativeUint(row.CountSemiSyncReplicas)
		// countValidSemiSyncReplicasEnabled := m.GetUint("count_valid_semi_sync_replicas")
		a.SemiSyncMasterWaitForReplicaCount = row.SemiSyncMasterWaitForReplicaCount
		a.SemiSyncMasterClients = row.SemiSyncMasterClients

		a.MinReplicaGTIDMode = row.MinReplicaGTIDMode
		a.MaxReplicaGTIDMode = row.MaxReplicaGTIDMode
		a.MaxReplicaGTIDErrant = row.MaxReplicaGTIDErrant

		a.CountLoggingReplicas = row.CountLoggingReplicas
		a.CountStatementBasedLoggingReplicas = row.CountStatementBasedLoggingReplicas
		a.CountMixedBasedLoggingReplicas = row.CountMixedBasedLoggingReplicas
		a.CountRowBasedLoggingReplicas = row.CountRowBasedLoggingReplicas
		a.CountDistinctMajorVersionsLoggingReplicas = row.CountDistinctLoggingMajorVersions

		a.CountDelayedReplicas = row.CountDelayedReplicas
		a.CountLaggingReplicas = row.CountLaggingReplicas

		a.IsReadOnly = row.ReadOnly == 1

		if !a.LastCheckValid {
			analysisMessage := fmt.Sprintf("analysis: ClusterName: %+v, IsMaster: %+v, LastCheckValid: %+v, LastCheckPartialSuccess: %+v, CountReplicas: %+v, CountValidReplicas: %+v, CountValidReplicatingReplicas: %+v, CountLaggingReplicas: %+v, CountDelayedReplicas: %+v, CountReplicasFailingToConnectToMaster: %+v",
				a.ClusterDetails.ClusterName, a.IsMaster, a.LastCheckValid, a.LastCheckPartialSuccess, a.CountReplicas, a.CountValidReplicas, a.CountValidReplicatingReplicas, a.CountLaggingReplicas, a.CountDelayedReplicas, a.CountReplicasFailingToConnectToMaster,
			)
			if util.ClearToLog("analysis_dao", analysisMessage) {
				log.Debugf("%s", analysisMessage)
			}
		}
		if !a.IsReplicationGroupMember /* Traditional Async/Semi-sync replication issue detection */ {
			if a.IsMaster && !a.LastCheckValid && a.CountReplicas == 0 {
				a.Analysis = DeadMasterWithoutReplicas
				a.Description = "Master cannot be reached by orchestrator and has no replica"
				//
			} else if a.IsMaster && !a.LastCheckValid && a.CountValidReplicas == a.CountReplicas && a.CountValidReplicatingReplicas == 0 {
				a.Analysis = DeadMaster
				a.Description = "Master cannot be reached by orchestrator and none of its replicas is replicating"
				//
			} else if a.IsMaster && !a.LastCheckValid && a.CountReplicas > 0 && a.CountValidReplicas == 0 && a.CountValidReplicatingReplicas == 0 {
				a.Analysis = DeadMasterAndReplicas
				a.Description = "Master cannot be reached by orchestrator and none of its replicas is replicating"
				//
			} else if a.IsMaster && !a.LastCheckValid && a.CountValidReplicas < a.CountReplicas && a.CountValidReplicas > 0 && a.CountValidReplicatingReplicas == 0 {
				a.Analysis = DeadMasterAndSomeReplicas
				a.Description = "Master cannot be reached by orchestrator; some of its replicas are unreachable and none of its reachable replicas is replicating"
				//
			} else if a.IsMaster && !a.LastCheckValid && a.CountLaggingReplicas == a.CountReplicas && a.CountDelayedReplicas < a.CountReplicas && a.CountValidReplicatingReplicas > 0 {
				a.Analysis = UnreachableMasterWithLaggingReplicas
				a.Description = "Master cannot be reached by orchestrator and all of its replicas are lagging"
				//
			} else if a.IsMaster && !a.LastCheckValid && !a.LastCheckPartialSuccess && a.CountValidReplicas > 0 && a.CountValidReplicatingReplicas > 0 {
				// partial success is here to reduce noise
				a.Analysis = UnreachableMaster
				a.Description = "Master cannot be reached by orchestrator but it has replicating replicas; possibly a network/host issue"
				//
			} else if a.IsMaster && !a.LastCheckValid && a.LastCheckPartialSuccess && a.CountReplicasFailingToConnectToMaster > 0 && a.CountValidReplicas > 0 && a.CountValidReplicatingReplicas > 0 {
				// there's partial success, but also at least one replica is failing to connect to master
				a.Analysis = UnreachableMaster
				a.Description = "Master cannot be reached by orchestrator but it has replicating replicas; possibly a network/host issue"
				//
			} else if a.IsMaster && a.SemiSyncMasterEnabled && a.SemiSyncMasterStatus && a.SemiSyncMasterWaitForReplicaCount > 0 && a.SemiSyncMasterClients < a.SemiSyncMasterWaitForReplicaCount {
				if isStaleBinlogCoordinates {
					a.Analysis = LockedSemiSyncMaster
					a.Description = "Semi sync master is locked since it doesn't get enough replica acknowledgements"
				} else {
					a.Analysis = LockedSemiSyncMasterHypothesis
					a.Description = "Semi sync master seems to be locked, more samplings needed to validate"
				}
				//
			} else if policy.EnforceExactSemiSyncReplicas && a.IsMaster && a.SemiSyncMasterEnabled && a.SemiSyncMasterStatus && a.SemiSyncMasterWaitForReplicaCount > 0 && a.SemiSyncMasterClients > a.SemiSyncMasterWaitForReplicaCount {
				a.Analysis = MasterWithTooManySemiSyncReplicas
				a.Description = "Semi sync master has more semi sync replicas than configured"
				//
			} else if a.IsMaster && a.LastCheckValid && a.IsReadOnly && a.CountValidReplicatingReplicas > 0 && policy.RecoverNonWriteableMaster {
				a.Analysis = NoWriteableMasterStructureWarning
				a.Description = "Master with replicas is read_only"
				//
			} else if a.IsMaster && a.LastCheckValid && a.CountReplicas == 1 && a.CountValidReplicas == a.CountReplicas && a.CountValidReplicatingReplicas == 0 {
				a.Analysis = MasterSingleReplicaNotReplicating
				a.Description = "Master is reachable but its single replica is not replicating"
				//
			} else if a.IsMaster && a.LastCheckValid && a.CountReplicas == 1 && a.CountValidReplicas == 0 {
				a.Analysis = MasterSingleReplicaDead
				a.Description = "Master is reachable but its single replica is dead"
				//
			} else if a.IsMaster && a.LastCheckValid && a.CountReplicas > 1 && a.CountValidReplicas == a.CountReplicas && a.CountValidReplicatingReplicas == 0 {
				a.Analysis = AllMasterReplicasNotReplicating
				a.Description = "Master is reachable but none of its replicas is replicating"
				//
			} else if a.IsMaster && a.LastCheckValid && a.CountReplicas > 1 && a.CountValidReplicas < a.CountReplicas && a.CountValidReplicas > 0 && a.CountValidReplicatingReplicas == 0 {
				a.Analysis = AllMasterReplicasNotReplicatingOrDead
				a.Description = "Master is reachable but none of its replicas is replicating"
				//
			} else /* co-master */ if a.IsCoMaster && !a.LastCheckValid && a.CountReplicas > 0 && a.CountValidReplicas == a.CountReplicas && a.CountValidReplicatingReplicas == 0 {
				a.Analysis = DeadCoMaster
				a.Description = "Co-master cannot be reached by orchestrator and none of its replicas is replicating"
				//
			} else if a.IsCoMaster && !a.LastCheckValid && a.CountReplicas > 0 && a.CountValidReplicas < a.CountReplicas && a.CountValidReplicas > 0 && a.CountValidReplicatingReplicas == 0 {
				a.Analysis = DeadCoMasterAndSomeReplicas
				a.Description = "Co-master cannot be reached by orchestrator; some of its replicas are unreachable and none of its reachable replicas is replicating"
				//
			} else if a.IsCoMaster && !a.LastCheckValid && !a.LastCheckPartialSuccess && a.CountValidReplicas > 0 && a.CountValidReplicatingReplicas > 0 {
				a.Analysis = UnreachableCoMaster
				a.Description = "Co-master cannot be reached by orchestrator but it has replicating replicas; possibly a network/host issue"
				//
			} else if a.IsCoMaster && a.LastCheckValid && a.CountReplicas > 0 && a.CountValidReplicatingReplicas == 0 {
				a.Analysis = AllCoMasterReplicasNotReplicating
				a.Description = "Co-master is reachable but none of its replicas is replicating"
				//
			} else /* intermediate-master */ if !a.IsMaster && !a.LastCheckValid && a.CountReplicas == 1 && a.CountValidReplicas == a.CountReplicas && a.CountReplicasFailingToConnectToMaster == a.CountReplicas && a.CountValidReplicatingReplicas == 0 {
				a.Analysis = DeadIntermediateMasterWithSingleReplicaFailingToConnect
				a.Description = "Intermediate master cannot be reached by orchestrator and its (single) replica is failing to connect"
				//
			} else if !a.IsMaster && !a.LastCheckValid && a.CountReplicas == 1 && a.CountValidReplicas == a.CountReplicas && a.CountValidReplicatingReplicas == 0 {
				a.Analysis = DeadIntermediateMasterWithSingleReplica
				a.Description = "Intermediate master cannot be reached by orchestrator and its (single) replica is not replicating"
				//
			} else if !a.IsMaster && !a.LastCheckValid && a.CountReplicas > 1 && a.CountValidReplicas == a.CountReplicas && a.CountValidReplicatingReplicas == 0 {
				a.Analysis = DeadIntermediateMaster
				a.Description = "Intermediate master cannot be reached by orchestrator and none of its replicas is replicating"
				//
			} else if !a.IsMaster && !a.LastCheckValid && a.CountValidReplicas < a.CountReplicas && a.CountValidReplicas > 0 && a.CountValidReplicatingReplicas == 0 {
				a.Analysis = DeadIntermediateMasterAndSomeReplicas
				a.Description = "Intermediate master cannot be reached by orchestrator; some of its replicas are unreachable and none of its reachable replicas is replicating"
				//
			} else if !a.IsMaster && !a.LastCheckValid && a.CountReplicas > 0 && a.CountValidReplicas == 0 {
				a.Analysis = DeadIntermediateMasterAndReplicas
				a.Description = "Intermediate master cannot be reached by orchestrator and all of its replicas are unreachable"
				//
			} else if !a.IsMaster && !a.LastCheckValid && a.CountLaggingReplicas == a.CountReplicas && a.CountDelayedReplicas < a.CountReplicas && a.CountValidReplicatingReplicas > 0 {
				a.Analysis = UnreachableIntermediateMasterWithLaggingReplicas
				a.Description = "Intermediate master cannot be reached by orchestrator and all of its replicas are lagging"
				//
			} else if !a.IsMaster && !a.LastCheckValid && !a.LastCheckPartialSuccess && a.CountValidReplicas > 0 && a.CountValidReplicatingReplicas > 0 {
				a.Analysis = UnreachableIntermediateMaster
				a.Description = "Intermediate master cannot be reached by orchestrator but it has replicating replicas; possibly a network/host issue"
				//
			} else if !a.IsMaster && a.LastCheckValid && a.CountReplicas > 1 && a.CountValidReplicatingReplicas == 0 &&
				a.CountReplicasFailingToConnectToMaster > 0 && a.CountReplicasFailingToConnectToMaster == a.CountValidReplicas {
				// All replicas are either failing to connect to master (and at least one of these have to exist)
				// or completely dead.
				// Must have at least two replicas to reach such conclusion -- do note that the intermediate master is still
				// reachable to orchestrator, so we base our conclusion on replicas only at this point.
				a.Analysis = AllIntermediateMasterReplicasFailingToConnectOrDead
				a.Description = "Intermediate master is reachable but all of its replicas are failing to connect"
				//
			} else if !a.IsMaster && a.LastCheckValid && a.CountReplicas > 0 && a.CountValidReplicas > 0 && a.CountValidReplicatingReplicas == 0 {
				a.Analysis = AllIntermediateMasterReplicasNotReplicating
				a.Description = "Intermediate master is reachable but none of its reachable replicas is replicating"
				//
			} else if a.IsBinlogServer && a.IsFailingToConnectToMaster {
				a.Analysis = BinlogServerFailingToConnectToMaster
				a.Description = "Binlog server is unable to connect to its master"
				//
			} else if a.ReplicationDepth == 1 && a.IsFailingToConnectToMaster {
				a.Analysis = FirstTierReplicaFailingToConnectToMaster
				a.Description = "1st tier replica (directly replicating from topology master) is unable to connect to the master"
				//
			}
			//		 else if a.IsMaster && a.CountReplicas == 0 {
			//			a.Analysis = MasterWithoutReplicas
			//			a.Description = "Master has no replicas"
			//		}

		} else /* Group replication issue detection */ {
			// Group member is not reachable, has replicas, and none of its reachable replicas can replicate from it
			if !a.LastCheckValid && a.CountReplicas > 0 && a.CountValidReplicatingReplicas == 0 {
				a.Analysis = DeadReplicationGroupMemberWithReplicas
				a.Description = "Group member is unreachable and all its reachable replicas are not replicating"
			}

		}
		appendAnalysis := func(analysis *ReplicationAnalysis) {
			if a.Analysis == NoProblem && len(a.StructureAnalysis) == 0 && !hints.IncludeNoProblem {
				return
			}
			for _, filter := range policy.RecoveryIgnoreHostnameFilters {
				if matched, _ := regexp.MatchString(filter, a.AnalyzedInstanceKey.Hostname); matched {
					return
				}
			}
			if a.IsDowntimed {
				a.SkippableDueToDowntime = true
			}
			if a.CountReplicas == a.CountDowntimedReplicas {
				switch a.Analysis {
				case AllMasterReplicasNotReplicating,
					AllMasterReplicasNotReplicatingOrDead,
					MasterSingleReplicaDead,
					AllCoMasterReplicasNotReplicating,
					DeadIntermediateMasterWithSingleReplica,
					DeadIntermediateMasterWithSingleReplicaFailingToConnect,
					DeadIntermediateMasterAndReplicas,
					DeadIntermediateMasterAndSomeReplicas,
					AllIntermediateMasterReplicasFailingToConnectOrDead,
					AllIntermediateMasterReplicasNotReplicating:
					a.IsReplicasDowntimed = true
					a.SkippableDueToDowntime = true
				}
			}
			if a.SkippableDueToDowntime && !hints.IncludeDowntimed {
				return
			}
			result = append(result, a)
		}

		{
			// Moving on to structure analysis
			// We also do structural checks. See if there's potential danger in promotions
			if a.IsMaster && a.CountLoggingReplicas == 0 && a.CountReplicas > 1 {
				a.StructureAnalysis = append(a.StructureAnalysis, NoLoggingReplicasStructureWarning)
			}
			if a.IsMaster && a.CountReplicas > 1 &&
				!a.OracleGTIDImmediateTopology &&
				!a.MariaDBGTIDImmediateTopology &&
				!a.BinlogServerImmediateTopology &&
				!a.PseudoGTIDImmediateTopology {
				a.StructureAnalysis = append(a.StructureAnalysis, NoFailoverSupportStructureWarning)
			}
			if a.IsMaster && a.CountStatementBasedLoggingReplicas > 0 && a.CountMixedBasedLoggingReplicas > 0 {
				a.StructureAnalysis = append(a.StructureAnalysis, StatementAndMixedLoggingReplicasStructureWarning)
			}
			if a.IsMaster && a.CountStatementBasedLoggingReplicas > 0 && a.CountRowBasedLoggingReplicas > 0 {
				a.StructureAnalysis = append(a.StructureAnalysis, StatementAndRowLoggingReplicasStructureWarning)
			}
			if a.IsMaster && a.CountMixedBasedLoggingReplicas > 0 && a.CountRowBasedLoggingReplicas > 0 {
				a.StructureAnalysis = append(a.StructureAnalysis, MixedAndRowLoggingReplicasStructureWarning)
			}
			if a.IsMaster && a.CountDistinctMajorVersionsLoggingReplicas > 1 {
				a.StructureAnalysis = append(a.StructureAnalysis, MultipleMajorVersionsLoggingReplicasStructureWarning)
			}

			if a.CountReplicas > 0 && (a.GTIDMode != a.MinReplicaGTIDMode || a.GTIDMode != a.MaxReplicaGTIDMode) {
				a.StructureAnalysis = append(a.StructureAnalysis, DifferentGTIDModesStructureWarning)
			}
			if a.MaxReplicaGTIDErrant != "" {
				a.StructureAnalysis = append(a.StructureAnalysis, ErrantGTIDStructureWarning)
			}

			if a.IsMaster && a.IsReadOnly {
				a.StructureAnalysis = append(a.StructureAnalysis, NoWriteableMasterStructureWarning)
			}

			if a.IsMaster && a.SemiSyncMasterEnabled && !a.SemiSyncMasterStatus && a.SemiSyncMasterWaitForReplicaCount > 0 && a.SemiSyncMasterClients < a.SemiSyncMasterWaitForReplicaCount {
				a.StructureAnalysis = append(a.StructureAnalysis, NotEnoughValidSemiSyncReplicasStructureWarning)
			}
		}
		appendAnalysis(&a)

		if a.CountReplicas > 0 && hints.AuditAnalysis {
			// Interesting enough for analysis
			go auditInstanceAnalysisInChangelog(&a.AnalyzedInstanceKey, a.Analysis)
		}
	}
	return result, log.Errore(err)
}

// auditInstanceAnalysisInChangelog will write down an instance's analysis in the database_instance_analysis_changelog table.
// To not repeat recurring analysis code, the database_instance_last_analysis table is used, so that only changes to
// analysis codes are written.
func auditInstanceAnalysisInChangelog(instanceKey *InstanceKey, analysisCode AnalysisCode) error {
	if lastWrittenAnalysis, found := recentInstantAnalysis.Get(instanceKey.DisplayString()); found {
		if lastWrittenAnalysis == analysisCode {
			// Surely nothing new.
			// And let's expand the timeout
			recentInstantAnalysis.Set(instanceKey.DisplayString(), analysisCode, cache.DefaultExpiration)
			return nil
		}
	}
	// Passed the cache; but does database agree that there's a change? Here's a persistent cache; this comes here
	// to verify no two orchestrator services are doing this without coordinating (namely, one dies, the other taking its place
	// and has no familiarity of the former's cache)
	analysisChangeWriteAttemptCounter.Add(context.Background(), 1)
	lastAnalysisChanged, err := metadata.WriteInstanceAnalysis(
		context.Background(), instanceKey.Hostname, instanceKey.Port, string(analysisCode),
	)
	if err != nil {
		return log.Errore(err)
	}
	recentInstantAnalysis.Set(instanceKey.DisplayString(), analysisCode, cache.DefaultExpiration)
	if !lastAnalysisChanged {
		return nil
	}
	analysisChangeWriteCounter.Add(context.Background(), 1)
	return nil
}

// ExpireInstanceAnalysisChangelog removes old-enough analysis entries from the changelog
func ExpireInstanceAnalysisChangelog() error {
	err := metadata.ExpireInstanceAnalysisChangelog(
		context.Background(), config.Config.Topology.Discovery.UnseenForgetHours,
	)
	return log.Errore(err)
}

// ReadReplicationAnalysisChangelog
func ReadReplicationAnalysisChangelog() (res [](*ReplicationAnalysisChangelog), err error) {
	analysisChangelog := &ReplicationAnalysisChangelog{}
	rows, err := metadata.ReadReplicationAnalysisChangelogRows(context.Background())
	for _, row := range rows {
		key := InstanceKey{Hostname: row.Hostname, Port: row.Port}

		if !analysisChangelog.AnalyzedInstanceKey.Equals(&key) {
			analysisChangelog = &ReplicationAnalysisChangelog{AnalyzedInstanceKey: key, Changelog: []string{}}
			res = append(res, analysisChangelog)
		}
		analysisEntry := fmt.Sprintf("%s;%s,", row.Timestamp, row.Analysis)
		analysisChangelog.Changelog = append(analysisChangelog.Changelog, analysisEntry)
	}

	if err != nil {
		log.Errore(err)
	}
	return res, err
}

// ReadPeerAnalysisMap reads raft-peer failure analysis, and returns a PeerAnalysisMap,
// indicating how many peers see which analysis
func ReadPeerAnalysisMap() (peerAnalysisMap PeerAnalysisMap, err error) {
	peerAnalysisMap = make(PeerAnalysisMap)
	rows, err := metadata.ReadPeerAnalysisRows(context.Background())
	for _, row := range rows {
		instanceKey := InstanceKey{Hostname: row.Hostname, Port: row.Port}
		instanceAnalysis := NewInstanceAnalysis(&instanceKey, AnalysisCode(row.Analysis))
		mapKey := instanceAnalysis.String()
		peerAnalysisMap[mapKey] = peerAnalysisMap[mapKey] + 1
	}
	return peerAnalysisMap, log.Errore(err)
}
