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

// Package recovery owns failure detection, recovery orchestration and history.
package recovery

import (
	"context"
	"fmt"
	"time"

	"github.com/openark/orchestrator/internal/attributes"
	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/golib/log"
	"github.com/openark/orchestrator/internal/inst/analysis"
	instaudit "github.com/openark/orchestrator/internal/inst/audit"
	instregroup "github.com/openark/orchestrator/internal/inst/change/regroup"
	instrelocation "github.com/openark/orchestrator/internal/inst/change/relocation"
	instreplication "github.com/openark/orchestrator/internal/inst/change/replication"
	instcluster "github.com/openark/orchestrator/internal/inst/cluster"
	instdowntime "github.com/openark/orchestrator/internal/inst/downtime"
	instmodel "github.com/openark/orchestrator/internal/inst/instance"
	instinventory "github.com/openark/orchestrator/internal/inst/inventory"
	instmaintenance "github.com/openark/orchestrator/internal/inst/maintenance"
	"github.com/openark/orchestrator/internal/kv"
	modeldomain "github.com/openark/orchestrator/internal/models/domain"
	orcraft "github.com/openark/orchestrator/internal/raft"
	"github.com/openark/orchestrator/internal/recoverypolicy"

	"github.com/openark/orchestrator/internal/observability"
)

func recoverDeadMasterInBinlogServerTopology(ctx context.Context, topologyRecovery *TopologyRecovery) (promotedReplica *instmodel.Instance, err error) {
	failedMasterKey := &topologyRecovery.AnalysisEntry.AnalyzedInstanceKey

	var promotedBinlogServer *instmodel.Instance

	_, promotedBinlogServer, err = instregroup.RegroupReplicasBinlogServers(ctx, failedMasterKey, true)
	if err != nil {
		return nil, log.Errore(err)
	}
	promotedBinlogServer, err = instreplication.StopReplication(&promotedBinlogServer.Key)
	if err != nil {
		return promotedReplica, log.Errore(err)
	}
	// Find candidate replica
	promotedReplica, err = instregroup.GetCandidateReplicaOfBinlogServerTopology(ctx, &promotedBinlogServer.Key)
	if err != nil {
		return promotedReplica, log.Errore(err)
	}
	// Align it with binlog server coordinates
	promotedReplica, err = instreplication.StopReplication(&promotedReplica.Key)
	if err != nil {
		return promotedReplica, log.Errore(err)
	}
	promotedReplica, err = instreplication.StartReplicationUntilMasterCoordinates(ctx, &promotedReplica.Key, &promotedBinlogServer.ExecBinlogCoordinates)
	if err != nil {
		return promotedReplica, log.Errore(err)
	}
	promotedReplica, err = instreplication.StopReplication(&promotedReplica.Key)
	if err != nil {
		return promotedReplica, log.Errore(err)
	}
	// Detach, flush binary logs forward
	promotedReplica, err = instreplication.ResetReplication(&promotedReplica.Key)
	if err != nil {
		return promotedReplica, log.Errore(err)
	}
	promotedReplica, err = instreplication.FlushBinaryLogsTo(&promotedReplica.Key, promotedBinlogServer.ExecBinlogCoordinates.LogFile)
	if err != nil {
		return promotedReplica, log.Errore(err)
	}
	promotedReplica, err = instreplication.FlushBinaryLogs(&promotedReplica.Key, 1)
	if err != nil {
		return promotedReplica, log.Errore(err)
	}
	promotedReplica, err = instrelocation.PurgeBinaryLogsToLatest(&promotedReplica.Key, false)
	if err != nil {
		return promotedReplica, log.Errore(err)
	}
	// Reconnect binlog servers to promoted replica (now master):
	promotedBinlogServer, err = instreplication.SkipToNextBinaryLog(ctx, &promotedBinlogServer.Key)
	if err != nil {
		return promotedReplica, log.Errore(err)
	}
	promotedBinlogServer, err = instrelocation.Repoint(ctx, &promotedBinlogServer.Key, &promotedReplica.Key, modeldomain.GTIDHintDeny)
	if err != nil {
		return nil, log.Errore(err)
	}

	func() {
		// Move binlog server replicas up to replicate from master.
		// This can only be done once a BLS has skipped to the next binlog
		// We postpone this operation. The master is already promoted and we're happy.
		binlogServerReplicas, err := instinventory.ReadBinlogServerReplicaInstances(&promotedBinlogServer.Key)
		if err != nil {
			return
		}
		maxBinlogServersToPromote := 3
		for i, binlogServerReplica := range binlogServerReplicas {
			if i >= maxBinlogServersToPromote {
				return
			}
			postponedFunction := func() error {
				binlogServerReplica, err := instreplication.StopReplication(&binlogServerReplica.Key)
				if err != nil {
					return err
				}
				// Make sure the BLS has the "next binlog" -- the one the master flushed & purged to. Otherwise the BLS
				// will request a binlog the master does not have
				if binlogServerReplica.ExecBinlogCoordinates.SmallerThan(&promotedBinlogServer.ExecBinlogCoordinates) {
					binlogServerReplica, err = instreplication.StartReplicationUntilMasterCoordinates(ctx, &binlogServerReplica.Key, &promotedBinlogServer.ExecBinlogCoordinates)
					if err != nil {
						return err
					}
				}
				_, err = instrelocation.Repoint(ctx, &binlogServerReplica.Key, &promotedReplica.Key, modeldomain.GTIDHintDeny)
				return err
			}
			topologyRecovery.AddPostponedFunction(postponedFunction, fmt.Sprintf("recoverDeadMasterInBinlogServerTopology, moving binlog server %+v", binlogServerReplica.Key))
		}
	}()

	return promotedReplica, err
}

