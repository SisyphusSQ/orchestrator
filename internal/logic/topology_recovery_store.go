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

package logic

import (
	"context"
	"fmt"
	"strings"

	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/golib/log"
	"github.com/openark/orchestrator/internal/inst"
	modeldomain "github.com/openark/orchestrator/internal/models/domain"
	"github.com/openark/orchestrator/internal/process"
	"github.com/openark/orchestrator/internal/raft"
	"github.com/openark/orchestrator/internal/recoverypolicy"
	"github.com/openark/orchestrator/internal/repository/metadata"
	"github.com/openark/orchestrator/internal/util"
)

func topologyRecoveryFromRow(row modeldomain.TopologyRecoveryRecord) *TopologyRecovery {
	topologyRecovery := NewTopologyRecovery(inst.ReplicationAnalysis{})
	topologyRecovery.Id = row.RecoveryID
	topologyRecovery.UID = row.UID
	topologyRecovery.IsActive = row.Active
	topologyRecovery.RecoveryStartTimestamp = row.StartActivePeriod
	topologyRecovery.RecoveryEndTimestamp = row.EndRecovery
	topologyRecovery.IsSuccessful = row.Successful
	topologyRecovery.ProcessingNodeHostname = row.ProcessingNodeHostname
	topologyRecovery.ProcessingNodeToken = row.ProcessingNodeToken
	topologyRecovery.PolicyRevision = row.PolicyRevision
	topologyRecovery.HookAssignmentRevision = row.HookAssignmentRevision
	topologyRecovery.AnalysisEntry.AnalyzedInstanceKey = inst.InstanceKey{Hostname: row.Hostname, Port: row.Port}
	topologyRecovery.AnalysisEntry.Analysis = inst.AnalysisCode(row.Analysis)
	topologyRecovery.AnalysisEntry.ClusterDetails.ClusterName = row.ClusterName
	topologyRecovery.AnalysisEntry.ClusterDetails.ClusterAlias = row.ClusterAlias
	topologyRecovery.AnalysisEntry.CountReplicas = row.CountAffectedReplicas
	topologyRecovery.AnalysisEntry.ReadReplicaHostsFromString(row.ReplicaHosts)
	topologyRecovery.SuccessorKey = &inst.InstanceKey{Hostname: row.SuccessorHostname, Port: row.SuccessorPort}
	topologyRecovery.SuccessorAlias = row.SuccessorAlias
	topologyRecovery.AnalysisEntry.ClusterDetails.ReadRecoveryInfo()
	topologyRecovery.AllErrors = strings.Split(row.AllErrors.String, "\n")
	topologyRecovery.LostReplicas.ReadCommaDelimitedList(row.LostReplicas.String)
	topologyRecovery.ParticipatingInstanceKeys.ReadCommaDelimitedList(row.ParticipatingInstances.String)
	topologyRecovery.Acknowledged = row.Acknowledged
	topologyRecovery.AcknowledgedAt = row.AcknowledgedAt.String
	topologyRecovery.AcknowledgedBy = row.AcknowledgedBy.String
	topologyRecovery.AcknowledgedComment = row.AcknowledgedComment.String
	topologyRecovery.LastDetectionId = row.LastDetectionID
	return topologyRecovery
}

