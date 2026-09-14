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
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"sync/atomic"
	"time"

	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/golib/log"
	"github.com/openark/orchestrator/internal/inst/analysis"
	instdowntime "github.com/openark/orchestrator/internal/inst/downtime"
	instmodel "github.com/openark/orchestrator/internal/inst/instance"
	"github.com/openark/orchestrator/internal/models/dto"
	orcraft "github.com/openark/orchestrator/internal/raft"
	"github.com/openark/orchestrator/internal/recoverypolicy"
	"github.com/openark/orchestrator/internal/util"
	"github.com/patrickmn/go-cache"

	"github.com/openark/orchestrator/internal/observability"
)

var countPendingRecoveries atomic.Int64

var emergencyReadTopologyInstanceMap *cache.Cache

var emergencyRestartReplicaTopologyInstanceMap *cache.Cache

var emergencyOperationGracefulPeriodMap *cache.Cache

func init() {

	go initializeTopologyRecoveryPostConfiguration()

	observability.Gauge("orchestrator_recovery_pending", "Pending recoveries", getCountPendingRecoveries)
}

func getCountPendingRecoveries() int64 {
	return countPendingRecoveries.Load()
}

func initializeTopologyRecoveryPostConfiguration() {
	config.WaitForConfigurationToBeLoaded()

	emergencyReadTopologyInstanceMap = cache.New(time.Second, time.Millisecond*250)
	emergencyRestartReplicaTopologyInstanceMap = cache.New(time.Second*30, time.Second)
	emergencyOperationGracefulPeriodMap = cache.New(time.Second*5, time.Millisecond*500)
}

// AuditTopologyRecovery audits a single step in a topology recovery process.
func AuditTopologyRecovery(topologyRecovery *TopologyRecovery, message string) error {
	log.Infof("topology_recovery: %s", message)
	if topologyRecovery == nil {
		return nil
	}

	recoveryStep := NewTopologyRecoveryStep(topologyRecovery.UID, message)

	_, err := orcraft.PublishCommand("write-recovery-step", recoveryStep)
	return err

}

func resolveRecovery(topologyRecovery *TopologyRecovery, successorInstance *instmodel.Instance) error {
	if successorInstance != nil {
		topologyRecovery.SuccessorKey = &successorInstance.Key
		topologyRecovery.SuccessorAlias = successorInstance.InstanceAlias
		topologyRecovery.IsSuccessful = true
		// Assign the current Binlog Coordinates of Successor Instance
		topologyRecovery.SuccessorBinlogCoordinates = &successorInstance.SelfBinlogCoordinates
	}

	_, err := orcraft.PublishCommand("resolve-recovery", topologyRecovery)
	return err

}

// checkAndRecoverGenericProblem is a general-purpose recovery function
func checkAndRecoverGenericProblem(ctx context.Context, analysisEntry analysis.ReplicationAnalysis, candidateInstanceKey *instmodel.InstanceKey, forceInstanceRecovery bool, skipProcesses bool) (bool, *TopologyRecovery, error) {
	return false, nil, nil
}

// checkAndExecuteFailureDetectionProcesses tries to register for failure detection and potentially executes
// failure-detection processes.
func checkAndExecuteFailureDetectionProcesses(ctx context.Context, analysisEntry analysis.ReplicationAnalysis, skipProcesses bool) (detectionRegistrationSuccess bool, processesExecutionAttempted bool, err error) {
	if ok, _ := AttemptFailureDetectionRegistration(&analysisEntry); !ok {
		if util.ClearToLog("checkAndExecuteFailureDetectionProcesses", analysisEntry.AnalyzedInstanceKey.StringCode()) {
			log.Infof("checkAndExecuteFailureDetectionProcesses: could not register %+v detection on %+v", analysisEntry.Analysis, analysisEntry.AnalyzedInstanceKey)
		}
		return false, false, nil
	}
	log.Infof("topology_recovery: detected %+v failure on %+v", analysisEntry.Analysis, analysisEntry.AnalyzedInstanceKey)
	// Execute on-detection processes
	if skipProcesses {
		return true, false, nil
	}
	err = executeHookPhaseContext(ctx, "failure_detection", "OnFailureDetectionProcesses", NewTopologyRecovery(analysisEntry))
	return true, true, err
}

