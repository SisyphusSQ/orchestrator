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
	goos "os"
	"strings"
	"time"

	"github.com/openark/orchestrator/internal/os"
	"github.com/openark/orchestrator/internal/process"
	"github.com/openark/orchestrator/internal/recoverypolicy"
)

// prepareCommand replaces agreed-upon placeholders with analysis data
func prepareCommand(command string, topologyRecovery *TopologyRecovery) (result string, async bool) {
	analysisEntry := &topologyRecovery.AnalysisEntry
	command = strings.TrimSpace(command)
	if strings.HasSuffix(command, "&") {
		command = strings.TrimRight(command, "&")
		async = true
	}
	command = strings.Replace(command, "{failureType}", string(analysisEntry.Analysis), -1)
	command = strings.Replace(command, "{instanceType}", string(analysisEntry.GetAnalysisInstanceType()), -1)
	command = strings.Replace(command, "{isMaster}", fmt.Sprintf("%t", analysisEntry.IsMaster), -1)
	command = strings.Replace(command, "{isCoMaster}", fmt.Sprintf("%t", analysisEntry.IsCoMaster), -1)
	command = strings.Replace(command, "{failureDescription}", analysisEntry.Description, -1)
	command = strings.Replace(command, "{command}", analysisEntry.CommandHint, -1)
	command = strings.Replace(command, "{failedHost}", analysisEntry.AnalyzedInstanceKey.Hostname, -1)
	command = strings.Replace(command, "{failedPort}", fmt.Sprintf("%d", analysisEntry.AnalyzedInstanceKey.Port), -1)
	command = strings.Replace(command, "{failureCluster}", analysisEntry.ClusterDetails.ClusterName, -1)
	command = strings.Replace(command, "{failureClusterAlias}", analysisEntry.ClusterDetails.ClusterAlias, -1)
	command = strings.Replace(command, "{failureClusterDomain}", analysisEntry.ClusterDetails.ClusterDomain, -1)
	command = strings.Replace(command, "{countSlaves}", fmt.Sprintf("%d", analysisEntry.CountReplicas), -1)
	command = strings.Replace(command, "{countReplicas}", fmt.Sprintf("%d", analysisEntry.CountReplicas), -1)
	command = strings.Replace(command, "{isDowntimed}", fmt.Sprint(analysisEntry.IsDowntimed), -1)
	command = strings.Replace(command, "{autoMasterRecovery}", fmt.Sprint(analysisEntry.ClusterDetails.HasAutomatedMasterRecovery), -1)
	command = strings.Replace(command, "{autoIntermediateMasterRecovery}", fmt.Sprint(analysisEntry.ClusterDetails.HasAutomatedIntermediateMasterRecovery), -1)
	command = strings.Replace(command, "{orchestratorHost}", process.ThisHostname, -1)
	command = strings.Replace(command, "{recoveryUID}", topologyRecovery.UID, -1)

	command = strings.Replace(command, "{isSuccessful}", fmt.Sprint(topologyRecovery.SuccessorKey != nil), -1)
	if topologyRecovery.SuccessorKey != nil {
		command = strings.Replace(command, "{successorHost}", topologyRecovery.SuccessorKey.Hostname, -1)
		command = strings.Replace(command, "{successorPort}", fmt.Sprintf("%d", topologyRecovery.SuccessorKey.Port), -1)
		// As long as SuccessorBinlogCoordinates != nil, we replace {successorBinlogCoordinates}
		// Format of the display string of binlog coordinates would be LogFile:LogPositon
		if topologyRecovery.SuccessorBinlogCoordinates != nil {
			command = strings.Replace(command, "{successorBinlogCoordinates}", topologyRecovery.SuccessorBinlogCoordinates.DisplayString(), -1)
		}
		// As long as SucesssorKey != nil, we replace {successorAlias}.
		// If SucessorAlias is "", it's fine. We'll replace {successorAlias} with "".
		command = strings.Replace(command, "{successorAlias}", topologyRecovery.SuccessorAlias, -1)
	}

	command = strings.Replace(command, "{lostSlaves}", topologyRecovery.LostReplicas.ToCommaDelimitedList(), -1)
	command = strings.Replace(command, "{lostReplicas}", topologyRecovery.LostReplicas.ToCommaDelimitedList(), -1)
	command = strings.Replace(command, "{countLostReplicas}", fmt.Sprintf("%d", len(topologyRecovery.LostReplicas)), -1)
	command = strings.Replace(command, "{slaveHosts}", analysisEntry.Replicas.ToCommaDelimitedList(), -1)
	command = strings.Replace(command, "{replicaHosts}", analysisEntry.Replicas.ToCommaDelimitedList(), -1)

	return command, async
}

