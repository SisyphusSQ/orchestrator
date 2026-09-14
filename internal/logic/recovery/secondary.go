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
	"errors"
	"fmt"
	"math/rand"
	"time"

	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/golib/log"
	"github.com/openark/orchestrator/internal/inst/analysis"
	instaudit "github.com/openark/orchestrator/internal/inst/audit"
	instregroup "github.com/openark/orchestrator/internal/inst/change/regroup"
	instrelocation "github.com/openark/orchestrator/internal/inst/change/relocation"
	instreplication "github.com/openark/orchestrator/internal/inst/change/replication"
	instdowntime "github.com/openark/orchestrator/internal/inst/downtime"
	instmodel "github.com/openark/orchestrator/internal/inst/instance"
	instinventory "github.com/openark/orchestrator/internal/inst/inventory"
	instmaintenance "github.com/openark/orchestrator/internal/inst/maintenance"
	"github.com/openark/orchestrator/internal/recoverypolicy"

	"github.com/openark/orchestrator/internal/observability"
)

// RecoverDeadIntermediateMaster performs intermediate master recovery; complete logic inside
func RecoverDeadIntermediateMaster(ctx context.Context, topologyRecovery *TopologyRecovery, skipProcesses bool) (successorInstance *instmodel.Instance, err error) {
	return RecoverDeadIntermediateMasterContext(ctx, topologyRecovery, skipProcesses)
}