func getCheckAndRecoverFunction(ctx context.Context, analysisCode analysis.AnalysisCode, analyzedInstanceKey *instmodel.InstanceKey) (
	checkAndRecoverFunction func(ctx context.Context, analysisEntry analysis.ReplicationAnalysis, candidateInstanceKey *instmodel.InstanceKey, forceInstanceRecovery bool, skipProcesses bool) (recoveryAttempted bool, topologyRecovery *TopologyRecovery, err error),
	isActionableRecovery bool,
) {
	switch analysisCode {
	// master
	case analysis.DeadMaster, analysis.DeadMasterAndSomeReplicas:
		if isInEmergencyOperationGracefulPeriod(analyzedInstanceKey) {
			return checkAndRecoverGenericProblem, false
		} else {
			return checkAndRecoverDeadMaster, true
		}
	case analysis.LockedSemiSyncMaster:
		if isInEmergencyOperationGracefulPeriod(analyzedInstanceKey) {
			return checkAndRecoverGenericProblem, false
		} else {
			return checkAndRecoverLockedSemiSyncMaster, true
		}
	case analysis.MasterWithTooManySemiSyncReplicas:
		return checkAndRecoverMasterWithTooManySemiSyncReplicas, true
	// intermediate master
	case analysis.DeadIntermediateMaster:
		return checkAndRecoverDeadIntermediateMaster, true
	case analysis.DeadIntermediateMasterAndSomeReplicas:
		return checkAndRecoverDeadIntermediateMaster, true
	case analysis.DeadIntermediateMasterWithSingleReplicaFailingToConnect:
		return checkAndRecoverDeadIntermediateMaster, true
	case analysis.AllIntermediateMasterReplicasFailingToConnectOrDead:
		return checkAndRecoverDeadIntermediateMaster, true
	case analysis.DeadIntermediateMasterAndReplicas:
		return checkAndRecoverGenericProblem, false
	// co-master
	case analysis.DeadCoMaster:
		return checkAndRecoverDeadCoMaster, true
	case analysis.DeadCoMasterAndSomeReplicas:
		return checkAndRecoverDeadCoMaster, true
	// master, non actionable
	case analysis.DeadMasterAndReplicas:
		return checkAndRecoverGenericProblem, false
	case analysis.UnreachableMaster:
		return checkAndRecoverGenericProblem, false
	case analysis.UnreachableMasterWithLaggingReplicas:
		return checkAndRecoverGenericProblem, false
	case analysis.AllMasterReplicasNotReplicating:
		return checkAndRecoverGenericProblem, false
	case analysis.AllMasterReplicasNotReplicatingOrDead:
		return checkAndRecoverGenericProblem, false
	case analysis.UnreachableIntermediateMasterWithLaggingReplicas:
		return checkAndRecoverGenericProblem, false
	// replication group members
	case analysis.DeadReplicationGroupMemberWithReplicas:
		return checkAndRecoverDeadGroupMemberWithReplicas, true
	// recoverable structure analysis
	case analysis.NoWriteableMasterStructureWarning:
		return checkAndRecoverNonWriteableMaster, true
	}
	// Right now this is mostly causing noise with no clear action.
	// Will revisit this in the future.
	// case inst.AllMasterReplicasStale:
	//   return checkAndRecoverGenericProblem, false

	return nil, false
}

