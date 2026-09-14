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
	"sort"

	"github.com/openark/orchestrator/internal/golib/log"
	"github.com/openark/orchestrator/internal/inst/analysis"
	instregroup "github.com/openark/orchestrator/internal/inst/change/regroup"
	instrelocation "github.com/openark/orchestrator/internal/inst/change/relocation"
	instreplication "github.com/openark/orchestrator/internal/inst/change/replication"
	instmodel "github.com/openark/orchestrator/internal/inst/instance"
	instinventory "github.com/openark/orchestrator/internal/inst/inventory"
	"github.com/openark/orchestrator/internal/recoverypolicy"
)

func MasterFailoverGeographicConstraintSatisfied(ctx context.Context, analysisEntry *analysis.ReplicationAnalysis, suggestedInstance *instmodel.Instance) (satisfied bool, dissatisfiedReason string) {
	policy := recoverypolicy.FromContext(ctx, analysisEntry.ClusterDetails.ClusterName)
	if policy.PreventCrossDataCenterMasterFailover {
		if suggestedInstance.DataCenter != analysisEntry.AnalyzedInstanceDataCenter {
			return false, fmt.Sprintf("PreventCrossDataCenterMasterFailover: will not promote server in %s when failed server in %s", suggestedInstance.DataCenter, analysisEntry.AnalyzedInstanceDataCenter)
		}
	}
	if policy.PreventCrossRegionMasterFailover {
		if suggestedInstance.Region != analysisEntry.AnalyzedInstanceRegion {
			return false, fmt.Sprintf("PreventCrossRegionMasterFailover: will not promote server in %s when failed server in %s", suggestedInstance.Region, analysisEntry.AnalyzedInstanceRegion)
		}
	}
	return true, ""
}