func RecoverDeadIntermediateMasterContext(ctx context.Context, topologyRecovery *TopologyRecovery, skipProcesses bool) (successorInstance *instmodel.Instance, err error) {
	ctx, span := observability.StartSpan(ctx, "recovery.execute")
	defer func() { observability.EndSpan(span, err) }()
	topologyRecovery.Type = IntermediateMasterRecovery
	analysisEntry := &topologyRecovery.AnalysisEntry
	failedInstanceKey := &analysisEntry.AnalyzedInstanceKey
	recoveryResolved := false

	instaudit.AuditOperation("recover-dead-intermediate-master", failedInstanceKey, "problem found; will recover")
	if !skipProcesses {
		if err := executeHookPhaseContext(ctx, "pre_failover", "PreFailoverProcesses", topologyRecovery); err != nil {
			return nil, topologyRecovery.AddError(err)
		}
	}

	intermediateMasterInstance, _, err := instinventory.ReadInstance(failedInstanceKey)
	if err != nil {
		return nil, topologyRecovery.AddError(err)
	}
	// Find possible candidate
	candidateSiblingOfIntermediateMaster, _ := GetCandidateSiblingOfIntermediateMaster(ctx, topologyRecovery, intermediateMasterInstance)
	relocateReplicasToCandidateSibling := func() {
		if candidateSiblingOfIntermediateMaster == nil {
			return
		}
		// We have a candidate
		AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("- RecoverDeadIntermediateMaster: will attempt a candidate intermediate master: %+v", candidateSiblingOfIntermediateMaster.Key))
		relocatedReplicas, candidateSibling, errs, err := instrelocation.RelocateReplicas(ctx, failedInstanceKey, &candidateSiblingOfIntermediateMaster.Key, "")
		topologyRecovery.AddErrors(errs)
		topologyRecovery.ParticipatingInstanceKeys.AddKey(candidateSiblingOfIntermediateMaster.Key)

		if len(relocatedReplicas) == 0 {
			AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("- RecoverDeadIntermediateMaster: failed to move any replica to candidate intermediate master (%+v)", candidateSibling.Key))
			return
		}
		if err != nil || len(errs) > 0 {
			AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("- RecoverDeadIntermediateMaster: move to candidate intermediate master (%+v) did not complete: err: %+v, errs: %+v", candidateSibling.Key, err, errs))
			return
		}
		if err == nil {
			recoveryResolved = true
			successorInstance = candidateSibling

			instaudit.AuditOperation("recover-dead-intermediate-master", failedInstanceKey, fmt.Sprintf("Relocated %d replicas under candidate sibling: %+v; %d errors: %+v", len(relocatedReplicas), candidateSibling.Key, len(errs), errs))
		}
	}
	// Plan A: find a replacement intermediate master in same Data Center
	if candidateSiblingOfIntermediateMaster != nil && candidateSiblingOfIntermediateMaster.DataCenter == intermediateMasterInstance.DataCenter {
		relocateReplicasToCandidateSibling()
	}
	if !recoveryResolved {
		AuditTopologyRecovery(topologyRecovery, "- RecoverDeadIntermediateMaster: will next attempt regrouping of replicas")
		// Plan B: regroup (we wish to reduce cross-DC replication streams)
		lostReplicas, _, _, _, regroupPromotedReplica, regroupError := instregroup.RegroupReplicas(ctx, failedInstanceKey, true, nil, nil)
		if regroupError != nil {
			topologyRecovery.AddError(regroupError)
			AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("- RecoverDeadIntermediateMaster: regroup failed on: %+v", regroupError))
		}
		if regroupPromotedReplica != nil {
			AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("- RecoverDeadIntermediateMaster: regrouped under %+v, with %d lost replicas", regroupPromotedReplica.Key, len(lostReplicas)))
			topologyRecovery.ParticipatingInstanceKeys.AddKey(regroupPromotedReplica.Key)
			if len(lostReplicas) == 0 && regroupError == nil {
				// Seems like the regroup worked flawlessly. The local replica took over all of its siblings.
				// We can consider this host to be the successor.
				successorInstance = regroupPromotedReplica
			}
		}
		// Plan C: try replacement intermediate master in other DC...
		if candidateSiblingOfIntermediateMaster != nil && candidateSiblingOfIntermediateMaster.DataCenter != intermediateMasterInstance.DataCenter {
			AuditTopologyRecovery(topologyRecovery, "- RecoverDeadIntermediateMaster: will next attempt relocating to another DC server")
			relocateReplicasToCandidateSibling()
		}
	}
	if !recoveryResolved {
		// Do we still have leftovers? some replicas couldn't move? Couldn't regroup? Only left with regroup's resulting leader?
		// nothing moved?
		// We don't care much if regroup made it or not. We prefer that it made it, in which case we only need to relocate up
		// one replica, but the operation is still valid if regroup partially/completely failed. We just promote anything
		// not regrouped.
		// So, match up all that's left, plan D
		AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("- RecoverDeadIntermediateMaster: will next attempt to relocate up from %+v", *failedInstanceKey))

		relocatedReplicas, masterInstance, errs, err := instrelocation.RelocateReplicas(ctx, failedInstanceKey, &analysisEntry.AnalyzedInstanceMasterKey, "")
		topologyRecovery.AddError(err)
		topologyRecovery.AddErrors(errs)
		topologyRecovery.ParticipatingInstanceKeys.AddKey(analysisEntry.AnalyzedInstanceMasterKey)

		if len(relocatedReplicas) > 0 {
			recoveryResolved = true
			if successorInstance == nil {
				// There could have been a local replica taking over its siblings. We'd like to consider that one as successor.
				successorInstance = masterInstance
			}
			instaudit.AuditOperation("recover-dead-intermediate-master", failedInstanceKey, fmt.Sprintf("Relocated replicas under: %+v %d errors: %+v", successorInstance.Key, len(errs), errs))
		} else {
			err = log.Errorf("topology_recovery: RecoverDeadIntermediateMaster failed to match up any replica from %+v", *failedInstanceKey)
			topologyRecovery.AddError(err)
		}
	}
	if !recoveryResolved {
		successorInstance = nil
	}
	resolveRecovery(topologyRecovery, successorInstance)
	return successorInstance, err
}