// executeCheckAndRecoverFunction will choose the correct check & recovery function based on analysis.
// It executes the function synchronuously
func executeCheckAndRecoverFunction(ctx context.Context, analysisEntry analysis.ReplicationAnalysis, candidateInstanceKey *instmodel.InstanceKey, forceInstanceRecovery bool, skipProcesses bool) (recoveryAttempted bool, topologyRecovery *TopologyRecovery, err error) {
	ctx, err = pinRecoveryExecution(ctx, analysisEntry.ClusterDetails.ClusterName)
	if err != nil {
		return false, nil, err
	}

	snapshot := recoverypolicy.SnapshotFromContext(ctx)
	analysisEntry.ClusterDetails.HasAutomatedMasterRecovery = snapshot.Policy.AutoMasterRecovery
	analysisEntry.ClusterDetails.HasAutomatedIntermediateMasterRecovery = snapshot.Policy.AutoIntermediateMasterRecovery

	countPendingRecoveries.Add(1)
	defer countPendingRecoveries.Add(-1)

	recoveryDisabledGlobally, recerr := IsRecoveryDisabled()
	// Check for recovery being disabled globally
	if recerr != nil {
		// Unexpected. Shouldn't get this
		log.Errorf("Unable to determine if recovery is disabled globally: %v", recerr)
	}
	checkAndRecoverFunction, isActionableRecovery := getCheckAndRecoverFunction(ctx, analysisEntry.Analysis, &analysisEntry.AnalyzedInstanceKey)
	analysisEntry.IsActionableRecovery = isActionableRecovery
	runEmergentOperations(&analysisEntry, !recoveryDisabledGlobally || forceInstanceRecovery)

	if checkAndRecoverFunction == nil {
		// Unhandled problem type
		if analysisEntry.Analysis != analysis.NoProblem {
			if util.ClearToLog("executeCheckAndRecoverFunction", analysisEntry.AnalyzedInstanceKey.StringCode()) {
				log.Warningf("executeCheckAndRecoverFunction: ignoring analysisEntry that has no action plan: %+v; key: %+v",
					analysisEntry.Analysis, analysisEntry.AnalyzedInstanceKey)
			}
		}

		return false, nil, nil
	}
	// we have a recovery function; its execution still depends on filters if not disabled.
	if isActionableRecovery || util.ClearToLog("executeCheckAndRecoverFunction: detection", analysisEntry.AnalyzedInstanceKey.StringCode()) {
		log.Infof("executeCheckAndRecoverFunction: proceeding with %+v detection on %+v; isActionable?: %+v; skipProcesses: %+v", analysisEntry.Analysis, analysisEntry.AnalyzedInstanceKey, isActionableRecovery, skipProcesses)
	}

	// At this point we have validated there's a failure scenario for which we have a recovery path.

	// with raft, all nodes can (and should) run analysis,
	// but only the leader proceeds to execute detection hooks and then to failover.
	if !orcraft.IsLeaderReady() {
		log.Infof("CheckAndRecover: Analysis: %+v, InstanceKey: %+v, candidateInstanceKey: %+v, "+
			"skipProcesses: %v: NOT detecting/recovering host (raft non-leader)",
			analysisEntry.Analysis, analysisEntry.AnalyzedInstanceKey, candidateInstanceKey, skipProcesses)
		return false, nil, err
	}

	// Initiate detection:
	registrationSuccess, _, err := checkAndExecuteFailureDetectionProcesses(ctx, analysisEntry, skipProcesses)
	if registrationSuccess {

		_, err := orcraft.PublishCommand("register-failure-detection", analysisEntry)
		log.Errore(err)

	}
	if err != nil {
		log.Errorf("executeCheckAndRecoverFunction: error on failure detection: %+v", err)
		return false, nil, err
	}
	// We don't mind whether detection really executed the processes or not
	// (it may have been silenced due to previous detection). We only care there's no error.

	// We're about to embark on recovery shortly...

	if recoveryDisabledGlobally {
		if !forceInstanceRecovery {
			log.Infof("CheckAndRecover: Analysis: %+v, InstanceKey: %+v, candidateInstanceKey: %+v, "+
				"skipProcesses: %v: NOT Recovering host (disabled globally)",
				analysisEntry.Analysis, analysisEntry.AnalyzedInstanceKey, candidateInstanceKey, skipProcesses)

			return false, nil, err
		}
		log.Infof("CheckAndRecover: Analysis: %+v, InstanceKey: %+v, candidateInstanceKey: %+v, "+
			"skipProcesses: %v: recoveries disabled globally but forcing this recovery",
			analysisEntry.Analysis, analysisEntry.AnalyzedInstanceKey, candidateInstanceKey, skipProcesses)
	}

	// Actually attempt recovery:
	if isActionableRecovery || util.ClearToLog("executeCheckAndRecoverFunction: recovery", analysisEntry.AnalyzedInstanceKey.StringCode()) {
		log.Infof("executeCheckAndRecoverFunction: proceeding with %+v recovery on %+v; isRecoverable?: %+v; skipProcesses: %+v", analysisEntry.Analysis, analysisEntry.AnalyzedInstanceKey, isActionableRecovery, skipProcesses)
	}
	recoveryAttempted, topologyRecovery, err = checkAndRecoverFunction(ctx, analysisEntry, candidateInstanceKey, forceInstanceRecovery, skipProcesses)
	if !recoveryAttempted {
		return recoveryAttempted, topologyRecovery, err
	}
	if topologyRecovery == nil {
		return recoveryAttempted, topologyRecovery, err
	}
	if b, err := json.Marshal(topologyRecovery); err == nil {
		log.Infof("Topology recovery: %+v", string(b))
	} else {
		log.Infof("Topology recovery: %+v", topologyRecovery)
	}
	if !skipProcesses {
		if topologyRecovery.SuccessorKey == nil {
			// Execute general unsuccessful post failover processes
			_ = executeHookPhaseContext(ctx, "post_unsuccessful_failover", "PostUnsuccessfulFailoverProcesses", topologyRecovery)
		} else {
			// Execute general post failover processes
			instdowntime.EndDowntime(topologyRecovery.SuccessorKey)
			_ = executeHookPhaseContext(ctx, "post_failover", "PostFailoverProcesses", topologyRecovery)
		}
	}
	AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("Waiting for %d postponed functions", topologyRecovery.PostponedFunctionsContainer.Len()))
	topologyRecovery.Wait()
	AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("Executed %d postponed functions", topologyRecovery.PostponedFunctionsContainer.Len()))
	if topologyRecovery.PostponedFunctionsContainer.Len() > 0 {
		AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("Executed postponed functions: %+v", strings.Join(topologyRecovery.PostponedFunctionsContainer.Descriptions(), ", ")))
	}
	return recoveryAttempted, topologyRecovery, err
}