// SuggestReplacementForPromotedReplica returns a server to take over the already
// promoted replica, if such server is found and makes an improvement over the promoted replica.
func SuggestReplacementForPromotedReplica(ctx context.Context, topologyRecovery *TopologyRecovery, deadInstanceKey *instmodel.InstanceKey, promotedReplica *instmodel.Instance, candidateInstanceKey *instmodel.InstanceKey) (replacement *instmodel.Instance, actionRequired bool, err error) {
	candidateReplicas, _ := instinventory.ReadClusterCandidateInstances(promotedReplica.ClusterName)
	candidateReplicas = instmodel.RemoveInstance(candidateReplicas, deadInstanceKey)
	deadInstance, _, err := instinventory.ReadInstance(deadInstanceKey)
	if err != nil {
		deadInstance = nil
	}
	// So we've already promoted a replica.
	// However, can we improve on our choice? Are there any replicas marked with "is_candidate"?
	// Maybe we actually promoted such a replica. Does that mean we should keep it?
	// Maybe we promoted a "neutral", and some "prefer" server is available.
	// Maybe we promoted a "prefer_not"
	// Maybe we promoted a server in a different DC than the master
	// There's many options. We may wish to replace the server we promoted with a better one.
	AuditTopologyRecovery(topologyRecovery, "checking if should replace promoted replica with a better candidate")
	if candidateInstanceKey == nil {
		AuditTopologyRecovery(topologyRecovery, "+ checking if promoted replica is the ideal candidate")
		if deadInstance != nil {
			for _, candidateReplica := range candidateReplicas {
				if promotedReplica.Key.Equals(&candidateReplica.Key) &&
					promotedReplica.DataCenter == deadInstance.DataCenter &&
					promotedReplica.PhysicalEnvironment == deadInstance.PhysicalEnvironment {
					// Seems like we promoted a candidate in the same DC & ENV as dead IM! Ideal! We're happy!
					AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("promoted replica %+v is the ideal candidate", promotedReplica.Key))
					return promotedReplica, false, nil
				}
			}
		}
	}
	// We didn't pick the ideal candidate; let's see if we can replace with a candidate from same DC and ENV
	if candidateInstanceKey == nil {
		// Try a candidate replica that is in same DC & env as the dead instance
		AuditTopologyRecovery(topologyRecovery, "+ searching for an ideal candidate")
		if deadInstance != nil {
			for _, candidateReplica := range candidateReplicas {
				if canTakeOverPromotedServerAsMaster(ctx, candidateReplica, promotedReplica) &&
					candidateReplica.DataCenter == deadInstance.DataCenter &&
					candidateReplica.PhysicalEnvironment == deadInstance.PhysicalEnvironment {
					// This would make a great candidate
					candidateInstanceKey = &candidateReplica.Key
					AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("no candidate was offered for %+v but orchestrator picks %+v as candidate replacement, based on being in same DC & env as failed instance", *deadInstanceKey, candidateReplica.Key))
				}
			}
		}
	}
	if candidateInstanceKey == nil {
		// We cannot find a candidate in same DC and ENV as dead master
		AuditTopologyRecovery(topologyRecovery, "+ checking if promoted replica is an OK candidate")
		for _, candidateReplica := range candidateReplicas {
			if promotedReplica.Key.Equals(&candidateReplica.Key) {
				// Seems like we promoted a candidate replica (though not in same DC and ENV as dead master)
				if satisfied, reason := MasterFailoverGeographicConstraintSatisfied(ctx, &topologyRecovery.AnalysisEntry, candidateReplica); satisfied {
					// Good enough. No further action required.
					AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("promoted replica %+v is a good candidate", promotedReplica.Key))
					return promotedReplica, false, nil
				} else {
					AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("skipping %+v; %s", candidateReplica.Key, reason))
				}
			}
		}
	}
	// Still nothing?
	if candidateInstanceKey == nil {
		// Try a candidate replica that is in same DC & env as the promoted replica (our promoted replica is not an "is_candidate")
		AuditTopologyRecovery(topologyRecovery, "+ searching for a candidate")
		for _, candidateReplica := range candidateReplicas {
			if canTakeOverPromotedServerAsMaster(ctx, candidateReplica, promotedReplica) &&
				promotedReplica.DataCenter == candidateReplica.DataCenter &&
				promotedReplica.PhysicalEnvironment == candidateReplica.PhysicalEnvironment {
				// OK, better than nothing
				candidateInstanceKey = &candidateReplica.Key
				AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("no candidate was offered for %+v but orchestrator picks %+v as candidate replacement, based on being in same DC & env as promoted instance", promotedReplica.Key, candidateReplica.Key))
			}
		}
	}
	// Still nothing?
	if candidateInstanceKey == nil {
		// Try a candidate replica (our promoted replica is not an "is_candidate")
		AuditTopologyRecovery(topologyRecovery, "+ searching for a candidate")
		for _, candidateReplica := range candidateReplicas {
			if canTakeOverPromotedServerAsMaster(ctx, candidateReplica, promotedReplica) {
				if satisfied, reason := MasterFailoverGeographicConstraintSatisfied(ctx, &topologyRecovery.AnalysisEntry, candidateReplica); satisfied {
					// OK, better than nothing
					candidateInstanceKey = &candidateReplica.Key
					AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("no candidate was offered for %+v but orchestrator picks %+v as candidate replacement", promotedReplica.Key, candidateReplica.Key))
				} else {
					AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("skipping %+v; %s", candidateReplica.Key, reason))
				}
			}
		}
	}

	keepSearchingHint := ""
	if satisfied, reason := MasterFailoverGeographicConstraintSatisfied(ctx, &topologyRecovery.AnalysisEntry, promotedReplica); !satisfied {
		keepSearchingHint = fmt.Sprintf("Will keep searching; %s", reason)
	} else if promotedReplica.PromotionRule == instmodel.PreferNotPromoteRule {
		keepSearchingHint = fmt.Sprintf("Will keep searching because we have promoted a server with prefer_not rule: %+v", promotedReplica.Key)
	}
	if keepSearchingHint != "" {
		AuditTopologyRecovery(topologyRecovery, keepSearchingHint)
		neutralReplicas, _ := instinventory.ReadClusterNeutralPromotionRuleInstances(promotedReplica.ClusterName)

		if candidateInstanceKey == nil {
			// Still nothing? Then we didn't find a replica marked as "candidate". OK, further down the stream we have:
			// find neutral instance in same dv&env as dead master
			AuditTopologyRecovery(topologyRecovery, "+ searching for a neutral server to replace promoted server, in same DC and env as dead master")
			for _, neutralReplica := range neutralReplicas {
				if canTakeOverPromotedServerAsMaster(ctx, neutralReplica, promotedReplica) &&
					deadInstance.DataCenter == neutralReplica.DataCenter &&
					deadInstance.PhysicalEnvironment == neutralReplica.PhysicalEnvironment {
					candidateInstanceKey = &neutralReplica.Key
					AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("no candidate was offered for %+v but orchestrator picks %+v as candidate replacement, based on being in same DC & env as dead master", promotedReplica.Key, neutralReplica.Key))
				}
			}
		}
		if candidateInstanceKey == nil {
			// find neutral instance in same dv&env as promoted replica
			AuditTopologyRecovery(topologyRecovery, "+ searching for a neutral server to replace promoted server, in same DC and env as promoted replica")
			for _, neutralReplica := range neutralReplicas {
				if canTakeOverPromotedServerAsMaster(ctx, neutralReplica, promotedReplica) &&
					promotedReplica.DataCenter == neutralReplica.DataCenter &&
					promotedReplica.PhysicalEnvironment == neutralReplica.PhysicalEnvironment {
					candidateInstanceKey = &neutralReplica.Key
					AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("no candidate was offered for %+v but orchestrator picks %+v as candidate replacement, based on being in same DC & env as promoted instance", promotedReplica.Key, neutralReplica.Key))
				}
			}
		}
		if candidateInstanceKey == nil {
			AuditTopologyRecovery(topologyRecovery, "+ searching for a neutral server to replace a prefer_not")
			for _, neutralReplica := range neutralReplicas {
				if canTakeOverPromotedServerAsMaster(ctx, neutralReplica, promotedReplica) {
					if satisfied, reason := MasterFailoverGeographicConstraintSatisfied(ctx, &topologyRecovery.AnalysisEntry, neutralReplica); satisfied {
						// OK, better than nothing
						candidateInstanceKey = &neutralReplica.Key
						AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("no candidate was offered for %+v but orchestrator picks %+v as candidate replacement, based on promoted instance having prefer_not promotion rule", promotedReplica.Key, neutralReplica.Key))
					} else {
						AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("skipping %+v; %s", neutralReplica.Key, reason))
					}
				}
			}
		}
	}

	// So do we have a candidate?
	if candidateInstanceKey == nil {
		// Found nothing. Stick with promoted replica
		AuditTopologyRecovery(topologyRecovery, "+ found no server to promote on top promoted replica")
		return promotedReplica, false, nil
	}
	if promotedReplica.Key.Equals(candidateInstanceKey) {
		// Sanity. It IS the candidate, nothing to promote...
		AuditTopologyRecovery(topologyRecovery, "+ sanity check: found our very own server to promote; doing nothing")
		return promotedReplica, false, nil
	}
	replacement, _, err = instinventory.ReadInstance(candidateInstanceKey)
	return replacement, true, err
}