func failureDetectionFromRow(row modeldomain.FailureDetectionRecord) *TopologyRecovery {
	failureDetection := &TopologyRecovery{}
	failureDetection.Id = row.DetectionID
	failureDetection.IsActive = row.Active
	failureDetection.RecoveryStartTimestamp = row.StartActivePeriod
	failureDetection.ProcessingNodeHostname = row.ProcessingNodeHostname
	failureDetection.ProcessingNodeToken = row.ProcessingNodeToken
	failureDetection.AnalysisEntry.AnalyzedInstanceKey = inst.InstanceKey{Hostname: row.Hostname, Port: row.Port}
	failureDetection.AnalysisEntry.Analysis = inst.AnalysisCode(row.Analysis)
	failureDetection.AnalysisEntry.ClusterDetails.ClusterName = row.ClusterName
	failureDetection.AnalysisEntry.ClusterDetails.ClusterAlias = row.ClusterAlias
	failureDetection.AnalysisEntry.CountReplicas = row.CountAffectedReplicas
	failureDetection.AnalysisEntry.ReadReplicaHostsFromString(row.ReplicaHosts)
	failureDetection.AnalysisEntry.StartActivePeriod = row.StartActivePeriod
	failureDetection.RelatedRecoveryId = row.RelatedRecoveryID.Int64
	failureDetection.AnalysisEntry.ClusterDetails.ReadRecoveryInfo()
	return failureDetection
}

func blockedRecoveryFromRow(row modeldomain.BlockedRecoveryRecord) BlockedTopologyRecovery {
	return BlockedTopologyRecovery{
		FailedInstanceKey:    inst.InstanceKey{Hostname: row.Hostname, Port: row.Port},
		ClusterName:          row.ClusterName,
		Analysis:             inst.AnalysisCode(row.Analysis),
		LastBlockedTimestamp: row.LastBlockedAt,
		BlockingRecoveryId:   row.BlockingRecoveryID.Int64,
	}
}

func topologyRecoveryStepFromRow(row modeldomain.TopologyRecoveryStepRecord) TopologyRecoveryStep {
	return TopologyRecoveryStep{
		Id:          row.ID,
		RecoveryUID: row.RecoveryUID,
		AuditAt:     row.AuditAt,
		Message:     row.Message,
	}
}

// AttemptFailureDetectionRegistration tries to add a failure-detection entry; if this fails that means the problem has already been detected
func AttemptFailureDetectionRegistration(analysisEntry *inst.ReplicationAnalysis) (registrationSuccessful bool, err error) {
	registered, err := metadata.RegisterFailureDetection(context.Background(), metadata.FailureDetectionRegistration{
		Hostname:           analysisEntry.AnalyzedInstanceKey.Hostname,
		Port:               analysisEntry.AnalyzedInstanceKey.Port,
		ProcessingHostname: process.ThisHostname,
		ProcessingToken:    util.ProcessToken.Hash,
		Analysis:           string(analysisEntry.Analysis),
		ClusterName:        analysisEntry.ClusterDetails.ClusterName,
		ClusterAlias:       analysisEntry.ClusterDetails.ClusterAlias,
		AffectedReplicas:   analysisEntry.CountReplicas,
		ReplicaHosts:       analysisEntry.Replicas.ToCommaDelimitedList(),
		Actionable:         analysisEntry.IsActionableRecovery,
		StartActivePeriod:  analysisEntry.StartActivePeriod,
	})
	return registered, log.Errore(err)
}

// ClearActiveFailureDetections clears the "in_active_period" flag for old-enough detections, thereby allowing for
// further detections on cleared instances.
func ClearActiveFailureDetections() error {
	err := metadata.ClearActiveFailureDetections(context.Background(), recoverypolicy.Current("").FailureDetectionPeriodBlockMinutes)
	return log.Errore(err)
}

// AcknowledgeInstanceFailureDetection clears a failure detection for a particular
// instance. This is automated by recovery process: it makes sense to acknowledge
// the detection of an instance just recovered.
func acknowledgeInstanceFailureDetection(instanceKey *inst.InstanceKey) error {
	return log.Errore(metadata.ClearFailureDetectionsByInstance(
		context.Background(), instanceKey.Hostname, instanceKey.Port,
	))
}

