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

	"github.com/openark/orchestrator/internal/golib/log"
	"github.com/openark/orchestrator/internal/inst/analysis"
	instaudit "github.com/openark/orchestrator/internal/inst/audit"
	instreplication "github.com/openark/orchestrator/internal/inst/change/replication"
	instmodel "github.com/openark/orchestrator/internal/inst/instance"
	"github.com/openark/orchestrator/internal/recoverypolicy"
)

// checkAndRecoverNonWriteableMaster attempts to recover from a read only master by turning it writeable.
// This behavior is feature protected, see config.Current().RecoverNonWriteableMaster
func checkAndRecoverNonWriteableMaster(ctx context.Context, analysisEntry analysis.ReplicationAnalysis, candidateInstanceKey *instmodel.InstanceKey, forceInstanceRecovery bool, skipProcesses bool) (recoveryAttempted bool, topologyRecovery *TopologyRecovery, err error) {
	if !recoverypolicy.FromContext(ctx, analysisEntry.ClusterDetails.ClusterName).RecoverNonWriteableMaster {
		return false, nil, nil
	}

	topologyRecovery, err = AttemptRecoveryRegistration(ctx, &analysisEntry, true, true)
	if topologyRecovery == nil {
		AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("found an active or recent recovery on %+v. Will not issue another checkAndRecoverNonWriteableMaster.", analysisEntry.AnalyzedInstanceKey))
		return false, nil, err
	}

	instaudit.AuditOperation("recover-non-writeable-master", &analysisEntry.AnalyzedInstanceKey, "problem found; will recover")
	if !skipProcesses {
		if err := executeHookPhaseContext(ctx, "pre_failover", "PreFailoverProcesses", topologyRecovery); err != nil {
			return false, topologyRecovery, topologyRecovery.AddError(err)
		}
	}

	instance, err := instreplication.SetReadOnly(&analysisEntry.AnalyzedInstanceKey, false)
	if err == nil {
		resolveRecovery(topologyRecovery, instance)
	}
	return true, topologyRecovery, err
}

// checkAndRecoverLockedSemiSyncMaster
func checkAndRecoverLockedSemiSyncMaster(ctx context.Context, analysisEntry analysis.ReplicationAnalysis, candidateInstanceKey *instmodel.InstanceKey, forceInstanceRecovery bool, skipProcesses bool) (recoveryAttempted bool, topologyRecovery *TopologyRecovery, err error) {
	policy := recoverypolicy.FromContext(ctx, analysisEntry.ClusterDetails.ClusterName)
	topologyRecovery, err = AttemptRecoveryRegistration(ctx, &analysisEntry, true, true)
	if topologyRecovery == nil {
		AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("found an active or recent recovery on %+v. Will not issue another RecoverLockedSemiSyncMaster.", analysisEntry.AnalyzedInstanceKey))
		return false, nil, err
	}
	if policy.EnforceExactSemiSyncReplicas {
		return recoverSemiSyncReplicas(topologyRecovery, analysisEntry, true)
	}
	if policy.RecoverLockedSemiSyncMaster {
		return recoverSemiSyncReplicas(topologyRecovery, analysisEntry, false)
	}
	AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("no action taken to recover locked semi sync master on %+v. Enable RecoverLockedSemiSyncMaster or EnforceExactSemiSyncReplicas change this behavior.", analysisEntry.AnalyzedInstanceKey))
	return false, nil, err
}

// checkAndRecoverMasterWithTooManySemiSyncReplicas registers and performs a recovery for MasterWithTooManySemiSyncReplicas
func checkAndRecoverMasterWithTooManySemiSyncReplicas(ctx context.Context, analysisEntry analysis.ReplicationAnalysis, candidateInstanceKey *instmodel.InstanceKey, forceInstanceRecovery bool, skipProcesses bool) (recoveryAttempted bool, topologyRecovery *TopologyRecovery, err error) {
	topologyRecovery, err = AttemptRecoveryRegistration(ctx, &analysisEntry, true, true)
	if topologyRecovery == nil {
		AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("found an active or recent recovery on %+v. Will not issue another RecoverMasterWithTooManySemiSyncReplicas.", analysisEntry.AnalyzedInstanceKey))
		return false, nil, err
	}
	return recoverSemiSyncReplicas(topologyRecovery, analysisEntry, true)
}

// recoverSemiSyncReplicas analyzes the replica topology for the given master and applies to repair it. If exactReplicaTopology is set, it will enable/disable the semi-sync enabled
// variable (rpl_semi_sync_replica_enabled) of the replicas depending on their semi-sync priority and promotion rule. If exactReplicaTopology is not set, the function will only ever
// enable semi-sync on replicas and never disable it. This variable typically corresponds to the EnforceExactSemiSyncReplicas config variable.
func recoverSemiSyncReplicas(topologyRecovery *TopologyRecovery, analysisEntry analysis.ReplicationAnalysis, exactReplicaTopology bool) (recoveryAttempted bool, topologyRecoveryOut *TopologyRecovery, err error) {
	masterInstance, replicas, actions, err := instreplication.AnalyzeSemiSyncReplicaTopology(&analysisEntry.AnalyzedInstanceKey, nil, exactReplicaTopology)
	if err != nil {
		AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("semi-sync: %s", err.Error()))
		return true, topologyRecovery, log.Errorf("semi-sync: %s", err.Error())
	} else if len(actions) == 0 {
		AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("semi-sync: cannot determine actions based on possible semi-sync replicas; cannot recover on %+v", &analysisEntry.AnalyzedInstanceKey))
		return true, topologyRecovery, log.Errorf("cannot determine actions based on possible semi-sync replicas; cannot recover on %+v", &analysisEntry.AnalyzedInstanceKey)
	}

	// Disable semi-sync master on all replicas; this is to avoid semi-sync failures on the replicas (rpl_semi_sync_master_no_tx)
	// and to make it consistent with the logic in SetReadOnly
	for _, replica := range replicas {
		instreplication.MaybeDisableSemiSyncMaster(replica) // it's okay if this fails
	}

	// Take action: we first enable and then disable (two loops) in order to avoid "locked master" scenarios
	AuditTopologyRecovery(topologyRecovery, "semi-sync: taking actions:")
	for replica, enable := range actions {
		if enable {
			AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("semi-sync: - %s: setting rpl_semi_sync_slave_enabled=%t, restarting slave_io thread", replica.Key.String(), enable))
			if _, err := instreplication.SetSemiSyncReplica(&replica.Key, enable); err != nil {
				return true, topologyRecovery, log.Errorf("cannot enable semi sync on replica %+v", replica.Key)
			}
		}
	}
	for replica, enable := range actions {
		if !enable {
			AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("semi-sync: - %s: setting rpl_semi_sync_slave_enabled=%t, restarting slave_io thread", replica.Key.String(), enable))
			if _, err := instreplication.SetSemiSyncReplica(&replica.Key, enable); err != nil {
				return true, topologyRecovery, fmt.Errorf("cannot disable semi sync on replica %+v", replica.Key)
			}
		}
	}

	resolveRecovery(topologyRecovery, masterInstance)
	AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("semi-sync: recovery complete; success = %t", topologyRecovery.IsSuccessful))
	return true, topologyRecovery, nil
}
