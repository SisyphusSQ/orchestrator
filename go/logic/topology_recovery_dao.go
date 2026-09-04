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
	"database/sql"
	"fmt"
	"strings"

	"github.com/openark/golib/log"
	"github.com/openark/orchestrator/go/config"
	"github.com/openark/orchestrator/go/db"
	"github.com/openark/orchestrator/go/inst"
	"github.com/openark/orchestrator/go/process"
	"github.com/openark/orchestrator/go/raft"
	"github.com/openark/orchestrator/go/util"
)

type topologyRecoveryRow struct {
	RecoveryID             int64          `gorm:"column:recovery_id"`
	UID                    string         `gorm:"column:uid"`
	Hostname               string         `gorm:"column:hostname"`
	Port                   int            `gorm:"column:port"`
	Active                 bool           `gorm:"column:is_active"`
	StartActivePeriod      string         `gorm:"column:start_active_period"`
	EndRecovery            string         `gorm:"column:end_recovery"`
	Successful             bool           `gorm:"column:is_successful"`
	ProcessingNodeHostname string         `gorm:"column:processing_node_hostname"`
	ProcessingNodeToken    string         `gorm:"column:processcing_node_token"`
	SuccessorHostname      string         `gorm:"column:successor_hostname"`
	SuccessorPort          int            `gorm:"column:successor_port"`
	SuccessorAlias         string         `gorm:"column:successor_alias"`
	Analysis               string         `gorm:"column:analysis"`
	ClusterName            string         `gorm:"column:cluster_name"`
	ClusterAlias           string         `gorm:"column:cluster_alias"`
	CountAffectedReplicas  uint           `gorm:"column:count_affected_slaves"`
	ReplicaHosts           string         `gorm:"column:slave_hosts"`
	ParticipatingInstances sql.NullString `gorm:"column:participating_instances"`
	LostReplicas           sql.NullString `gorm:"column:lost_slaves"`
	AllErrors              sql.NullString `gorm:"column:all_errors"`
	Acknowledged           bool           `gorm:"column:acknowledged"`
	AcknowledgedAt         sql.NullString `gorm:"column:acknowledged_at"`
	AcknowledgedBy         sql.NullString `gorm:"column:acknowledged_by"`
	AcknowledgedComment    sql.NullString `gorm:"column:acknowledge_comment"`
	LastDetectionID        int64          `gorm:"column:last_detection_id"`
}

type failureDetectionRow struct {
	DetectionID            int64         `gorm:"column:detection_id"`
	Hostname               string        `gorm:"column:hostname"`
	Port                   int           `gorm:"column:port"`
	Active                 bool          `gorm:"column:is_active"`
	StartActivePeriod      string        `gorm:"column:start_active_period"`
	ProcessingNodeHostname string        `gorm:"column:processing_node_hostname"`
	ProcessingNodeToken    string        `gorm:"column:processcing_node_token"`
	Analysis               string        `gorm:"column:analysis"`
	ClusterName            string        `gorm:"column:cluster_name"`
	ClusterAlias           string        `gorm:"column:cluster_alias"`
	CountAffectedReplicas  uint          `gorm:"column:count_affected_slaves"`
	ReplicaHosts           string        `gorm:"column:slave_hosts"`
	RelatedRecoveryID      sql.NullInt64 `gorm:"column:related_recovery_id"`
}

type blockedRecoveryRow struct {
	Hostname           string        `gorm:"column:hostname"`
	Port               int           `gorm:"column:port"`
	ClusterName        string        `gorm:"column:cluster_name"`
	Analysis           string        `gorm:"column:analysis"`
	LastBlockedAt      string        `gorm:"column:last_blocked_timestamp"`
	BlockingRecoveryID sql.NullInt64 `gorm:"column:blocking_recovery_id"`
}

type topologyRecoveryStepRow struct {
	ID          int64  `gorm:"column:recovery_step_id"`
	RecoveryUID string `gorm:"column:recovery_uid"`
	AuditAt     string `gorm:"column:audit_at"`
	Message     string `gorm:"column:message"`
}