// applyEnvironmentVariables sets the relevant environment variables for a recovery
func applyEnvironmentVariables(topologyRecovery *TopologyRecovery) []string {
	analysisEntry := &topologyRecovery.AnalysisEntry
	env := goos.Environ()
	env = append(env, fmt.Sprintf("ORC_FAILURE_TYPE=%s", string(analysisEntry.Analysis)))
	env = append(env, fmt.Sprintf("ORC_INSTANCE_TYPE=%s", string(analysisEntry.GetAnalysisInstanceType())))
	env = append(env, fmt.Sprintf("ORC_IS_MASTER=%t", analysisEntry.IsMaster))
	env = append(env, fmt.Sprintf("ORC_IS_CO_MASTER=%t", analysisEntry.IsCoMaster))
	env = append(env, fmt.Sprintf("ORC_FAILURE_DESCRIPTION=%s", analysisEntry.Description))
	env = append(env, fmt.Sprintf("ORC_COMMAND=%s", analysisEntry.CommandHint))
	env = append(env, fmt.Sprintf("ORC_FAILED_HOST=%s", analysisEntry.AnalyzedInstanceKey.Hostname))
	env = append(env, fmt.Sprintf("ORC_FAILED_PORT=%d", analysisEntry.AnalyzedInstanceKey.Port))
	env = append(env, fmt.Sprintf("ORC_FAILURE_CLUSTER=%s", analysisEntry.ClusterDetails.ClusterName))
	env = append(env, fmt.Sprintf("ORC_FAILURE_CLUSTER_ALIAS=%s", analysisEntry.ClusterDetails.ClusterAlias))
	env = append(env, fmt.Sprintf("ORC_FAILURE_CLUSTER_DOMAIN=%s", analysisEntry.ClusterDetails.ClusterDomain))
	env = append(env, fmt.Sprintf("ORC_COUNT_REPLICAS=%d", analysisEntry.CountReplicas))
	env = append(env, fmt.Sprintf("ORC_IS_DOWNTIMED=%v", analysisEntry.IsDowntimed))
	env = append(env, fmt.Sprintf("ORC_AUTO_MASTER_RECOVERY=%v", analysisEntry.ClusterDetails.HasAutomatedMasterRecovery))
	env = append(env, fmt.Sprintf("ORC_AUTO_INTERMEDIATE_MASTER_RECOVERY=%v", analysisEntry.ClusterDetails.HasAutomatedIntermediateMasterRecovery))
	env = append(env, fmt.Sprintf("ORC_ORCHESTRATOR_HOST=%s", process.ThisHostname))
	env = append(env, fmt.Sprintf("ORC_IS_SUCCESSFUL=%v", (topologyRecovery.SuccessorKey != nil)))
	env = append(env, fmt.Sprintf("ORC_LOST_REPLICAS=%s", topologyRecovery.LostReplicas.ToCommaDelimitedList()))
	env = append(env, fmt.Sprintf("ORC_REPLICA_HOSTS=%s", analysisEntry.Replicas.ToCommaDelimitedList()))
	env = append(env, fmt.Sprintf("ORC_RECOVERY_UID=%s", topologyRecovery.UID))

	if topologyRecovery.SuccessorKey != nil {
		env = append(env, fmt.Sprintf("ORC_SUCCESSOR_HOST=%s", topologyRecovery.SuccessorKey.Hostname))
		env = append(env, fmt.Sprintf("ORC_SUCCESSOR_PORT=%d", topologyRecovery.SuccessorKey.Port))
		// As long as SuccessorBinlogCoordinates != nil, we set ORC_SUCCESSOR_BINLOG_COORDINATES
		// Format of the display string of binlog coordinates would be LogFile:LogPositon
		if topologyRecovery.SuccessorBinlogCoordinates != nil {
			env = append(env, fmt.Sprintf("ORC_SUCCESSOR_BINLOG_COORDINATES=%s", topologyRecovery.SuccessorBinlogCoordinates.DisplayString()))
		}
		// As long as SucesssorKey != nil, we replace {successorAlias}.
		// If SucessorAlias is "", it's fine. We'll replace {successorAlias} with "".
		env = append(env, fmt.Sprintf("ORC_SUCCESSOR_ALIAS=%s", topologyRecovery.SuccessorAlias))
	}

	return env
}

func executeHookPhaseContext(ctx context.Context, phase, description string, topologyRecovery *TopologyRecovery) error {
	snapshot := recoverypolicy.SnapshotFromContext(ctx)
	if snapshot == nil {
		return fmt.Errorf("%s requires a recovery execution snapshot", description)
	}
	hooks := snapshot.Hooks
	if topologyRecovery != nil {
		topologyRecovery.PolicyRevision = snapshot.PolicyRevision
		topologyRecovery.HookAssignmentRevision = snapshot.HookRevision
	}
	profiles := hooks[phase]
	if len(profiles) == 0 {
		AuditTopologyRecovery(topologyRecovery, fmt.Sprintf("No %s hooks to run", description))
		return nil
	}
	for _, profile := range profiles {
		for index, rawCommand := range profile.Commands {
			command, _ := prepareCommand(rawCommand, topologyRecovery)
			commandCtx, cancel := context.WithTimeout(ctx, time.Duration(profile.TimeoutSeconds)*time.Second)
			start := time.Now()
			output, commandErr := os.CommandRunContext(commandCtx, command, applyEnvironmentVariables(topologyRecovery), profile.OutputLimitBytes)
			output = recoverypolicy.RedactOutput(output)
			cancel()
			message := fmt.Sprintf("%s profile=%s revision=%d command=%d/%d duration=%s output=%q", description, profile.ID, profile.Revision, index+1, len(profile.Commands), time.Since(start).Round(time.Millisecond), output)
			if commandErr != nil {
				AuditTopologyRecovery(topologyRecovery, message+fmt.Sprintf(" error=%v", commandErr))
				if profile.FailurePolicy == "abort" {
					return commandErr
				}
				continue
			}
			AuditTopologyRecovery(topologyRecovery, message+" status=completed")
		}
	}
	return nil
}
