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

	"github.com/openark/orchestrator/internal/golib/log"
	"github.com/openark/orchestrator/internal/inst/analysis"
	instregroup "github.com/openark/orchestrator/internal/inst/change/regroup"
	instrelocation "github.com/openark/orchestrator/internal/inst/change/relocation"
	instreplication "github.com/openark/orchestrator/internal/inst/change/replication"
	instmodel "github.com/openark/orchestrator/internal/inst/instance"
	instinventory "github.com/openark/orchestrator/internal/inst/inventory"
	insttopology "github.com/openark/orchestrator/internal/inst/topology"
	modeldomain "github.com/openark/orchestrator/internal/models/domain"
	"github.com/openark/orchestrator/internal/models/dto"
	"github.com/openark/orchestrator/internal/recoverypolicy"
)

func forceAnalysisEntry(clusterName string, analysisCode analysis.AnalysisCode, commandHint string, failedInstanceKey *instmodel.InstanceKey) (analysisEntry analysis.ReplicationAnalysis, err error) {
	clusterInfo, err := instinventory.ReadClusterInfo(clusterName)
	if err != nil {
		return analysisEntry, err
	}

	clusterAnalysisEntries, err := analysis.GetReplicationAnalysis(clusterInfo.ClusterName, &dto.ReplicationAnalysisHints{IncludeDowntimed: true, IncludeNoProblem: true})
	if err != nil {
		return analysisEntry, err
	}

	for _, entry := range clusterAnalysisEntries {
		if entry.AnalyzedInstanceKey.Equals(failedInstanceKey) {
			analysisEntry = entry
		}
	}
	analysisEntry.Analysis = analysisCode // we force this analysis
	analysisEntry.CommandHint = commandHint
	analysisEntry.ClusterDetails = *clusterInfo
	analysisEntry.AnalyzedInstanceKey = *failedInstanceKey

	return analysisEntry, nil
}

// ForceExecuteRecovery can be called to issue a recovery process even if analysis says there is no recovery case.
// The caller of this function injects the type of analysis it wishes the function to assume.
// By calling this function one takes responsibility for one's actions.
func ForceExecuteRecovery(ctx context.Context, analysisEntry analysis.ReplicationAnalysis, candidateInstanceKey *instmodel.InstanceKey, skipProcesses bool) (recoveryAttempted bool, topologyRecovery *TopologyRecovery, err error) {
	return executeCheckAndRecoverFunction(ctx, analysisEntry, candidateInstanceKey, true, skipProcesses)
}

// ForceMasterFailover *trusts* master of given cluster is dead and initiates a failover
func ForceMasterFailover(ctx context.Context, clusterName string) (topologyRecovery *TopologyRecovery, err error) {
	clusterMasters, err := instinventory.ReadClusterMaster(clusterName)
	if err != nil {
		return nil, fmt.Errorf("cannot deduce cluster master for %+v", clusterName)
	}
	if len(clusterMasters) != 1 {
		return nil, fmt.Errorf("cannot deduce cluster master for %+v", clusterName)
	}
	clusterMaster := clusterMasters[0]

	analysisEntry, err := forceAnalysisEntry(clusterName, analysis.DeadMaster, analysis.ForceMasterFailoverCommandHint, &clusterMaster.Key)
	if err != nil {
		return nil, err
	}
	recoveryAttempted, topologyRecovery, err := ForceExecuteRecovery(ctx, analysisEntry, nil, false)
	if err != nil {
		return nil, err
	}
	if !recoveryAttempted {
		return nil, fmt.Errorf("unexpected error: recovery not attempted. This should not happen")
	}
	if topologyRecovery == nil {
		return nil, fmt.Errorf("recovery attempted but with no results. This should not happen")
	}
	if topologyRecovery.SuccessorKey == nil {
		return nil, fmt.Errorf("recovery attempted yet no replica promoted")
	}
	return topologyRecovery, nil
}

// ForceMasterTakeover *trusts* master of given cluster is dead and fails over to designated instance,
// which has to be its direct child.
func ForceMasterTakeover(ctx context.Context, clusterName string, destination *instmodel.Instance) (topologyRecovery *TopologyRecovery, err error) {
	clusterMasters, err := instinventory.ReadClusterWriteableMaster(clusterName)
	if err != nil {
		return nil, fmt.Errorf("cannot deduce cluster master for %+v", clusterName)
	}
	if len(clusterMasters) != 1 {
		return nil, fmt.Errorf("cannot deduce cluster master for %+v", clusterName)
	}
	clusterMaster := clusterMasters[0]

	if !destination.MasterKey.Equals(&clusterMaster.Key) {
		return nil, fmt.Errorf("you may only promote a direct child of the master %+v. The master of %+v is %+v", clusterMaster.Key, destination.Key, destination.MasterKey)
	}
	log.Infof("Will demote %+v and promote %+v instead", clusterMaster.Key, destination.Key)

	analysisEntry, err := forceAnalysisEntry(clusterName, analysis.DeadMaster, analysis.ForceMasterTakeoverCommandHint, &clusterMaster.Key)
	if err != nil {
		return nil, err
	}
	recoveryAttempted, topologyRecovery, err := ForceExecuteRecovery(ctx, analysisEntry, &destination.Key, false)
	if err != nil {
		return nil, err
	}
	if !recoveryAttempted {
		return nil, fmt.Errorf("unexpected error: recovery not attempted. This should not happen")
	}
	if topologyRecovery == nil {
		return nil, fmt.Errorf("recovery attempted but with no results. This should not happen")
	}
	if topologyRecovery.SuccessorKey == nil {
		return nil, fmt.Errorf("recovery attempted yet no replica promoted")
	}
	return topologyRecovery, nil
}