// replacePromotedReplicaWithCandidate is called after a master (or co-master)
// died and was replaced by some promotedReplica.
// But, is there an even better replica to promote?
// if candidateInstanceKey is given, then it is forced to be promoted over the promotedReplica
// Otherwise, search for the best to promote!
func replacePromotedReplicaWithCandidate(ctx context.Context, topologyRecovery *TopologyRecovery, deadInstanceKey *instmodel.InstanceKey, promotedReplica *instmodel.Instance, candidateInstanceKey *instmodel.InstanceKey) (*instmodel.Instance, error) {
	candidateInstance, actionRequired, err := SuggestReplacementForPromotedReplica(ctx, topologyRecovery, deadInstanceKey, promotedReplica, candidateInstanceKey)
	if err != nil {
		return promotedReplica, log.Errore(err)
	}
	if !actionRequired {
		AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("replace-promoted-replica-with-candidate: promoted instance %+v requires no further action", promotedReplica.Key))
		return promotedReplica, nil
	}

	// Try and promote suggested candidate, if applicable and possible
	AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("replace-promoted-replica-with-candidate: promoted instance %+v is not the suggested candidate %+v. Will see what can be done", promotedReplica.Key, candidateInstance.Key))

	if candidateInstance.MasterKey.Equals(&promotedReplica.Key) {
		AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("replace-promoted-replica-with-candidate: suggested candidate %+v is replica of promoted instance %+v. Will try and take its master", candidateInstance.Key, promotedReplica.Key))
		candidateInstance, err = instrelocation.TakeMaster(ctx, &candidateInstance.Key, topologyRecovery.Type == CoMasterRecovery)
		if err != nil {
			return promotedReplica, log.Errore(err)
		}
		AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("success promoting %+v over %+v", candidateInstance.Key, promotedReplica.Key))

		// As followup to taking over, let's relocate all the rest of the replicas under the candidate instance
		relocateReplicasFunc := func() error {
			log.Debugf("replace-promoted-replica-with-candidate: relocating replicas of %+v below %+v", promotedReplica.Key, candidateInstance.Key)

			relocatedReplicas, _, _, err := instrelocation.RelocateReplicas(ctx, &promotedReplica.Key, &candidateInstance.Key, "")
			log.Debugf("replace-promoted-replica-with-candidate: + relocated %+v replicas of %+v below %+v", len(relocatedReplicas), promotedReplica.Key, candidateInstance.Key)
			AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("relocated %+v replicas of %+v below %+v", len(relocatedReplicas), promotedReplica.Key, candidateInstance.Key))
			return log.Errore(err)
		}
		postponedFunctionsContainer := &topologyRecovery.PostponedFunctionsContainer
		if postponedFunctionsContainer != nil {
			postponedFunctionsContainer.AddPostponedFunction(relocateReplicasFunc, fmt.Sprintf("replace-promoted-replica-with-candidate: relocate replicas of %+v", promotedReplica.Key))
		} else {
			_ = relocateReplicasFunc()
			// We do not propagate the error. It is logged, but otherwise should not fail the entire failover operation
		}
		return candidateInstance, nil
	}

	AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("could not manage to promoted suggested candidate %+v", candidateInstance.Key))
	return promotedReplica, nil
}