func topologyRecoveryFromRow(row topologyRecoveryRow) *TopologyRecovery {
	topologyRecovery := NewTopologyRecovery(inst.ReplicationAnalysis{})
	topologyRecovery.Id = row.RecoveryID
	topologyRecovery.UID = row.UID
	topologyRecovery.IsActive = row.Active
	topologyRecovery.RecoveryStartTimestamp = row.StartActivePeriod
	topologyRecovery.RecoveryEndTimestamp = row.EndRecovery
	topologyRecovery.IsSuccessful = row.Successful
	topologyRecovery.ProcessingNodeHostname = row.ProcessingNodeHostname
	topologyRecovery.ProcessingNodeToken = row.ProcessingNodeToken
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

func failureDetectionFromRow(row failureDetectionRow) *TopologyRecovery {
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

func blockedRecoveryFromRow(row blockedRecoveryRow) BlockedTopologyRecovery {
	return BlockedTopologyRecovery{
		FailedInstanceKey:    inst.InstanceKey{Hostname: row.Hostname, Port: row.Port},
		ClusterName:          row.ClusterName,
		Analysis:             inst.AnalysisCode(row.Analysis),
		LastBlockedTimestamp: row.LastBlockedAt,
		BlockingRecoveryId:   row.BlockingRecoveryID.Int64,
	}
}

func topologyRecoveryStepFromRow(row topologyRecoveryStepRow) TopologyRecoveryStep {
	return TopologyRecoveryStep{
		Id:          row.ID,
		RecoveryUID: row.RecoveryUID,
		AuditAt:     row.AuditAt,
		Message:     row.Message,
	}
}

// AttemptFailureDetectionRegistration tries to add a failure-detection entry; if this fails that means the problem has already been detected
func AttemptFailureDetectionRegistration(analysisEntry *inst.ReplicationAnalysis) (registrationSuccessful bool, err error) {
	args := []interface{}{
		analysisEntry.AnalyzedInstanceKey.Hostname,
		analysisEntry.AnalyzedInstanceKey.Port,
		process.ThisHostname,
		util.ProcessToken.Hash,
		string(analysisEntry.Analysis),
		analysisEntry.ClusterDetails.ClusterName,
		analysisEntry.ClusterDetails.ClusterAlias,
		analysisEntry.CountReplicas,
		analysisEntry.Replicas.ToCommaDelimitedList(),
		analysisEntry.IsActionableRecovery,
	}
	startActivePeriodHint := "now()"
	if analysisEntry.StartActivePeriod != "" {
		startActivePeriodHint = "?"
		args = append(args, analysisEntry.StartActivePeriod)
	}

	query := fmt.Sprintf(`
			insert ignore
				into topology_failure_detection (
					hostname,
					port,
					in_active_period,
					end_active_period_unixtime,
					processing_node_hostname,
					processcing_node_token,
					analysis,
					cluster_name,
					cluster_alias,
					count_affected_slaves,
					slave_hosts,
					is_actionable,
					start_active_period
				) values (
					?,
					?,
					1,
					0,
					?,
					?,
					?,
					?,
					?,
					?,
					?,
					?,
					%s
				)
			`, startActivePeriodHint)

	sqlResult, err := db.ExecOrchestrator(query, args...)
	if err != nil {
		return false, log.Errore(err)
	}
	rows, err := sqlResult.RowsAffected()
	if err != nil {
		return false, log.Errore(err)
	}
	return (rows > 0), nil
}

// ClearActiveFailureDetections clears the "in_active_period" flag for old-enough detections, thereby allowing for
// further detections on cleared instances.
func ClearActiveFailureDetections() error {
	_, err := db.ExecOrchestrator(`
			update topology_failure_detection set
				in_active_period = 0,
				end_active_period_unixtime = UNIX_TIMESTAMP()
			where
				in_active_period = 1
				AND start_active_period < NOW() - INTERVAL ? MINUTE
			`,
		config.Config.FailureDetectionPeriodBlockMinutes,
	)
	return log.Errore(err)
}

// clearAcknowledgedFailureDetections clears the "in_active_period" flag for detections
// that were acknowledged
func clearAcknowledgedFailureDetections(whereClause string, args []interface{}) error {
	query := fmt.Sprintf(`
			update topology_failure_detection set
				in_active_period = 0,
				end_active_period_unixtime = UNIX_TIMESTAMP()
			where
				in_active_period = 1
				and %s
			`, whereClause)
	_, err := db.ExecOrchestrator(query, args...)
	return log.Errore(err)
}

// AcknowledgeInstanceFailureDetection clears a failure detection for a particular
// instance. This is automated by recovery process: it makes sense to acknowledge
// the detection of an instance just recovered.
func acknowledgeInstanceFailureDetection(instanceKey *inst.InstanceKey) error {
	whereClause := `
			hostname = ?
			and port = ?
		`
	args := []interface{}{instanceKey.Hostname, instanceKey.Port}
	return clearAcknowledgedFailureDetections(whereClause, args)
}

func writeTopologyRecovery(topologyRecovery *TopologyRecovery) (*TopologyRecovery, error) {
	analysisEntry := topologyRecovery.AnalysisEntry
	sqlResult, err := db.ExecOrchestratorSQLContext(context.Background(), `
			insert ignore
				into topology_recovery (
					recovery_id,
					uid,
					hostname,
					port,
					in_active_period,
					start_active_period,
					end_active_period_unixtime,
					processing_node_hostname,
					processcing_node_token,
					analysis,
					cluster_name,
					cluster_alias,
					count_affected_slaves,
					slave_hosts,
					last_detection_id
				) values (
					?,
					?,
					?,
					?,
					1,
					NOW(),
					0,
					?,
					?,
					?,
					?,
					?,
					?,
					?,
					(select ifnull(max(detection_id), 0) from topology_failure_detection where hostname=? and port=?)
				)
			`,
		db.NilIfZero(topologyRecovery.Id),
		topologyRecovery.UID,
		analysisEntry.AnalyzedInstanceKey.Hostname, analysisEntry.AnalyzedInstanceKey.Port,
		process.ThisHostname, util.ProcessToken.Hash,
		string(analysisEntry.Analysis),
		analysisEntry.ClusterDetails.ClusterName,
		analysisEntry.ClusterDetails.ClusterAlias,
		analysisEntry.CountReplicas, analysisEntry.Replicas.ToCommaDelimitedList(),
		analysisEntry.AnalyzedInstanceKey.Hostname, analysisEntry.AnalyzedInstanceKey.Port,
	)
	if err != nil {
		return nil, err
	}
	rows, err := sqlResult.RowsAffected()
	if err != nil {
		return nil, err
	}
	if rows == 0 {
		return nil, nil
	}
	lastInsertId, err := sqlResult.LastInsertId()
	if err != nil {
		return nil, err
	}
	topologyRecovery.Id = lastInsertId
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
	if orcraft.IsRaftEnabled() {
		if _, err := orcraft.PublishCommand("write-recovery", topologyRecovery); err != nil {
			return nil, log.Errore(err)
		}
	}
	return topologyRecovery, nil
}

// ClearActiveRecoveries clears the "in_active_period" flag for old-enough recoveries, thereby allowing for
// further recoveries on cleared instances.
func ClearActiveRecoveries() error {
	_, err := db.ExecOrchestrator(`
			update topology_recovery set
				in_active_period = 0,
				end_active_period_unixtime = UNIX_TIMESTAMP()
			where
				in_active_period = 1
				AND start_active_period < NOW() - INTERVAL ? SECOND
			`,
		config.Config.RecoveryPeriodBlockSeconds,
	)
	return log.Errore(err)
}

// RegisterBlockedRecoveries writes down currently blocked recoveries, and indicates what recovery they are blocked on.
// Recoveries are blocked through the in_active_period flag, which comes to avoid flapping.
func RegisterBlockedRecoveries(analysisEntry *inst.ReplicationAnalysis, blockingRecoveries []*TopologyRecovery) error {
	for _, recovery := range blockingRecoveries {
		_, err := db.ExecOrchestrator(`
			insert
				into blocked_topology_recovery (
					hostname,
					port,
					cluster_name,
					analysis,
					last_blocked_timestamp,
					blocking_recovery_id
				) values (
					?,
					?,
					?,
					?,
					NOW(),
					?
				)
				on duplicate key update
					cluster_name=values(cluster_name),
					analysis=values(analysis),
					last_blocked_timestamp=values(last_blocked_timestamp),
					blocking_recovery_id=values(blocking_recovery_id)
			`, analysisEntry.AnalyzedInstanceKey.Hostname,
			analysisEntry.AnalyzedInstanceKey.Port,
			analysisEntry.ClusterDetails.ClusterName,
			string(analysisEntry.Analysis),
			recovery.Id,
		)
		if err != nil {
			log.Errore(err)
		}
	}
	return nil
}

// ExpireBlockedRecoveries clears listing of blocked recoveries that are no longer actually blocked.
func ExpireBlockedRecoveries() error {
	type instanceKeyRow struct {
		Hostname string `gorm:"column:hostname"`
		Port     int    `gorm:"column:port"`
	}
	// Older recovery is acknowledged by now, hence blocked recovery should be released.
	// Do NOTE that the data in blocked_topology_recovery is only used for auditing: it is NOT the data
	// based on which we make automated decisions.

	query := `
		select
				blocked_topology_recovery.hostname,
				blocked_topology_recovery.port
			from
				blocked_topology_recovery
				left join topology_recovery on (blocking_recovery_id = topology_recovery.recovery_id and acknowledged = 0)
			where
				acknowledged is null
		`
	expiredKeys := inst.NewInstanceKeyMap()
	rows, err := db.QueryOrchestratorRows[instanceKeyRow](context.Background(), query)
	for _, row := range rows {
		key := inst.InstanceKey{Hostname: row.Hostname, Port: row.Port}
		expiredKeys.AddKey(key)
	}

	for _, expiredKey := range expiredKeys.GetInstanceKeys() {
		_, err := db.ExecOrchestrator(`
				delete
					from blocked_topology_recovery
				where
						hostname = ?
						and port = ?
				`,
			expiredKey.Hostname, expiredKey.Port,
		)
		if err != nil {
			return log.Errore(err)
		}
	}

	if err != nil {
		return log.Errore(err)
	}
	// Some oversampling, if a problem has not been noticed for some time (e.g. the server came up alive
	// before action was taken), expire it.
	// Recall that RegisterBlockedRecoveries continuously updates the last_blocked_timestamp column.
	_, err = db.ExecOrchestrator(`
			delete
				from blocked_topology_recovery
				where
					last_blocked_timestamp < NOW() - interval ? second
			`, (config.RecoveryPollSeconds * 2),
	)
	return log.Errore(err)
}

// acknowledgeRecoveries sets acknowledged* details and clears the in_active_period flags from a set of entries
func acknowledgeRecoveries(owner string, comment string, markEndRecovery bool, whereClause string, args []interface{}) (countAcknowledgedEntries int64, err error) {
	additionalSet := ``
	if markEndRecovery {
		additionalSet = `
				end_recovery=IFNULL(end_recovery, NOW()),
			`
	}
	query := fmt.Sprintf(`
			update topology_recovery set
				in_active_period = 0,
				end_active_period_unixtime = case when end_active_period_unixtime = 0 then UNIX_TIMESTAMP() else end_active_period_unixtime end,
				%s
				acknowledged = 1,
				acknowledged_at = NOW(),
				acknowledged_by = ?,
				acknowledge_comment = ?
			where
				acknowledged = 0
				and
				%s
		`, additionalSet, whereClause)
	args = append([]interface{}{owner, comment}, args...)
	sqlResult, err := db.ExecOrchestrator(query, args...)
	if err != nil {
		return 0, log.Errore(err)
	}
	rows, err := sqlResult.RowsAffected()
	return rows, log.Errore(err)
}

// AcknowledgeAllRecoveries acknowledges all unacknowledged recoveries.
func AcknowledgeAllRecoveries(owner string, comment string) (countAcknowledgedEntries int64, err error) {
	whereClause := `1 = 1`
	return acknowledgeRecoveries(owner, comment, false, whereClause, nil)
}

// AcknowledgeRecovery acknowledges a particular recovery.
// This also implied clearing their active period, which in turn enables further recoveries on those topologies
func AcknowledgeRecovery(recoveryId int64, owner string, comment string) (countAcknowledgedEntries int64, err error) {
	whereClause := `recovery_id = ?`
	return acknowledgeRecoveries(owner, comment, false, whereClause, []interface{}{recoveryId})
}

// AcknowledgeRecovery acknowledges a particular recovery.
// This also implied clearing their active period, which in turn enables further recoveries on those topologies
func AcknowledgeRecoveryByUID(recoveryUID string, owner string, comment string) (countAcknowledgedEntries int64, err error) {
	whereClause := `uid = ?`
	return acknowledgeRecoveries(owner, comment, false, whereClause, []interface{}{recoveryUID})
}

// AcknowledgeClusterRecoveries marks active recoveries for given cluster as acknowledged.
// This also implied clearing their active period, which in turn enables further recoveries on those topologies
func AcknowledgeClusterRecoveries(clusterName string, owner string, comment string) (countAcknowledgedEntries int64, err error) {
	{
		whereClause := `cluster_name = ?`
		args := []interface{}{clusterName}
		clearAcknowledgedFailureDetections(whereClause, args)
		count, err := acknowledgeRecoveries(owner, comment, false, whereClause, args)
		if err != nil {
			return count, err
		}
		countAcknowledgedEntries = countAcknowledgedEntries + count
	}
	{
		clusterInfo, err := inst.ReadClusterInfo(clusterName)
		whereClause := `cluster_alias = ? and cluster_alias != ''`
		args := []interface{}{clusterInfo.ClusterAlias}
		clearAcknowledgedFailureDetections(whereClause, args)
		count, err := acknowledgeRecoveries(owner, comment, false, whereClause, args)
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
	whereClause := `
			hostname = ?
			and port = ?
		`
	args := []interface{}{instanceKey.Hostname, instanceKey.Port}
	clearAcknowledgedFailureDetections(whereClause, args)
	return acknowledgeRecoveries(owner, comment, false, whereClause, args)
}

// AcknowledgeInstanceCompletedRecoveries marks active and COMPLETED recoveries for given instance as acknowledged.
// This also implied clearing their active period, which in turn enables further recoveries on those topologies
func AcknowledgeInstanceCompletedRecoveries(instanceKey *inst.InstanceKey, owner string, comment string) (countAcknowledgedEntries int64, err error) {
	whereClause := `
			hostname = ?
			and port = ?
			and end_recovery is not null
		`
	return acknowledgeRecoveries(owner, comment, false, whereClause, []interface{}{instanceKey.Hostname, instanceKey.Port})
}

// AcknowledgeCrashedRecoveries marks recoveries whose processing nodes has crashed as acknowledged.
func AcknowledgeCrashedRecoveries() (countAcknowledgedEntries int64, err error) {
	whereClause := `
			in_active_period = 1
			and end_recovery is null
			and concat(processing_node_hostname, ':', processcing_node_token) not in (
				select concat(hostname, ':', token) from node_health
			)
		`
	return acknowledgeRecoveries("orchestrator", "detected crashed recovery", true, whereClause, nil)
}

// ResolveRecovery is called on completion of a recovery process and updates the recovery status.
// It does not clear the "active period" as this still takes place in order to avoid flapping.
func writeResolveRecovery(topologyRecovery *TopologyRecovery) error {
	var successorKeyToWrite inst.InstanceKey
	if topologyRecovery.IsSuccessful {
		successorKeyToWrite = *topologyRecovery.SuccessorKey
	}
	_, err := db.ExecOrchestrator(`
			update topology_recovery set
				is_successful = ?,
				successor_hostname = ?,
				successor_port = ?,
				successor_alias = ?,
				lost_slaves = ?,
				participating_instances = ?,
				all_errors = ?,
				end_recovery = NOW()
			where
				uid = ?
			`, topologyRecovery.IsSuccessful, successorKeyToWrite.Hostname, successorKeyToWrite.Port,
		topologyRecovery.SuccessorAlias, topologyRecovery.LostReplicas.ToCommaDelimitedList(),
		topologyRecovery.ParticipatingInstanceKeys.ToCommaDelimitedList(),
		strings.Join(topologyRecovery.AllErrors, "\n"),
		topologyRecovery.UID,
	)
	return log.Errore(err)
}

// readRecoveries reads recovery entry/audit entries from topology_recovery
func readRecoveries(whereCondition string, limit string, args []interface{}) ([]*TopologyRecovery, error) {
	res := []*TopologyRecovery{}
	query := fmt.Sprintf(`
		select
      recovery_id,
			uid,
      hostname,
      port,
      (IFNULL(end_active_period_unixtime, 0) = 0) as is_active,
      start_active_period,
      IFNULL(end_active_period_unixtime, 0) as end_active_period_unixtime,
      IFNULL(end_recovery, '') AS end_recovery,
      is_successful,
      processing_node_hostname,
      processcing_node_token,
      ifnull(successor_hostname, '') as successor_hostname,
      ifnull(successor_port, 0) as successor_port,
      ifnull(successor_alias, '') as successor_alias,
      analysis,
      cluster_name,
      cluster_alias,
      count_affected_slaves,
      slave_hosts,
      participating_instances,
      lost_slaves,
      all_errors,
      acknowledged,
      acknowledged_at,
      acknowledged_by,
      acknowledge_comment,
      last_detection_id
		from
			topology_recovery
		%s
		order by
			recovery_id desc
		%s
		`, whereCondition, limit)
	rows, err := db.QueryOrchestratorRows[topologyRecoveryRow](context.Background(), query, args...)
	for _, row := range rows {
		res = append(res, topologyRecoveryFromRow(row))
	}

	return res, log.Errore(err)
}

// ReadActiveRecoveries reads active recovery entry/audit entries from topology_recovery
func ReadActiveClusterRecovery(clusterName string) ([]*TopologyRecovery, error) {
	whereClause := `
		where
			in_active_period=1
			and end_recovery is null
			and cluster_name=?`
	return readRecoveries(whereClause, ``, []interface{}{clusterName})
}

// ReadInActivePeriodClusterRecovery reads recoveries (possibly complete!) that are in active period.
// (may be used to block further recoveries on this cluster)
func ReadInActivePeriodClusterRecovery(clusterName string) ([]*TopologyRecovery, error) {
	whereClause := `
		where
			in_active_period=1
			and cluster_name=?`
	return readRecoveries(whereClause, ``, []interface{}{clusterName})
}

// ReadRecentlyActiveClusterRecovery reads recently completed entries for a given cluster
func ReadRecentlyActiveClusterRecovery(clusterName string) ([]*TopologyRecovery, error) {
	whereClause := `
		where
			end_recovery > now() - interval 5 minute
			and cluster_name=?`
	return readRecoveries(whereClause, ``, []interface{}{clusterName})
}

// ReadInActivePeriodSuccessorInstanceRecovery reads completed recoveries for a given instance, where said instance
// was promoted as result, still in active period (may be used to block further recoveries should this instance die)
func ReadInActivePeriodSuccessorInstanceRecovery(instanceKey *inst.InstanceKey) ([]*TopologyRecovery, error) {
	whereClause := `
		where
			in_active_period=1
			and
				successor_hostname=? and successor_port=?`
	return readRecoveries(whereClause, ``, []interface{}{instanceKey.Hostname, instanceKey.Port})
}

// ReadRecentlyActiveInstanceRecovery reads recently completed entries for a given instance
func ReadRecentlyActiveInstanceRecovery(instanceKey *inst.InstanceKey) ([]*TopologyRecovery, error) {
	whereClause := `
		where
			end_recovery > now() - interval 5 minute
			and
				successor_hostname=? and successor_port=?`
	return readRecoveries(whereClause, ``, []interface{}{instanceKey.Hostname, instanceKey.Port})
}

// ReadActiveRecoveries reads active recovery entry/audit entries from topology_recovery
func ReadActiveRecoveries() ([]*TopologyRecovery, error) {
	return readRecoveries(`
		where
			in_active_period=1
			and end_recovery is null`,
		``, nil)
}

// ReadCompletedRecoveries reads completed recovery entry/audit entries from topology_recovery
func ReadCompletedRecoveries(page int) ([]*TopologyRecovery, error) {
	limit := `
		limit ?
		offset ?`
	return readRecoveries(`where end_recovery is not null`, limit, []interface{}{config.AuditPageSize, page * config.AuditPageSize})
}

// ReadRecovery reads completed recovery entry/audit entries from topology_recovery
func ReadRecovery(recoveryId int64) ([]*TopologyRecovery, error) {
	whereClause := `where recovery_id = ?`
	return readRecoveries(whereClause, ``, []interface{}{recoveryId})
}

// ReadRecoveryByUID reads completed recovery entry/audit entries from topology_recovery
func ReadRecoveryByUID(recoveryUID string) ([]*TopologyRecovery, error) {
	whereClause := `where uid = ?`
	return readRecoveries(whereClause, ``, []interface{}{recoveryUID})
}

// ReadCRecoveries reads latest recovery entries from topology_recovery
func ReadRecentRecoveries(clusterName string, clusterAlias string, unacknowledgedOnly bool, page int) ([]*TopologyRecovery, error) {
	whereConditions := []string{}
	whereClause := ""
	args := []interface{}{}
	if unacknowledgedOnly {
		whereConditions = append(whereConditions, `acknowledged=0`)
	}
	if clusterName != "" {
		whereConditions = append(whereConditions, `cluster_name=?`)
		args = append(args, clusterName)
	} else if clusterAlias != "" {
		whereConditions = append(whereConditions, `cluster_alias=?`)
		args = append(args, clusterAlias)
	}
	if len(whereConditions) > 0 {
		whereClause = fmt.Sprintf("where %s", strings.Join(whereConditions, " and "))
	}
	limit := `
		limit ?
		offset ?`
	args = append(args, config.AuditPageSize, page*config.AuditPageSize)
	return readRecoveries(whereClause, limit, args)
}

// readRecoveries reads recovery entry/audit entries from topology_recovery
func readFailureDetections(whereCondition string, limit string, args []interface{}) ([]*TopologyRecovery, error) {
	res := []*TopologyRecovery{}
	query := fmt.Sprintf(`
		select
      detection_id,
      hostname,
      port,
      in_active_period as is_active,
      start_active_period,
      end_active_period_unixtime,
      processing_node_hostname,
      processcing_node_token,
      analysis,
      cluster_name,
      cluster_alias,
      count_affected_slaves,
      slave_hosts,
      (select max(recovery_id) from topology_recovery where topology_recovery.last_detection_id = detection_id) as related_recovery_id
		from
			topology_failure_detection
		%s
		order by
			detection_id desc
		%s
		`, whereCondition, limit)
	rows, err := db.QueryOrchestratorRows[failureDetectionRow](context.Background(), query, args...)
	for _, row := range rows {
		res = append(res, failureDetectionFromRow(row))
	}

	return res, log.Errore(err)
}

// ReadRecentFailureDetections
func ReadRecentFailureDetections(clusterAlias string, page int) ([]*TopologyRecovery, error) {
	whereClause := ""
	args := []interface{}{}
	if clusterAlias != "" {
		whereClause = `where cluster_alias = ?`
		args = append(args, clusterAlias)
	}
	limit := `
		limit ?
		offset ?`
	args = append(args, config.AuditPageSize, page*config.AuditPageSize)
	return readFailureDetections(whereClause, limit, args)
}

// ReadFailureDetection
func ReadFailureDetection(detectionId int64) ([]*TopologyRecovery, error) {
	whereClause := `where detection_id = ?`
	return readFailureDetections(whereClause, ``, []interface{}{detectionId})
}

// ReadBlockedRecoveries reads blocked recovery entries, potentially filtered by cluster name (empty to unfilter)
func ReadBlockedRecoveries(clusterName string) ([]BlockedTopologyRecovery, error) {
	res := []BlockedTopologyRecovery{}
	whereClause := ""
	args := []interface{}{}
	if clusterName != "" {
		whereClause = `where cluster_name = ?`
		args = append(args, clusterName)
	}
	query := fmt.Sprintf(`
		select
				hostname,
				port,
				cluster_name,
				analysis,
				last_blocked_timestamp,
				blocking_recovery_id
			from
				blocked_topology_recovery
			%s
			order by
				last_blocked_timestamp desc
		`, whereClause)
	rows, err := db.QueryOrchestratorRows[blockedRecoveryRow](context.Background(), query, args...)
	for _, row := range rows {
		res = append(res, blockedRecoveryFromRow(row))
	}

	return res, log.Errore(err)
}

// writeTopologyRecoveryStep writes down a single step in a recovery process
func writeTopologyRecoveryStep(topologyRecoveryStep *TopologyRecoveryStep) error {
	sqlResult, err := db.ExecOrchestratorSQLContext(context.Background(), `
			insert ignore
				into topology_recovery_steps (
					recovery_step_id, recovery_uid, audit_at, message
				) values (?, ?, now(), ?)
			`, db.NilIfZero(topologyRecoveryStep.Id), topologyRecoveryStep.RecoveryUID, topologyRecoveryStep.Message,
	)
	if err != nil {
		return log.Errore(err)
	}
	topologyRecoveryStep.Id, err = sqlResult.LastInsertId()
	return log.Errore(err)
}

// ReadTopologyRecoverySteps reads recovery steps for a given recovery
func ReadTopologyRecoverySteps(recoveryUID string) ([]TopologyRecoveryStep, error) {
	res := []TopologyRecoveryStep{}
	query := `
		select
			recovery_step_id, recovery_uid, audit_at, message
		from
			topology_recovery_steps
		where
			recovery_uid=?
		order by
			recovery_step_id asc
		`
	rows, err := db.QueryOrchestratorRows[topologyRecoveryStepRow](context.Background(), query, recoveryUID)
	for _, row := range rows {
		res = append(res, topologyRecoveryStepFromRow(row))
	}
	return res, log.Errore(err)
}

// ExpireFailureDetectionHistory removes old rows from the topology_failure_detection table
func ExpireFailureDetectionHistory() error {
	return inst.ExpireTableData("topology_failure_detection", "start_active_period")
}

// ExpireTopologyRecoveryHistory removes old rows from the topology_failure_detection table
func ExpireTopologyRecoveryHistory() error {
	return inst.ExpireTableData("topology_recovery", "start_active_period")
}

// ExpireTopologyRecoveryStepsHistory removes old rows from the topology_failure_detection table
func ExpireTopologyRecoveryStepsHistory() error {
	return inst.ExpireTableData("topology_recovery_steps", "audit_at")
}