// GracefulMasterTakeover will demote master of existing topology and promote its
// direct replica instead.
// It expects that replica to have no siblings.
// This function is graceful in that it will first lock down the master, then wait
// for the designated replica to catch up with last position.
// It will point old master at the newly promoted master at the correct coordinates, but will not start replication.
func GracefulMasterTakeover(ctx context.Context, clusterName string, designatedKey *instmodel.InstanceKey, auto bool) (topologyRecovery *TopologyRecovery, promotedMasterCoordinates *instmodel.BinlogCoordinates, err error) {
	ctx, err = pinRecoveryExecution(ctx, clusterName)
	if err != nil {
		return nil, nil, err
	}

	clusterMasters, err := instinventory.ReadClusterMaster(clusterName)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot deduce cluster master for %+v; error: %+v", clusterName, err)
	}
	if len(clusterMasters) != 1 {
		return nil, nil, fmt.Errorf("cannot deduce cluster master for %+v. Found %+v potential masters", clusterName, len(clusterMasters))
	}
	clusterMaster := clusterMasters[0]

	clusterMasterDirectReplicas, err := instinventory.ReadReplicaInstances(&clusterMaster.Key)
	if err != nil {
		return nil, nil, log.Errore(err)
	}

	if len(clusterMasterDirectReplicas) == 0 {
		return nil, nil, fmt.Errorf("master %+v doesn't seem to have replicas", clusterMaster.Key)
	}

	if designatedKey != nil && !designatedKey.IsValid() {
		// An empty or invalid key is as good as no key
		designatedKey = nil
	}
	designatedInstance, err := getGracefulMasterTakeoverDesignatedInstance(ctx, &clusterMaster.Key, designatedKey, clusterMasterDirectReplicas, auto)
	if err != nil {
		return nil, nil, log.Errore(err)
	}

	if instregroup.IsBannedFromBeingCandidateReplica(ctx, designatedInstance) {
		return nil, nil, fmt.Errorf("GracefulMasterTakeover: designated instance %+v cannot be promoted due to promotion rule or it is explicitly ignored in PromotionIgnoreHostnameFilters configuration", designatedInstance.Key)
	}

	masterOfDesignatedInstance, err := insttopology.GetInstanceMaster(designatedInstance)
	if err != nil {
		return nil, nil, err
	}
	if !masterOfDesignatedInstance.Key.Equals(&clusterMaster.Key) {
		return nil, nil, fmt.Errorf("sanity check failure. It seems like the designated instance %+v does not replicate from the master %+v (designated instance's master key is %+v). This error is strange. Panicking", designatedInstance.Key, clusterMaster.Key, designatedInstance.MasterKey)
	}
	if !designatedInstance.HasReasonableMaintenanceReplicationLag(ctx) {
		return nil, nil, fmt.Errorf("desginated instance %+v seems to be lagging too much for this operation. Aborting", designatedInstance.Key)
	}

	if len(clusterMasterDirectReplicas) > 1 {
		log.Infof("GracefulMasterTakeover: Will let %+v take over its siblings", designatedInstance.Key)
		relocatedReplicas, _, _, err := instrelocation.RelocateReplicas(ctx, &clusterMaster.Key, &designatedInstance.Key, "")
		if len(relocatedReplicas) != len(clusterMasterDirectReplicas)-1 {
			// We are unable to make designated instance master of all its siblings
			relocatedReplicasKeyMap := instmodel.NewInstanceKeyMap()
			relocatedReplicasKeyMap.AddInstances(relocatedReplicas)
			// Let's see which replicas have not been relocated
			for _, directReplica := range clusterMasterDirectReplicas {
				if relocatedReplicasKeyMap.HasKey(directReplica.Key) {
					// relocated, good
					continue
				}
				if directReplica.Key.Equals(&designatedInstance.Key) {
					// obviously we skip this one
					continue
				}
				if directReplica.IsDowntimed {
					// obviously we skip this one
					log.Warningf("GracefulMasterTakeover: unable to relocate %+v below designated %+v, but since it is downtimed (downtime reason: %s) I will proceed", directReplica.Key, designatedInstance.Key, directReplica.DowntimeReason)
					continue
				}
				return nil, nil, fmt.Errorf("desginated instance %+v cannot take over all of its siblings. Error: %+v", designatedInstance.Key, err)
			}
		}
	}
	log.Infof("GracefulMasterTakeover: Will demote %+v and promote %+v instead", clusterMaster.Key, designatedInstance.Key)

	replicationCreds, replicationCredentialsError := instreplication.ReadReplicationCredentials(&designatedInstance.Key)

	analysisEntry, err := forceAnalysisEntry(clusterName, analysis.DeadMaster, analysis.GracefulMasterTakeoverCommandHint, &clusterMaster.Key)
	if err != nil {
		return nil, nil, err
	}
	preGracefulTakeoverTopologyRecovery := &TopologyRecovery{
		SuccessorKey:  &designatedInstance.Key,
		AnalysisEntry: analysisEntry,
	}
	if err := executeHookPhaseContext(ctx, "pre_graceful_takeover", "PreGracefulTakeoverProcesses", preGracefulTakeoverTopologyRecovery); err != nil {
		return nil, nil, fmt.Errorf("failed running PreGracefulTakeoverProcesses: %+v", err)
	}

	log.Infof("GracefulMasterTakeover: Will set %+v as read_only", clusterMaster.Key)
	if clusterMaster, err = instreplication.SetReadOnly(&clusterMaster.Key, true); err != nil {
		return nil, nil, err
	}
	demotedMasterSelfBinlogCoordinates := &clusterMaster.SelfBinlogCoordinates
	log.Infof("GracefulMasterTakeover: Will wait for %+v to reach master coordinates %+v", designatedInstance.Key, *demotedMasterSelfBinlogCoordinates)
	if designatedInstance, _, err = instreplication.WaitForExecBinlogCoordinatesToReach(&designatedInstance.Key, demotedMasterSelfBinlogCoordinates, time.Duration(recoverypolicy.FromContext(ctx, clusterName).ReasonableMaintenanceReplicationLagSeconds)*time.Second); err != nil {
		return nil, nil, err
	}
	promotedMasterCoordinates = &designatedInstance.SelfBinlogCoordinates

	log.Infof("GracefulMasterTakeover: attempting recovery")
	recoveryAttempted, topologyRecovery, err := ForceExecuteRecovery(ctx, analysisEntry, &designatedInstance.Key, false)
	if err != nil {
		log.Errorf("GracefulMasterTakeover: noting an error, and for now proceeding: %+v", err)
	}
	if !recoveryAttempted {
		return nil, nil, fmt.Errorf("GracefulMasterTakeover: unexpected error: recovery not attempted. This should not happen")
	}
	if topologyRecovery == nil {
		return nil, nil, fmt.Errorf("GracefulMasterTakeover: recovery attempted but with no results. This should not happen")
	}
	if topologyRecovery.SuccessorKey == nil {
		// Promotion fails.
		// Undo setting read-only on original master.
		instreplication.SetReadOnly(&clusterMaster.Key, false)
		return nil, nil, fmt.Errorf("GracefulMasterTakeover: Recovery attempted yet no replica promoted; err=%+v", err)
	}
	var gtidHint modeldomain.OperationGTIDHint = modeldomain.GTIDHintNeutral
	if topologyRecovery.RecoveryType == MasterRecoveryGTID {
		gtidHint = modeldomain.GTIDHintForce
	}
	clusterMaster, err = instreplication.ChangeMasterTo(&clusterMaster.Key, &designatedInstance.Key, promotedMasterCoordinates, false, gtidHint)
	if !clusterMaster.SelfBinlogCoordinates.Equals(demotedMasterSelfBinlogCoordinates) {
		log.Errorf("GracefulMasterTakeover: sanity problem. Demoted master's coordinates changed from %+v to %+v while supposed to have been frozen", *demotedMasterSelfBinlogCoordinates, clusterMaster.SelfBinlogCoordinates)
	}
	if !clusterMaster.HasReplicationCredentials && replicationCredentialsError == nil {
		_, credentialsErr := instreplication.ChangeMasterCredentials(&clusterMaster.Key, replicationCreds)
		if err == nil {
			err = credentialsErr
		}
	}
	if !designatedInstance.AllowTLS {
		_, enableSSLErr := instreplication.EnableMasterGetSourcePublicKey(&clusterMaster.Key)
		if err == nil {
			err = enableSSLErr
		}
	}
	if auto {
		_, startReplicationErr := instreplication.StartReplication(ctx, &clusterMaster.Key)
		if err == nil {
			err = startReplicationErr
		}
	}
	_ = executeHookPhaseContext(ctx, "post_graceful_takeover", "PostGracefulTakeoverProcesses", topologyRecovery)

	return topologyRecovery, promotedMasterCoordinates, err
}