// RecoverDeadReplicationGroupMemberWithReplicas performs dead group member recovery. It does so by finding members of
// the same replication group of the one of the failed instance, picking a random one and relocating replicas to it.
func RecoverDeadReplicationGroupMemberWithReplicas(ctx context.Context, topologyRecovery *TopologyRecovery, skipProcesses bool) (successorInstance *instmodel.Instance, err error) {
	topologyRecovery.Type = ReplicationGroupMemberRecovery
	analysisEntry := &topologyRecovery.AnalysisEntry
	failedGroupMemberInstanceKey := &analysisEntry.AnalyzedInstanceKey
	instaudit.AuditOperation("recover-dead-replication-group-member-with-replicas", failedGroupMemberInstanceKey, "problem found; will recover")
	if !skipProcesses {
		if err := executeHookPhaseContext(ctx, "pre_failover", "PreFailoverProcesses", topologyRecovery); err != nil {
			return nil, topologyRecovery.AddError(err)
		}
	}
	failedGroupMember, _, err := instinventory.ReadInstance(failedGroupMemberInstanceKey)
	if err != nil {
		return nil, topologyRecovery.AddError(err)
	}
	// Find a group member under which we can relocate the replicas of the failed one.
	groupMembers := failedGroupMember.ReplicationGroupMembers.GetInstanceKeys()
	if len(groupMembers) == 0 {
		return nil, topologyRecovery.AddError(errors.New("RecoverDeadReplicationGroupMemberWithReplicas: unable to find a candidate group member to relocate replicas to"))
	}
	// We have a group member to move replicas to, go ahead and do that
	AuditTopologyRecovery(topologyRecovery, "Finding a candidate group member to relocate replicas to")
	candidateGroupMemberInstanceKey := &groupMembers[rand.Intn(len(failedGroupMember.ReplicationGroupMembers.GetInstanceKeys()))]
	AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("Found group member %+v", candidateGroupMemberInstanceKey))
	relocatedReplicas, successorInstance, errs, err := instrelocation.RelocateReplicas(ctx, failedGroupMemberInstanceKey, candidateGroupMemberInstanceKey, "")
	topologyRecovery.AddErrors(errs)
	if len(relocatedReplicas) != len(failedGroupMember.Replicas.GetInstanceKeys()) {
		AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("- RecoverDeadReplicationGroupMemberWithReplicas: failed to move all replicas to candidate group member (%+v)", candidateGroupMemberInstanceKey))
		return nil, topologyRecovery.AddError(fmt.Errorf("RecoverDeadReplicationGroupMemberWithReplicas: Unable to relocate replicas to +%v", candidateGroupMemberInstanceKey))
	}
	AuditTopologyRecovery(topologyRecovery, "All replicas successfully relocated")
	resolveRecovery(topologyRecovery, successorInstance)
	return successorInstance, err
}

// checkAndRecoverDeadIntermediateMaster checks a given analysis, decides whether to take action, and possibly takes action
// Returns true when action was taken.
func checkAndRecoverDeadIntermediateMaster(ctx context.Context, analysisEntry analysis.ReplicationAnalysis, candidateInstanceKey *instmodel.InstanceKey, forceInstanceRecovery bool, skipProcesses bool) (bool, *TopologyRecovery, error) {
	if !(forceInstanceRecovery || analysisEntry.ClusterDetails.HasAutomatedIntermediateMasterRecovery) {
		return false, nil, nil
	}
	topologyRecovery, err := AttemptRecoveryRegistration(ctx, &analysisEntry, !forceInstanceRecovery, !forceInstanceRecovery)
	if topologyRecovery == nil {
		AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("- RecoverDeadIntermediateMaster: found an active or recent recovery on %+v. Will not issue another RecoverDeadIntermediateMaster.", analysisEntry.AnalyzedInstanceKey))
		return false, nil, err
	}

	// That's it! We must do recovery!
	recoveryCtx, finishRecovery := observability.BeginRecovery(ctx, "dead_intermediate_master")
	recoveryResult := "failure"
	defer func() { finishRecovery(recoveryResult) }()
	promotedReplica, err := RecoverDeadIntermediateMasterContext(recoveryCtx, topologyRecovery, skipProcesses)
	if promotedReplica != nil {
		// success
		recoveryResult = "success"

		if !skipProcesses {
			// Execute post intermediate-master-failover processes
			topologyRecovery.SuccessorKey = &promotedReplica.Key
			topologyRecovery.SuccessorAlias = promotedReplica.InstanceAlias
			_ = executeHookPhaseContext(recoveryCtx, "post_intermediate_master_failover", "PostIntermediateMasterFailoverProcesses", topologyRecovery)
		}
	} else {
		recoveryResult = "failure"
	}
	return true, topologyRecovery, err
}

// RecoverDeadCoMaster recovers a dead co-master, complete logic inside
func RecoverDeadCoMaster(ctx context.Context, topologyRecovery *TopologyRecovery, skipProcesses bool) (promotedReplica *instmodel.Instance, lostReplicas []*instmodel.Instance, err error) {
	return RecoverDeadCoMasterContext(ctx, topologyRecovery, skipProcesses)
}