// CheckAndRecover is the main entry point for the recovery mechanism
func CheckAndRecover(ctx context.Context, specificInstance *instmodel.InstanceKey, candidateInstanceKey *instmodel.InstanceKey, skipProcesses bool) (recoveryAttempted bool, promotedReplicaKey *instmodel.InstanceKey, err error) {
	// Allow the analysis to run even if we don't want to recover
	replicationAnalysis, err := analysis.GetReplicationAnalysis("", &dto.ReplicationAnalysisHints{IncludeDowntimed: true, AuditAnalysis: true})
	if err != nil {
		return false, nil, log.Errore(err)
	}
	if *config.RuntimeCLIFlags.Noop {
		log.Infof("--noop provided; will not execute processes")
		skipProcesses = true
	}
	// intentionally iterating entries in random order
	for _, j := range rand.Perm(len(replicationAnalysis)) {
		analysisEntry := replicationAnalysis[j]
		if specificInstance != nil {
			// We are looking for a specific instance; if this is not the one, skip!
			if !specificInstance.Equals(&analysisEntry.AnalyzedInstanceKey) {
				continue
			}
		}
		if analysisEntry.SkippableDueToDowntime && specificInstance == nil {
			// Only recover a downtimed server if explicitly requested
			continue
		}

		if specificInstance != nil {
			// force mode. Keep it synchronous
			var topologyRecovery *TopologyRecovery
			recoveryAttempted, topologyRecovery, err = executeCheckAndRecoverFunction(ctx, analysisEntry, candidateInstanceKey, true, skipProcesses)
			log.Errore(err)
			if topologyRecovery != nil {
				promotedReplicaKey = topologyRecovery.SuccessorKey
			}
		} else {
			go func() {
				_, _, err := executeCheckAndRecoverFunction(ctx, analysisEntry, candidateInstanceKey, false, skipProcesses)
				log.Errore(err)
			}()
		}
	}
	return recoveryAttempted, promotedReplicaKey, err
}