func writeTopologyRecovery(topologyRecovery *TopologyRecovery) (*TopologyRecovery, error) {
	analysisEntry := topologyRecovery.AnalysisEntry
	id, inserted, err := metadata.RegisterTopologyRecovery(context.Background(), metadata.TopologyRecoveryRegistration{
		ID:                     topologyRecovery.Id,
		UID:                    topologyRecovery.UID,
		Hostname:               analysisEntry.AnalyzedInstanceKey.Hostname,
		Port:                   analysisEntry.AnalyzedInstanceKey.Port,
		ProcessingHostname:     process.ThisHostname,
		ProcessingToken:        util.ProcessToken.Hash,
		PolicyRevision:         topologyRecovery.PolicyRevision,
		HookAssignmentRevision: topologyRecovery.HookAssignmentRevision,
		Analysis:               string(analysisEntry.Analysis),
		ClusterName:            analysisEntry.ClusterDetails.ClusterName,
		ClusterAlias:           analysisEntry.ClusterDetails.ClusterAlias,
		AffectedReplicas:       analysisEntry.CountReplicas,
		ReplicaHosts:           analysisEntry.Replicas.ToCommaDelimitedList(),
	})
	if err != nil {
		return nil, err
	}
	if !inserted {
		return nil, nil
	}
	topologyRecovery.Id = id
	return topologyRecovery, nil
}

// AttemptRecoveryRegistration tries to add a recovery entry; if this fails that means recovery is already in place.
func AttemptRecoveryRegistration(analysisEntry *inst.ReplicationAnalysis, failIfFailedInstanceInActiveRecovery bool, failIfClusterInActiveRecovery bool) (*TopologyRecovery, error) {
	if failIfFailedInstanceInActiveRecovery {
		// Let's check if this instance has just been promoted recently and is still in active period.
		// If so, we reject recovery registration to avoid flapping.
		recoveries, err := ReadInActivePeriodSuccessorInstanceRecovery(&analysisEntry.AnalyzedInstanceKey)
		if err != nil {
			return nil, log.Errore(err)
		}
		if len(recoveries) > 0 {
			RegisterBlockedRecoveries(analysisEntry, recoveries)
			return nil, log.Errorf("AttemptRecoveryRegistration: instance %+v has recently been promoted (by failover of %+v) and is in active period. It will not be failed over. You may acknowledge the failure on %+v (-c ack-instance-recoveries) to remove this blockage", analysisEntry.AnalyzedInstanceKey, recoveries[0].AnalysisEntry.AnalyzedInstanceKey, recoveries[0].AnalysisEntry.AnalyzedInstanceKey)
		}
	}
	if failIfClusterInActiveRecovery {
		// Let's check if this cluster has just experienced a failover and is still in active period.
		// If so, we reject recovery registration to avoid flapping.
		recoveries, err := ReadInActivePeriodClusterRecovery(analysisEntry.ClusterDetails.ClusterName)
		if err != nil {
			return nil, log.Errore(err)
		}
		if len(recoveries) > 0 {
			RegisterBlockedRecoveries(analysisEntry, recoveries)
			return nil, log.Errorf("AttemptRecoveryRegistration: cluster %+v has recently experienced a failover (of %+v) and is in active period. It will not be failed over again. You may acknowledge the failure on this cluster (-c ack-cluster-recoveries) or on %+v (-c ack-instance-recoveries) to remove this blockage", analysisEntry.ClusterDetails.ClusterName, recoveries[0].AnalysisEntry.AnalyzedInstanceKey, recoveries[0].AnalysisEntry.AnalyzedInstanceKey)
		}
	}
	if !failIfFailedInstanceInActiveRecovery {
		// Implicitly acknowledge this instance's possibly existing active recovery, provided they are completed.
		AcknowledgeInstanceCompletedRecoveries(&analysisEntry.AnalyzedInstanceKey, "orchestrator", fmt.Sprintf("implicit acknowledge due to user invocation of recovery on same instance: %+v", analysisEntry.AnalyzedInstanceKey))
		// The fact we only acknowledge a completed recovery solves the possible case of two DBAs simultaneously
		// trying to recover the same instance at the same time
	}

	topologyRecovery := NewTopologyRecovery(*analysisEntry)

	topologyRecovery, err := writeTopologyRecovery(topologyRecovery)
	if err != nil {
		return nil, log.Errore(err)
	}

	if _, err := orcraft.PublishCommand("write-recovery", topologyRecovery); err != nil {
		return nil, log.Errore(err)
	}

	return topologyRecovery, nil
}