func GetMasterRecoveryType(analysisEntry *analysis.ReplicationAnalysis) (masterRecoveryType MasterRecoveryType) {
	masterRecoveryType = MasterRecoveryPseudoGTID
	if analysisEntry.OracleGTIDImmediateTopology || analysisEntry.MariaDBGTIDImmediateTopology {
		masterRecoveryType = MasterRecoveryGTID
	} else if analysisEntry.BinlogServerImmediateTopology {
		masterRecoveryType = MasterRecoveryBinlogServer
	}
	return masterRecoveryType
}

func recoverDeadMasterContext(ctx context.Context, topologyRecovery *TopologyRecovery, candidateInstanceKey *instmodel.InstanceKey, skipProcesses bool) (recoveryAttempted bool, promotedReplica *instmodel.Instance, lostReplicas []*instmodel.Instance, err error) {
	ctx, span := observability.StartSpan(ctx, "recovery.execute")
	defer func() { observability.EndSpan(span, err) }()
	topologyRecovery.Type = MasterRecovery
	analysisEntry := &topologyRecovery.AnalysisEntry
	failedInstanceKey := &analysisEntry.AnalyzedInstanceKey
	var cannotReplicateReplicas []*instmodel.Instance
	postponedAll := false

	instaudit.AuditOperation("recover-dead-master", failedInstanceKey, "problem found; will recover")
	if !skipProcesses {
		if err := executeHookPhaseContext(ctx, "pre_failover", "PreFailoverProcesses", topologyRecovery); err != nil {
			return false, nil, lostReplicas, topologyRecovery.AddError(err)
		}
	}

	AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("RecoverDeadMaster: will recover %+v", *failedInstanceKey))

	topologyRecovery.RecoveryType = GetMasterRecoveryType(analysisEntry)
	AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("RecoverDeadMaster: masterRecoveryType=%+v", topologyRecovery.RecoveryType))

	promotedReplicaIsIdeal := func(promoted *instmodel.Instance, hasBestPromotionRule bool) bool {
		if promoted == nil {
			return false
		}
		AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("RecoverDeadMaster: promotedReplicaIsIdeal(%+v)", promoted.Key))
		if candidateInstanceKey != nil { //explicit request to promote a specific server
			return promoted.Key.Equals(candidateInstanceKey)
		}
		if promoted.DataCenter == topologyRecovery.AnalysisEntry.AnalyzedInstanceDataCenter &&
			promoted.PhysicalEnvironment == topologyRecovery.AnalysisEntry.AnalyzedInstancePhysicalEnvironment {
			if promoted.PromotionRule == instmodel.MustPromoteRule || promoted.PromotionRule == instmodel.PreferPromoteRule ||
				(hasBestPromotionRule && promoted.PromotionRule != instmodel.MustNotPromoteRule) {
				AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("RecoverDeadMaster: found %+v to be ideal candidate; will optimize recovery", promoted.Key))
				postponedAll = true
				return true
			}
		}
		return false
	}
	switch topologyRecovery.RecoveryType {
	case MasterRecoveryGTID:
		{
			AuditTopologyRecovery(topologyRecovery, "RecoverDeadMaster: regrouping replicas via GTID")
			lostReplicas, _, cannotReplicateReplicas, promotedReplica, err = instregroup.RegroupReplicasGTID(ctx, failedInstanceKey, true, false, nil, &topologyRecovery.PostponedFunctionsContainer, promotedReplicaIsIdeal)
		}
	case MasterRecoveryPseudoGTID:
		{
			AuditTopologyRecovery(topologyRecovery, "RecoverDeadMaster: regrouping replicas via Pseudo-GTID")
			lostReplicas, _, _, cannotReplicateReplicas, promotedReplica, err = instregroup.RegroupReplicasPseudoGTIDIncludingSubReplicasOfBinlogServers(ctx, failedInstanceKey, true, nil, &topologyRecovery.PostponedFunctionsContainer, promotedReplicaIsIdeal)
		}
	case MasterRecoveryBinlogServer:
		{
			AuditTopologyRecovery(topologyRecovery, "RecoverDeadMaster: recovering via binlog servers")
			promotedReplica, err = recoverDeadMasterInBinlogServerTopology(ctx, topologyRecovery)
		}
	}
	topologyRecovery.AddError(err)
	lostReplicas = append(lostReplicas, cannotReplicateReplicas...)
	for _, replica := range lostReplicas {
		AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("RecoverDeadMaster: - lost replica: %+v", replica.Key))
	}

	if promotedReplica != nil && len(lostReplicas) > 0 && recoverypolicy.FromContext(ctx, analysisEntry.ClusterDetails.ClusterName).DetachLostReplicasAfterMasterFailover {
		postponedFunction := func() error {
			AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("RecoverDeadMaster: lost %+v replicas during recovery process; detaching them", len(lostReplicas)))
			for _, replica := range lostReplicas {
				instrelocation.DetachReplicaMasterHost(ctx, &replica.Key)
			}
			return nil
		}
		topologyRecovery.AddPostponedFunction(postponedFunction, fmt.Sprintf("RecoverDeadMaster, detach %+v lost replicas", len(lostReplicas)))
	}

	func() error {
		instdowntime.BeginDowntime(instdowntime.NewDowntime(failedInstanceKey, instmaintenance.GetMaintenanceOwner(), instmodel.DowntimeLostInRecoveryMessage, time.Duration(config.LostInRecoveryDowntimeSeconds)*time.Second))
		acknowledgeInstanceFailureDetection(&analysisEntry.AnalyzedInstanceKey)
		for _, replica := range lostReplicas {
			instdowntime.BeginDowntime(instdowntime.NewDowntime(&replica.Key, instmaintenance.GetMaintenanceOwner(), instmodel.DowntimeLostInRecoveryMessage, time.Duration(config.LostInRecoveryDowntimeSeconds)*time.Second))
		}
		return nil
	}()

	AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("RecoverDeadMaster: %d postponed functions", topologyRecovery.PostponedFunctionsContainer.Len()))

	if promotedReplica != nil && !postponedAll {
		promotedReplica, err = replacePromotedReplicaWithCandidate(ctx, topologyRecovery, &analysisEntry.AnalyzedInstanceKey, promotedReplica, candidateInstanceKey)
		topologyRecovery.AddError(err)
	}

	if promotedReplica == nil {
		message := "Failure: no replica promoted."
		AuditTopologyRecovery(topologyRecovery, message)
		instaudit.AuditOperation("recover-dead-master", failedInstanceKey, message)
	} else {
		message := fmt.Sprintf("promoted replica: %+v", promotedReplica.Key)
		AuditTopologyRecovery(topologyRecovery, message)
		instaudit.AuditOperation("recover-dead-master", failedInstanceKey, message)
	}
	return true, promotedReplica, lostReplicas, err
}