func RecoverDeadCoMasterContext(ctx context.Context, topologyRecovery *TopologyRecovery, skipProcesses bool) (promotedReplica *instmodel.Instance, lostReplicas []*instmodel.Instance, err error) {
	ctx, span := observability.StartSpan(ctx, "recovery.execute")
	defer func() { observability.EndSpan(span, err) }()
	topologyRecovery.Type = CoMasterRecovery
	analysisEntry := &topologyRecovery.AnalysisEntry
	policy := recoverypolicy.FromContext(ctx, analysisEntry.ClusterDetails.ClusterName)
	failedInstanceKey := &analysisEntry.AnalyzedInstanceKey
	otherCoMasterKey := &analysisEntry.AnalyzedInstanceMasterKey
	otherCoMaster, found, _ := instinventory.ReadInstance(otherCoMasterKey)
	if otherCoMaster == nil || !found {
		return nil, lostReplicas, topologyRecovery.AddError(log.Errorf("RecoverDeadCoMaster: could not read info for co-master %+v of %+v", *otherCoMasterKey, *failedInstanceKey))
	}
	instaudit.AuditOperation("recover-dead-co-master", failedInstanceKey, "problem found; will recover")
	if !skipProcesses {
		if err := executeHookPhaseContext(ctx, "pre_failover", "PreFailoverProcesses", topologyRecovery); err != nil {
			return nil, lostReplicas, topologyRecovery.AddError(err)
		}
	}

	AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("RecoverDeadCoMaster: will recover %+v", *failedInstanceKey))

	var coMasterRecoveryType MasterRecoveryType = MasterRecoveryPseudoGTID
	if analysisEntry.OracleGTIDImmediateTopology || analysisEntry.MariaDBGTIDImmediateTopology {
		coMasterRecoveryType = MasterRecoveryGTID
	}

	AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("RecoverDeadCoMaster: coMasterRecoveryType=%+v", coMasterRecoveryType))

	var cannotReplicateReplicas []*instmodel.Instance
	switch coMasterRecoveryType {
	case MasterRecoveryGTID:
		{
			lostReplicas, _, cannotReplicateReplicas, promotedReplica, err = instregroup.RegroupReplicasGTID(ctx, failedInstanceKey, true, false, nil, &topologyRecovery.PostponedFunctionsContainer, nil)
		}
	case MasterRecoveryPseudoGTID:
		{
			lostReplicas, _, _, cannotReplicateReplicas, promotedReplica, err = instregroup.RegroupReplicasPseudoGTIDIncludingSubReplicasOfBinlogServers(ctx, failedInstanceKey, true, nil, &topologyRecovery.PostponedFunctionsContainer, nil)
		}
	}
	topologyRecovery.AddError(err)
	lostReplicas = append(lostReplicas, cannotReplicateReplicas...)

	mustPromoteOtherCoMaster := policy.CoMasterRecoveryMustPromoteOtherCoMaster
	if !otherCoMaster.ReadOnly {
		AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("RecoverDeadCoMaster: other co-master %+v is writeable hence has to be promoted", otherCoMaster.Key))
		mustPromoteOtherCoMaster = true
	}
	AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("RecoverDeadCoMaster: mustPromoteOtherCoMaster? %+v", mustPromoteOtherCoMaster))

	if promotedReplica != nil {
		topologyRecovery.ParticipatingInstanceKeys.AddKey(promotedReplica.Key)
		if mustPromoteOtherCoMaster {
			AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("RecoverDeadCoMaster: mustPromoteOtherCoMaster. Verifying that %+v is/can be promoted", *otherCoMasterKey))
			promotedReplica, err = replacePromotedReplicaWithCandidate(ctx, topologyRecovery, failedInstanceKey, promotedReplica, otherCoMasterKey)
		} else {
			// We are allowed to promote any server
			promotedReplica, err = replacePromotedReplicaWithCandidate(ctx, topologyRecovery, failedInstanceKey, promotedReplica, nil)
		}
		topologyRecovery.AddError(err)
	}
	if promotedReplica != nil {
		if mustPromoteOtherCoMaster && !promotedReplica.Key.Equals(otherCoMasterKey) {
			topologyRecovery.AddError(log.Errorf("RecoverDeadCoMaster: could not manage to promote other-co-master %+v; was only able to promote %+v; mustPromoteOtherCoMaster is true (either CoMasterRecoveryMustPromoteOtherCoMaster is true, or co-master is writeable), therefore failing", *otherCoMasterKey, promotedReplica.Key))
			promotedReplica = nil
		}
	}
	if promotedReplica != nil {
		if policy.SQLThreadPromotionPolicy == "wait" {
			AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("Waiting to ensure the SQL thread catches up on %+v", promotedReplica.Key))
			if _, err := instreplication.WaitForSQLThreadUpToDate(ctx, &promotedReplica.Key, 0, 0); err != nil {
				return promotedReplica, lostReplicas, err
			}
			AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("SQL thread caught up on %+v", promotedReplica.Key))
		}
		topologyRecovery.ParticipatingInstanceKeys.AddKey(promotedReplica.Key)
	}

	// OK, we may have someone promoted. Either this was the other co-master or another replica.
	// Noting down that we DO NOT attempt to set a new co-master topology. We are good with remaining with a single master.
	// I tried solving the "let's promote a replica and create a new co-master setup" but this turns so complex due to various factors.
	// I see this as risky and not worth the questionable benefit.
	// Maybe future me is a smarter person and finds a simple solution. Unlikely. I'm getting dumber.
	//
	// ...
	// Now that we're convinced, take a look at what we can be left with:
	// Say we started with M1<->M2<-S1, with M2 failing, and we promoted S1.
	// We now have M1->S1 (because S1 is promoted), S1->M2 (because that's what it remembers), M2->M1 (because that's what it remembers)
	// !! This is an evil 3-node circle that must be broken.
	// config.Current().ApplyMySQLPromotionAfterMasterFailover, if true, will cause it to break, because we would RESET SLAVE on S1
	// but we want to make sure the circle is broken no matter what.
	// So in the case we promoted not-the-other-co-master, we issue a detach-replica-master-host, which is a reversible operation
	if promotedReplica != nil && !promotedReplica.Key.Equals(otherCoMasterKey) {
		_, err = instrelocation.DetachReplicaMasterHost(ctx, &promotedReplica.Key)
		topologyRecovery.AddError(log.Errore(err))
	}

	if promotedReplica != nil && len(lostReplicas) > 0 && policy.DetachLostReplicasAfterMasterFailover {
		postponedFunction := func() error {
			AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("- RecoverDeadCoMaster: lost %+v replicas during recovery process; detaching them", len(lostReplicas)))
			for _, replica := range lostReplicas {
				instrelocation.DetachReplicaMasterHost(ctx, &replica.Key)
			}
			return nil
		}
		topologyRecovery.AddPostponedFunction(postponedFunction, fmt.Sprintf("RecoverDeadCoMaster, detaching %+v replicas", len(lostReplicas)))
	}

	func() error {
		instdowntime.BeginDowntime(instdowntime.NewDowntime(failedInstanceKey, instmaintenance.GetMaintenanceOwner(), instmodel.DowntimeLostInRecoveryMessage, time.Duration(config.LostInRecoveryDowntimeSeconds)*time.Second))
		acknowledgeInstanceFailureDetection(&analysisEntry.AnalyzedInstanceKey)
		for _, replica := range lostReplicas {
			instdowntime.BeginDowntime(instdowntime.NewDowntime(&replica.Key, instmaintenance.GetMaintenanceOwner(), instmodel.DowntimeLostInRecoveryMessage, time.Duration(config.LostInRecoveryDowntimeSeconds)*time.Second))
		}
		return nil
	}()

	return promotedReplica, lostReplicas, err
}