// ClearActiveRecoveries clears the "in_active_period" flag for old-enough recoveries, thereby allowing for
// further recoveries on cleared instances.
func ClearActiveRecoveries() error {
	err := metadata.ClearActiveRecoveries(context.Background(), recoverypolicy.Current("").RecoveryPeriodBlockSeconds)
	return log.Errore(err)
}

// RegisterBlockedRecoveries writes down currently blocked recoveries, and indicates what recovery they are blocked on.
// Recoveries are blocked through the in_active_period flag, which comes to avoid flapping.
func RegisterBlockedRecoveries(analysisEntry *inst.ReplicationAnalysis, blockingRecoveries []*TopologyRecovery) error {
	for _, recovery := range blockingRecoveries {
		err := metadata.UpsertBlockedRecovery(context.Background(), metadata.BlockedRecoveryRegistration{
			Hostname:           analysisEntry.AnalyzedInstanceKey.Hostname,
			Port:               analysisEntry.AnalyzedInstanceKey.Port,
			ClusterName:        analysisEntry.ClusterDetails.ClusterName,
			Analysis:           string(analysisEntry.Analysis),
			BlockingRecoveryID: recovery.Id,
		})
		if err != nil {
			log.Errore(err)
		}
	}
	return nil
}

// ExpireBlockedRecoveries clears listing of blocked recoveries that are no longer actually blocked.
func ExpireBlockedRecoveries() error {
	return log.Errore(metadata.ExpireBlockedRecoveries(
		context.Background(), config.RecoveryPollSeconds*2,
	))
}

func acknowledgeRecoveries(owner, comment string, filter metadata.RecoveryAcknowledgementFilter) (int64, error) {
	count, err := metadata.AcknowledgeRecoveries(context.Background(), owner, comment, filter)
	return count, log.Errore(err)
}

// AcknowledgeAllRecoveries acknowledges all unacknowledged recoveries.
func AcknowledgeAllRecoveries(owner string, comment string) (countAcknowledgedEntries int64, err error) {
	return acknowledgeRecoveries(owner, comment, metadata.RecoveryAcknowledgementFilter{Kind: metadata.AcknowledgeAll})
}

// AcknowledgeRecovery acknowledges a particular recovery.
// This also implied clearing their active period, which in turn enables further recoveries on those topologies
func AcknowledgeRecovery(recoveryId int64, owner string, comment string) (countAcknowledgedEntries int64, err error) {
	return acknowledgeRecoveries(owner, comment, metadata.RecoveryAcknowledgementFilter{Kind: metadata.AcknowledgeByID, ID: recoveryId})
}

// AcknowledgeRecovery acknowledges a particular recovery.
// This also implied clearing their active period, which in turn enables further recoveries on those topologies
func AcknowledgeRecoveryByUID(recoveryUID string, owner string, comment string) (countAcknowledgedEntries int64, err error) {
	return acknowledgeRecoveries(owner, comment, metadata.RecoveryAcknowledgementFilter{Kind: metadata.AcknowledgeByUID, UID: recoveryUID})
}