// checkAndRecoverDeadMaster checks a given analysis, decides whether to take action, and possibly takes action
// Returns true when action was taken.
func checkAndRecoverDeadMaster(ctx context.Context, analysisEntry analysis.ReplicationAnalysis, candidateInstanceKey *instmodel.InstanceKey, forceInstanceRecovery bool, skipProcesses bool) (recoveryAttempted bool, topologyRecovery *TopologyRecovery, err error) {
	policy := recoverypolicy.FromContext(ctx, analysisEntry.ClusterDetails.ClusterName)
	if !(forceInstanceRecovery || analysisEntry.ClusterDetails.HasAutomatedMasterRecovery) {
		return false, nil, nil
	}
	topologyRecovery, err = AttemptRecoveryRegistration(ctx, &analysisEntry, !forceInstanceRecovery, !forceInstanceRecovery)
	if topologyRecovery == nil {
		AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("found an active or recent recovery on %+v. Will not issue another RecoverDeadMaster.", analysisEntry.AnalyzedInstanceKey))
		return false, nil, err
	}

	// That's it! We must do recovery!
	AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("will handle DeadMaster event on %+v", analysisEntry.ClusterDetails.ClusterName))
	recoveryCtx, finishRecovery := observability.BeginRecovery(ctx, "dead_master")
	recoveryResult := "failure"
	defer func() { finishRecovery(recoveryResult) }()
	recoveryAttempted, promotedReplica, lostReplicas, err := recoverDeadMasterContext(recoveryCtx, topologyRecovery, candidateInstanceKey, skipProcesses)
	if err != nil {
		AuditTopologyRecovery(topologyRecovery, err.Error())
	}
	topologyRecovery.LostReplicas.AddInstances(lostReplicas)
	if !recoveryAttempted {
		return false, topologyRecovery, err
	}

	overrideMasterPromotion := func() (*instmodel.Instance, error) {
		if promotedReplica == nil {
			// No promotion; nothing to override.
			return promotedReplica, err
		}
		// Scenarios where we might cancel the promotion.
		if satisfied, reason := MasterFailoverGeographicConstraintSatisfied(ctx, &analysisEntry, promotedReplica); !satisfied {
			return nil, fmt.Errorf("RecoverDeadMaster: failed %+v promotion; %s", promotedReplica.Key, reason)
		}
		AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("RecoverDeadMaster: promoted replica lag seconds: %+v", promotedReplica.ReplicationLagSeconds.Int64))
		if policy.FailMasterPromotionOnLagMinutes > 0 &&
			time.Duration(promotedReplica.ReplicationLagSeconds.Int64)*time.Second >= time.Duration(policy.FailMasterPromotionOnLagMinutes)*time.Minute {
			// candidate replica lags too much
			return nil, fmt.Errorf("RecoverDeadMaster: failed promotion. FailMasterPromotionOnLagMinutes is set to %d (minutes) and promoted replica %+v 's lag is %d (seconds)", policy.FailMasterPromotionOnLagMinutes, promotedReplica.Key, promotedReplica.ReplicationLagSeconds.Int64)
		}
		AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("RecoverDeadMaster: promoted replica sql thread up-to-date: %+v", promotedReplica.SQLThreadUpToDate()))
		if policy.SQLThreadPromotionPolicy == "reject" && !promotedReplica.SQLThreadUpToDate() {
			return nil, fmt.Errorf("RecoverDeadMaster: failed promotion. FailMasterPromotionIfSQLThreadNotUpToDate is set and promoted replica %+v 's sql thread is not up to date (relay logs still unapplied). Aborting promotion", promotedReplica.Key)
		}
		if policy.SQLThreadPromotionPolicy == "wait" && !promotedReplica.SQLThreadUpToDate() {
			AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("DelayMasterPromotionIfSQLThreadNotUpToDate: waiting for SQL thread on %+v", promotedReplica.Key))
			if _, err := instreplication.WaitForSQLThreadUpToDate(ctx, &promotedReplica.Key, 0, 0); err != nil {
				return nil, fmt.Errorf("DelayMasterPromotionIfSQLThreadNotUpToDate error: %+v", err)
			}
			AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("DelayMasterPromotionIfSQLThreadNotUpToDate: SQL thread caught up on %+v", promotedReplica.Key))
		}
		// All seems well. No override done.
		AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("RecoverDeadMaster: found no reason to override promotion of %+v", promotedReplica.Key))
		return promotedReplica, err
	}
	if promotedReplica, err = overrideMasterPromotion(); err != nil {
		AuditTopologyRecovery(topologyRecovery, err.Error())
	}
	// And this is the end; whether successful or not, we're done.
	resolveRecovery(topologyRecovery, promotedReplica)
	// Now, see whether we are successful or not. From this point there's no going back.
	if promotedReplica != nil {
		// Success!
		recoveryResult = "success"
		AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("RecoverDeadMaster: successfully promoted %+v", promotedReplica.Key))
		AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("- RecoverDeadMaster: promoted server coordinates: %+v", promotedReplica.SelfBinlogCoordinates))

		if policy.ApplyMySQLPromotionAfterMasterFailover || analysisEntry.CommandHint == analysis.GracefulMasterTakeoverCommandHint {
			// on GracefulMasterTakeoverCommandHint it makes utter sense to RESET SLAVE ALL and read_only=0, and there is no sense in not doing so.
			AuditTopologyRecovery(topologyRecovery, "- RecoverDeadMaster: will apply MySQL changes to promoted master")
			{
				_, err := instrelocation.ResetReplicationOperation(ctx, &promotedReplica.Key)
				if err != nil {
					// Ugly, but this is important. Let's give it another try
					_, err = instrelocation.ResetReplicationOperation(ctx, &promotedReplica.Key)
				}
				AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("- RecoverDeadMaster: applying RESET SLAVE ALL on promoted master: success=%t", (err == nil)))
				if err != nil {
					AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("- RecoverDeadMaster: NOTE that %+v is promoted even though SHOW SLAVE STATUS may still show it has a master", promotedReplica.Key))
				}
			}
			{
				_, err := instreplication.SetReadOnly(&promotedReplica.Key, false)
				AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("- RecoverDeadMaster: applying read-only=0 on promoted master: success=%t", (err == nil)))
			}
			// Let's attempt, though we won't necessarily succeed, to set old master as read-only
			go func() {
				_, err := instreplication.SetReadOnly(&analysisEntry.AnalyzedInstanceKey, true)
				AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("- RecoverDeadMaster: applying read-only=1 on demoted master: success=%t", (err == nil)))
			}()
		}

		kvPairs := instcluster.GetClusterMasterKVPairs(analysisEntry.ClusterDetails.ClusterAlias, &promotedReplica.Key)
		AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("Writing KV %+v", kvPairs))

		for _, kvPair := range kvPairs {
			_, err := orcraft.PublishCommand("put-key-value", kvPair)
			log.Errore(err)
		}
		// since we'll be affecting 3rd party tools here, we _prefer_ to mitigate re-applying
		// of the put-key-value event upon startup. We _recommend_ a snapshot in the near future.
		go orcraft.PublishCommand("async-snapshot", "")

		{
			AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("Distributing KV %+v", kvPairs))
			err := kv.DistributePairs(kvPairs)
			log.Errore(err)
		}
		if policy.MasterFailoverDetachReplicaMasterHost {
			postponedFunction := func() error {
				AuditTopologyRecovery(topologyRecovery, "- RecoverDeadMaster: detaching master host on promoted master")
				instrelocation.DetachReplicaMasterHost(ctx, &promotedReplica.Key)
				return nil
			}
			topologyRecovery.AddPostponedFunction(postponedFunction, fmt.Sprintf("RecoverDeadMaster, detaching promoted master host %+v", promotedReplica.Key))
		}
		func() error {
			before := analysisEntry.AnalyzedInstanceKey.StringCode()
			after := promotedReplica.Key.StringCode()
			AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("- RecoverDeadMaster: updating cluster_alias: %v -> %v", before, after))
			//~~~inst.ReplaceClusterName(before, after)
			if alias := analysisEntry.ClusterDetails.ClusterAlias; alias != "" {
				instcluster.SetClusterAlias(promotedReplica.Key.StringCode(), alias)
			} else {
				instcluster.ReplaceAliasClusterName(before, after)
			}
			return nil
		}()

		attributes.SetGeneralAttribute(analysisEntry.ClusterDetails.ClusterDomain, promotedReplica.Key.StringCode())

		if !skipProcesses {
			// Execute post master-failover processes
			_ = executeHookPhaseContext(recoveryCtx, "post_master_failover", "PostMasterFailoverProcesses", topologyRecovery)
		}
	} else {
		recoveryResult = "failure"
	}

	return true, topologyRecovery, err
}