// checkAndRecoverDeadCoMaster checks a given analysis, decides whether to take action, and possibly takes action
// Returns true when action was taken.
func checkAndRecoverDeadCoMaster(ctx context.Context, analysisEntry analysis.ReplicationAnalysis, candidateInstanceKey *instmodel.InstanceKey, forceInstanceRecovery bool, skipProcesses bool) (bool, *TopologyRecovery, error) {
	policy := recoverypolicy.FromContext(ctx, analysisEntry.ClusterDetails.ClusterName)
	failedInstanceKey := &analysisEntry.AnalyzedInstanceKey
	if !(forceInstanceRecovery || analysisEntry.ClusterDetails.HasAutomatedMasterRecovery) {
		return false, nil, nil
	}
	topologyRecovery, err := AttemptRecoveryRegistration(ctx, &analysisEntry, !forceInstanceRecovery, !forceInstanceRecovery)
	if topologyRecovery == nil {
		AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("found an active or recent recovery on %+v. Will not issue another RecoverDeadCoMaster.", analysisEntry.AnalyzedInstanceKey))
		return false, nil, err
	}

	// That's it! We must do recovery!
	recoveryCtx, finishRecovery := observability.BeginRecovery(ctx, "dead_co_master")
	recoveryResult := "failure"
	defer func() { finishRecovery(recoveryResult) }()
	promotedReplica, lostReplicas, err := RecoverDeadCoMasterContext(recoveryCtx, topologyRecovery, skipProcesses)
	resolveRecovery(topologyRecovery, promotedReplica)
	if promotedReplica == nil {
		instaudit.AuditOperation("recover-dead-co-master", failedInstanceKey, "Failure: no replica promoted.")
	} else {
		instaudit.AuditOperation("recover-dead-co-master", failedInstanceKey, fmt.Sprintf("promoted: %+v", promotedReplica.Key))
	}
	topologyRecovery.LostReplicas.AddInstances(lostReplicas)
	if promotedReplica != nil {
		if policy.SQLThreadPromotionPolicy == "reject" && !promotedReplica.SQLThreadUpToDate() {
			return false, nil, log.Errorf("Promoted replica %+v: sql thread is not up to date (relay logs still unapplied). Aborting promotion", promotedReplica.Key)
		}
		// success
		recoveryResult = "success"

		if policy.ApplyMySQLPromotionAfterMasterFailover {
			AuditTopologyRecovery(topologyRecovery, "- RecoverDeadMaster: will apply MySQL changes to promoted master")
			instreplication.SetReadOnly(&promotedReplica.Key, false)
		}
		if !skipProcesses {
			// Execute post intermediate-master-failover processes
			topologyRecovery.SuccessorKey = &promotedReplica.Key
			topologyRecovery.SuccessorAlias = promotedReplica.InstanceAlias
			_ = executeHookPhaseContext(recoveryCtx, "post_master_failover", "PostMasterFailoverProcesses", topologyRecovery)
		}
	} else {
		recoveryResult = "failure"
	}
	return true, topologyRecovery, err
}