// AcknowledgeClusterRecoveries marks active recoveries for given cluster as acknowledged.
// This also implied clearing their active period, which in turn enables further recoveries on those topologies
func AcknowledgeClusterRecoveries(clusterName string, owner string, comment string) (countAcknowledgedEntries int64, err error) {
	{
		if err := metadata.ClearFailureDetectionsByClusterName(context.Background(), clusterName); err != nil {
			return countAcknowledgedEntries, log.Errore(err)
		}
		count, err := acknowledgeRecoveries(owner, comment, metadata.RecoveryAcknowledgementFilter{
			Kind: metadata.AcknowledgeByClusterName, ClusterName: clusterName,
		})
		if err != nil {
			return count, err
		}
		countAcknowledgedEntries = countAcknowledgedEntries + count
	}
	{
		clusterInfo, err := inst.ReadClusterInfo(clusterName)
		if err != nil {
			return countAcknowledgedEntries, err
		}
		if err := metadata.ClearFailureDetectionsByClusterAlias(context.Background(), clusterInfo.ClusterAlias); err != nil {
			return countAcknowledgedEntries, log.Errore(err)
		}
		count, err := acknowledgeRecoveries(owner, comment, metadata.RecoveryAcknowledgementFilter{
			Kind: metadata.AcknowledgeByClusterAlias, ClusterAlias: clusterInfo.ClusterAlias,
		})
		if err != nil {
			return count, err
		}
		countAcknowledgedEntries = countAcknowledgedEntries + count

	}
	return countAcknowledgedEntries, nil
}

// AcknowledgeInstanceRecoveries marks active recoveries for given instance as acknowledged.
// This also implied clearing their active period, which in turn enables further recoveries on those topologies
func AcknowledgeInstanceRecoveries(instanceKey *inst.InstanceKey, owner string, comment string) (countAcknowledgedEntries int64, err error) {
	if err := metadata.ClearFailureDetectionsByInstance(context.Background(), instanceKey.Hostname, instanceKey.Port); err != nil {
		return 0, log.Errore(err)
	}
	return acknowledgeRecoveries(owner, comment, metadata.RecoveryAcknowledgementFilter{
		Kind: metadata.AcknowledgeByInstance, Hostname: instanceKey.Hostname, Port: instanceKey.Port,
	})
}

// AcknowledgeInstanceCompletedRecoveries marks active and COMPLETED recoveries for given instance as acknowledged.
// This also implied clearing their active period, which in turn enables further recoveries on those topologies
func AcknowledgeInstanceCompletedRecoveries(instanceKey *inst.InstanceKey, owner string, comment string) (countAcknowledgedEntries int64, err error) {
	return acknowledgeRecoveries(owner, comment, metadata.RecoveryAcknowledgementFilter{
		Kind: metadata.AcknowledgeCompletedByInstance, Hostname: instanceKey.Hostname, Port: instanceKey.Port,
	})
}

// AcknowledgeCrashedRecoveries marks recoveries whose processing nodes has crashed as acknowledged.
func AcknowledgeCrashedRecoveries() (countAcknowledgedEntries int64, err error) {
	return acknowledgeRecoveries("orchestrator", "detected crashed recovery", metadata.RecoveryAcknowledgementFilter{
		Kind: metadata.AcknowledgeCrashed,
	})
}

// ResolveRecovery is called on completion of a recovery process and updates the recovery status.
// It does not clear the "active period" as this still takes place in order to avoid flapping.
func writeResolveRecovery(topologyRecovery *TopologyRecovery) error {
	var successorKeyToWrite inst.InstanceKey
	if topologyRecovery.IsSuccessful {
		successorKeyToWrite = *topologyRecovery.SuccessorKey
	}
	err := metadata.ResolveTopologyRecovery(context.Background(), metadata.ResolveTopologyRecoveryInput{
		Successful:             topologyRecovery.IsSuccessful,
		SuccessorHostname:      successorKeyToWrite.Hostname,
		SuccessorPort:          successorKeyToWrite.Port,
		SuccessorAlias:         topologyRecovery.SuccessorAlias,
		LostReplicas:           topologyRecovery.LostReplicas.ToCommaDelimitedList(),
		ParticipatingInstances: topologyRecovery.ParticipatingInstanceKeys.ToCommaDelimitedList(),
		AllErrors:              strings.Join(topologyRecovery.AllErrors, "\n"),
		UID:                    topologyRecovery.UID,
	})
	return log.Errore(err)
}