// isGenerallyValidAsCandidateSiblingOfIntermediateMaster sees that basic server configuration and state are valid
func isGenerallyValidAsCandidateSiblingOfIntermediateMaster(sibling *instmodel.Instance) bool {
	if !sibling.LogBinEnabled {
		return false
	}
	if !sibling.LogReplicationUpdatesEnabled {
		return false
	}
	if !sibling.ReplicaRunning() {
		return false
	}
	if !sibling.IsLastCheckValid {
		return false
	}
	return true
}

// isValidAsCandidateSiblingOfIntermediateMaster checks to see that the given sibling is capable to take over instance's replicas
func isValidAsCandidateSiblingOfIntermediateMaster(ctx context.Context, intermediateMasterInstance *instmodel.Instance, sibling *instmodel.Instance) bool {
	if sibling.Key.Equals(&intermediateMasterInstance.Key) {
		// same instance
		return false
	}
	if !isGenerallyValidAsCandidateSiblingOfIntermediateMaster(sibling) {
		return false
	}
	if instregroup.IsBannedFromBeingCandidateReplica(ctx, sibling) {
		return false
	}
	if sibling.HasReplicationFilters != intermediateMasterInstance.HasReplicationFilters {
		return false
	}
	if sibling.IsBinlogServer() != intermediateMasterInstance.IsBinlogServer() {
		// When both are binlog servers, failover is trivial.
		// When failed IM is binlog server, its sibling is still valid, but we catually prefer to just repoint the replica up -- simplest!
		return false
	}
	if sibling.ExecBinlogCoordinates.SmallerThan(&intermediateMasterInstance.ExecBinlogCoordinates) {
		return false
	}
	return true
}

func isGenerallyValidAsWouldBeMaster(ctx context.Context, replica *instmodel.Instance, requireLogReplicationUpdates bool) bool {
	if !replica.IsLastCheckValid {
		// something wrong with this replica right now. We shouldn't hope to be able to promote it
		return false
	}
	if !replica.LogBinEnabled {
		return false
	}
	if requireLogReplicationUpdates && !replica.LogReplicationUpdatesEnabled {
		return false
	}
	if replica.IsBinlogServer() {
		return false
	}
	if instregroup.IsBannedFromBeingCandidateReplica(ctx, replica) {
		return false
	}

	return true
}

func canTakeOverPromotedServerAsMaster(ctx context.Context, wantToTakeOver *instmodel.Instance, toBeTakenOver *instmodel.Instance) bool {
	if !isGenerallyValidAsWouldBeMaster(ctx, wantToTakeOver, true) {
		return false
	}
	if !wantToTakeOver.MasterKey.Equals(&toBeTakenOver.Key) {
		return false
	}
	if canReplicate, _ := toBeTakenOver.CanReplicateFromEx(ctx, wantToTakeOver, "canTakeOverPromotedServerAsMaster()"); !canReplicate {
		return false
	}
	return true
}