// checkAndRecoverDeadGroupMemberWithReplicas checks whether action needs to be taken for an analysis involving a dead
// replication group member, and takes the action if applicable. Notice that under our view of the world, a primary
// replication group member is akin to a master in traditional async/semisync replication; whereas secondary group
// members are akin to intermediate masters. Considering also that a failed group member can always be considered as a
// secondary (even if it was primary, the group should have detected its failure and elected a new primary), then
// failure of a group member with replicas is akin to failure of an intermediate master.
func checkAndRecoverDeadGroupMemberWithReplicas(ctx context.Context, analysisEntry analysis.ReplicationAnalysis, candidateInstanceKey *instmodel.InstanceKey, forceInstanceRecovery bool, skipProcesses bool) (bool, *TopologyRecovery, error) {
	// Don't proceed with recovery unless it was forced or automatic intermediate source recovery is enabled.
	// We consider failed group members akin to failed intermediate masters, so we re-use the configuration for
	// intermediates.
	if !(forceInstanceRecovery || analysisEntry.ClusterDetails.HasAutomatedIntermediateMasterRecovery) {
		return false, nil, nil
	}
	// Try to record the recovery. It it fails to be recorded, it because it is already being dealt with.
	topologyRecovery, err := AttemptRecoveryRegistration(ctx, &analysisEntry, !forceInstanceRecovery, !forceInstanceRecovery)
	if err != nil {
		return false, nil, err
	}
	// Proceed with recovery
	recoveryCtx, finishRecovery := observability.BeginRecovery(ctx, "dead_replication_group_member")
	recoveryResult := "failure"
	defer func() { finishRecovery(recoveryResult) }()

	recoveredToGroupMember, err := RecoverDeadReplicationGroupMemberWithReplicas(ctx, topologyRecovery, skipProcesses)

	if recoveredToGroupMember != nil {
		// success
		recoveryResult = "success"

		if !skipProcesses {
			// Execute post failover processes
			topologyRecovery.SuccessorKey = &recoveredToGroupMember.Key
			topologyRecovery.SuccessorAlias = recoveredToGroupMember.InstanceAlias
			// For the same reasons that were mentioned above, we re-use the post intermediate master fail-over hooks
			_ = executeHookPhaseContext(recoveryCtx, "post_intermediate_master_failover", "PostIntermediateMasterFailoverProcesses", topologyRecovery)
		}
	} else {
		recoveryResult = "failure"
	}
	return true, topologyRecovery, err
}