// readRecoveries maps repository rows into recovery business objects.
func readRecoveries(filter metadata.RecoveryReadFilter) ([]*TopologyRecovery, error) {
	res := []*TopologyRecovery{}
	rows, err := metadata.ReadTopologyRecoveries(context.Background(), filter)
	for _, row := range rows {
		res = append(res, topologyRecoveryFromRow(row))
	}

	return res, log.Errore(err)
}

// ReadActiveRecoveries reads active recovery entry/audit entries from topology_recovery
func ReadActiveClusterRecovery(clusterName string) ([]*TopologyRecovery, error) {
	return readRecoveries(metadata.RecoveryReadFilter{
		Kind: metadata.ReadActiveClusterRecovery, ClusterName: clusterName,
	})
}

// ReadInActivePeriodClusterRecovery reads recoveries (possibly complete!) that are in active period.
// (may be used to block further recoveries on this cluster)
func ReadInActivePeriodClusterRecovery(clusterName string) ([]*TopologyRecovery, error) {
	return readRecoveries(metadata.RecoveryReadFilter{
		Kind: metadata.ReadInActivePeriodClusterRecovery, ClusterName: clusterName,
	})
}

// ReadRecentlyActiveClusterRecovery reads recently completed entries for a given cluster
func ReadRecentlyActiveClusterRecovery(clusterName string) ([]*TopologyRecovery, error) {
	return readRecoveries(metadata.RecoveryReadFilter{
		Kind: metadata.ReadRecentlyActiveClusterRecovery, ClusterName: clusterName,
	})
}

// ReadInActivePeriodSuccessorInstanceRecovery reads completed recoveries for a given instance, where said instance
// was promoted as result, still in active period (may be used to block further recoveries should this instance die)
func ReadInActivePeriodSuccessorInstanceRecovery(instanceKey *inst.InstanceKey) ([]*TopologyRecovery, error) {
	return readRecoveries(metadata.RecoveryReadFilter{
		Kind: metadata.ReadInActivePeriodSuccessorRecovery, Hostname: instanceKey.Hostname, Port: instanceKey.Port,
	})
}

// ReadRecentlyActiveInstanceRecovery reads recently completed entries for a given instance
func ReadRecentlyActiveInstanceRecovery(instanceKey *inst.InstanceKey) ([]*TopologyRecovery, error) {
	return readRecoveries(metadata.RecoveryReadFilter{
		Kind: metadata.ReadRecentlyActiveSuccessorRecovery, Hostname: instanceKey.Hostname, Port: instanceKey.Port,
	})
}

// ReadActiveRecoveries reads active recovery entry/audit entries from topology_recovery
func ReadActiveRecoveries() ([]*TopologyRecovery, error) {
	return readRecoveries(metadata.RecoveryReadFilter{Kind: metadata.ReadActiveRecoveries})
}

// ReadCompletedRecoveries reads completed recovery entry/audit entries from topology_recovery
func ReadCompletedRecoveries(page int) ([]*TopologyRecovery, error) {
	return readRecoveries(metadata.RecoveryReadFilter{
		Kind: metadata.ReadCompletedRecoveries, Limit: config.AuditPageSize, Offset: page * config.AuditPageSize,
	})
}

// ReadRecovery reads completed recovery entry/audit entries from topology_recovery
func ReadRecovery(recoveryId int64) ([]*TopologyRecovery, error) {
	return readRecoveries(metadata.RecoveryReadFilter{Kind: metadata.ReadRecoveryByID, ID: recoveryId})
}

// ReadRecoveryByUID reads completed recovery entry/audit entries from topology_recovery
func ReadRecoveryByUID(recoveryUID string) ([]*TopologyRecovery, error) {
	return readRecoveries(metadata.RecoveryReadFilter{Kind: metadata.ReadRecoveryByUID, UID: recoveryUID})
}