// GetCandidateSiblingOfIntermediateMaster chooses the best sibling of a dead intermediate master
// to whom the IM's replicas can be moved.
func GetCandidateSiblingOfIntermediateMaster(ctx context.Context, topologyRecovery *TopologyRecovery, intermediateMasterInstance *instmodel.Instance) (*instmodel.Instance, error) {

	siblings, err := instinventory.ReadReplicaInstances(&intermediateMasterInstance.MasterKey)
	if err != nil {
		return nil, err
	}
	if len(siblings) <= 1 {
		return nil, log.Errorf("topology_recovery: no siblings found for %+v", intermediateMasterInstance.Key)
	}

	sort.Sort(sort.Reverse(InstancesByCountReplicas(siblings)))

	// In the next series of steps we attempt to return a good replacement.
	// None of the below attempts is sure to pick a winning server. Perhaps picked server is not enough up-todate -- but
	// this has small likelihood in the general case, and, well, it's an attempt. It's a Plan A, but we have Plan B & C if this fails.

	// At first, we try to return an "is_candidate" server in same dc & env
	AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("searching for the best candidate sibling of dead intermediate master %+v", intermediateMasterInstance.Key))
	for _, sibling := range siblings {
		if isValidAsCandidateSiblingOfIntermediateMaster(ctx, intermediateMasterInstance, sibling) &&
			sibling.IsCandidate &&
			sibling.DataCenter == intermediateMasterInstance.DataCenter &&
			sibling.PhysicalEnvironment == intermediateMasterInstance.PhysicalEnvironment {
			AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("found %+v as the ideal candidate", sibling.Key))
			return sibling, nil
		}
	}
	// No candidate in same DC & env, let's search for a candidate anywhere
	for _, sibling := range siblings {
		if isValidAsCandidateSiblingOfIntermediateMaster(ctx, intermediateMasterInstance, sibling) && sibling.IsCandidate {
			AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("found %+v as a replacement for %+v [candidate sibling]", sibling.Key, intermediateMasterInstance.Key))
			return sibling, nil
		}
	}
	// Go for some valid in the same DC & ENV
	for _, sibling := range siblings {
		if isValidAsCandidateSiblingOfIntermediateMaster(ctx, intermediateMasterInstance, sibling) &&
			sibling.DataCenter == intermediateMasterInstance.DataCenter &&
			sibling.PhysicalEnvironment == intermediateMasterInstance.PhysicalEnvironment {
			AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("found %+v as a replacement for %+v [same dc & environment]", sibling.Key, intermediateMasterInstance.Key))
			return sibling, nil
		}
	}
	// Just whatever is valid.
	for _, sibling := range siblings {
		if isValidAsCandidateSiblingOfIntermediateMaster(ctx, intermediateMasterInstance, sibling) {
			AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("found %+v as a replacement for %+v [any sibling]", sibling.Key, intermediateMasterInstance.Key))
			return sibling, nil
		}
	}
	return nil, log.Errorf("topology_recovery: cannot find candidate sibling of %+v", intermediateMasterInstance.Key)
}

func getGracefulMasterTakeoverDesignatedInstance(ctx context.Context, clusterMasterKey *instmodel.InstanceKey, designatedKey *instmodel.InstanceKey, clusterMasterDirectReplicas []*instmodel.Instance, auto bool) (designatedInstance *instmodel.Instance, err error) {
	if designatedKey == nil {
		// User did not specify a replica to promote
		if len(clusterMasterDirectReplicas) == 1 {
			// Single replica. That's the one we'll promote
			return clusterMasterDirectReplicas[0], nil
		}
		// More than one replica.
		if !auto {
			return nil, fmt.Errorf("GracefulMasterTakeover: target instance not indicated, auto=false, and master %+v has %+v replicas. orchestrator cannot choose where to failover to. Aborting", *clusterMasterKey, len(clusterMasterDirectReplicas))
		}
		log.Debugf("GracefulMasterTakeover: request takeover for master %+v, no designated replica indicated. orchestrator will attempt to auto deduce replica.", *clusterMasterKey)
		designatedInstance, _, _, _, _, err = instregroup.GetCandidateReplica(ctx, clusterMasterKey, false)
		if err != nil || designatedInstance == nil {
			return nil, fmt.Errorf("GracefulMasterTakeover: no target instance indicated, failed to auto-detect candidate replica for master %+v. Aborting", *clusterMasterKey)
		}
		log.Debugf("GracefulMasterTakeover: candidateReplica=%+v", designatedInstance.Key)
		if _, err := instreplication.StartReplication(ctx, &designatedInstance.Key); err != nil {
			return nil, fmt.Errorf("GracefulMasterTakeover:cannot start replication on designated replica %+v. Aborting", designatedKey)
		}
		log.Infof("GracefulMasterTakeover: designated master deduced to be %+v", designatedInstance.Key)
		return designatedInstance, nil
	}

	// Verify designated instance is a direct replica of master
	for _, directReplica := range clusterMasterDirectReplicas {
		if directReplica.Key.Equals(designatedKey) {
			designatedInstance = directReplica
		}
	}
	if designatedInstance == nil {
		return nil, fmt.Errorf("GracefulMasterTakeover: indicated designated instance %+v must be directly replicating from the master %+v", *designatedKey, *clusterMasterKey)
	}
	log.Infof("GracefulMasterTakeover: designated master instructed to be %+v", designatedInstance.Key)
	return designatedInstance, nil
}