// ReadCRecoveries reads latest recovery entries from topology_recovery
func ReadRecentRecoveries(clusterName string, clusterAlias string, unacknowledgedOnly bool, page int) ([]*TopologyRecovery, error) {
	return readRecoveries(metadata.RecoveryReadFilter{
		Kind:               metadata.ReadRecentRecoveries,
		ClusterName:        clusterName,
		ClusterAlias:       clusterAlias,
		UnacknowledgedOnly: unacknowledgedOnly,
		Limit:              config.AuditPageSize,
		Offset:             page * config.AuditPageSize,
	})
}

func readFailureDetections(filter metadata.FailureDetectionReadFilter) ([]*TopologyRecovery, error) {
	res := []*TopologyRecovery{}
	rows, err := metadata.ReadFailureDetections(context.Background(), filter)
	for _, row := range rows {
		res = append(res, failureDetectionFromRow(row))
	}

	return res, log.Errore(err)
}

// ReadRecentFailureDetections
func ReadRecentFailureDetections(clusterAlias string, page int) ([]*TopologyRecovery, error) {
	return readFailureDetections(metadata.FailureDetectionReadFilter{
		ClusterAlias: clusterAlias,
		Limit:        config.AuditPageSize,
		Offset:       page * config.AuditPageSize,
	})
}

// ReadFailureDetection
func ReadFailureDetection(detectionId int64) ([]*TopologyRecovery, error) {
	return readFailureDetections(metadata.FailureDetectionReadFilter{ID: detectionId})
}

// ReadBlockedRecoveries reads blocked recovery entries, potentially filtered by cluster name (empty to unfilter)
func ReadBlockedRecoveries(clusterName string) ([]BlockedTopologyRecovery, error) {
	res := []BlockedTopologyRecovery{}
	rows, err := metadata.ReadBlockedRecoveryRows(context.Background(), clusterName)
	for _, row := range rows {
		res = append(res, blockedRecoveryFromRow(row))
	}

	return res, log.Errore(err)
}

// writeTopologyRecoveryStep writes down a single step in a recovery process
func writeTopologyRecoveryStep(topologyRecoveryStep *TopologyRecoveryStep) error {
	id, err := metadata.WriteTopologyRecoveryStep(context.Background(), modeldomain.TopologyRecoveryStepRecord{
		ID: topologyRecoveryStep.Id, RecoveryUID: topologyRecoveryStep.RecoveryUID, Message: topologyRecoveryStep.Message,
	})
	topologyRecoveryStep.Id = id
	return log.Errore(err)
}

// ReadTopologyRecoverySteps reads recovery steps for a given recovery
func ReadTopologyRecoverySteps(recoveryUID string) ([]TopologyRecoveryStep, error) {
	res := []TopologyRecoveryStep{}
	rows, err := metadata.ReadTopologyRecoveryStepRows(context.Background(), recoveryUID)
	for _, row := range rows {
		res = append(res, topologyRecoveryStepFromRow(row))
	}
	return res, log.Errore(err)
}

// ExpireFailureDetectionHistory removes old rows from the topology_failure_detection table
func ExpireFailureDetectionHistory() error {
	return metadata.ExpireFailureDetectionHistory(context.Background(), config.Config.Audit.PurgeDays)
}

// ExpireTopologyRecoveryHistory removes old rows from the topology_failure_detection table
func ExpireTopologyRecoveryHistory() error {
	return metadata.ExpireTopologyRecoveryHistory(context.Background(), config.Config.Audit.PurgeDays)
}

// ExpireTopologyRecoveryStepsHistory removes old rows from the topology_failure_detection table
func ExpireTopologyRecoveryStepsHistory() error {
	return metadata.ExpireTopologyRecoveryStepsHistory(context.Background(), config.Config.Audit.PurgeDays)
}
