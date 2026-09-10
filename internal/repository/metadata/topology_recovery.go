package metadata

import (
	"context"
	"fmt"
	"strings"

	modeldo "github.com/openark/orchestrator/internal/models/do"
	modeldomain "github.com/openark/orchestrator/internal/models/domain"
	"github.com/openark/orchestrator/internal/repository/database"
)

// FailureDetectionRegistration is the persistence input for a newly observed
// topology failure. It intentionally contains no recovery decision logic.
type FailureDetectionRegistration struct {
	Hostname           string
	Port               int
	ProcessingHostname string
	ProcessingToken    string
	Analysis           string
	ClusterName        string
	ClusterAlias       string
	AffectedReplicas   uint
	ReplicaHosts       string
	Actionable         bool
	StartActivePeriod  string
}

// RegisterFailureDetection records a failure once for its active period.
func RegisterFailureDetection(ctx context.Context, input FailureDetectionRegistration) (bool, error) {
	args := []any{
		input.Hostname,
		input.Port,
		input.ProcessingHostname,
		input.ProcessingToken,
		input.Analysis,
		input.ClusterName,
		input.ClusterAlias,
		input.AffectedReplicas,
		input.ReplicaHosts,
		input.Actionable,
	}
	startActivePeriod := "now()"
	if input.StartActivePeriod != "" {
		startActivePeriod = "?"
		args = append(args, input.StartActivePeriod)
	}
	result, err := database.ExecOrchestratorContext(ctx, fmt.Sprintf(`
		insert ignore into topology_failure_detection (
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
		) values (?, ?, 1, 0, ?, ?, ?, ?, ?, ?, ?, ?, %s)
	`, startActivePeriod), args...)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows > 0, err
}

func ClearActiveFailureDetections(ctx context.Context, blockMinutes int) error {
	_, err := database.ExecOrchestratorContext(ctx, `
		update topology_failure_detection set
			in_active_period = 0,
			end_active_period_unixtime = UNIX_TIMESTAMP()
		where
			in_active_period = 1
			and start_active_period < NOW() - INTERVAL ? MINUTE
	`, blockMinutes)
	return err
}

func clearFailureDetections(ctx context.Context, condition string, args ...any) error {
	_, err := database.ExecOrchestratorContext(ctx, fmt.Sprintf(`
		update topology_failure_detection set
			in_active_period = 0,
			end_active_period_unixtime = UNIX_TIMESTAMP()
		where
			in_active_period = 1
			and %s
	`, condition), args...)
	return err
}

func ClearFailureDetectionsByInstance(ctx context.Context, hostname string, port int) error {
	return clearFailureDetections(ctx, "hostname = ? and port = ?", hostname, port)
}

func ClearFailureDetectionsByClusterName(ctx context.Context, clusterName string) error {
	return clearFailureDetections(ctx, "cluster_name = ?", clusterName)
}

func ClearFailureDetectionsByClusterAlias(ctx context.Context, clusterAlias string) error {
	return clearFailureDetections(ctx, "cluster_alias = ? and cluster_alias != ''", clusterAlias)
}

// TopologyRecoveryRegistration is the persistence input for one recovery.
type TopologyRecoveryRegistration struct {
	ID                     int64
	UID                    string
	Hostname               string
	Port                   int
	ProcessingHostname     string
	ProcessingToken        string
	PolicyRevision         int64
	HookAssignmentRevision int64
	Analysis               string
	ClusterName            string
	ClusterAlias           string
	AffectedReplicas       uint
	ReplicaHosts           string
}

// RegisterTopologyRecovery inserts a recovery and reports whether it won the
// active-period uniqueness race. The returned id is meaningful when inserted.
func RegisterTopologyRecovery(ctx context.Context, input TopologyRecoveryRegistration) (id int64, inserted bool, err error) {
	result, err := database.ExecOrchestratorSQLContext(ctx, `
		insert ignore into topology_recovery (
			id,
			uid,
			hostname,
			port,
			in_active_period,
			start_active_period,
			end_active_period_unixtime,
			processing_node_hostname,
			processcing_node_token,
			policy_revision,
			hook_assignment_revision,
			analysis,
			cluster_name,
			cluster_alias,
			count_affected_slaves,
			slave_hosts,
			last_detection_id
		) values (
			?, ?, ?, ?, 1, NOW(), 0, ?, ?, ?, ?, ?, ?, ?, ?, ?,
			(select ifnull(max(id), 0) from topology_failure_detection where hostname = ? and port = ?)
		)
	`, nilIfZero(input.ID), input.UID, input.Hostname, input.Port,
		input.ProcessingHostname, input.ProcessingToken,
		input.PolicyRevision, input.HookAssignmentRevision,
		input.Analysis, input.ClusterName, input.ClusterAlias,
		input.AffectedReplicas, input.ReplicaHosts, input.Hostname, input.Port)
	if err != nil {
		return 0, false, err
	}
	rows, err := result.RowsAffected()
	if err != nil || rows == 0 {
		return 0, false, err
	}
	id, err = result.LastInsertId()
	return id, true, err
}

func ClearActiveRecoveries(ctx context.Context, blockSeconds int) error {
	_, err := database.ExecOrchestratorContext(ctx, `
		update topology_recovery set
			in_active_period = 0,
			end_active_period_unixtime = UNIX_TIMESTAMP()
		where
			in_active_period = 1
			and start_active_period < NOW() - INTERVAL ? SECOND
	`, blockSeconds)
	return err
}

type BlockedRecoveryRegistration struct {
	Hostname           string
	Port               int
	ClusterName        string
	Analysis           string
	BlockingRecoveryID int64
}

func UpsertBlockedRecovery(ctx context.Context, input BlockedRecoveryRegistration) error {
	_, err := database.ExecOrchestratorContext(ctx, `
		insert into blocked_topology_recovery (
			hostname, port, cluster_name, analysis, last_blocked_timestamp, blocking_recovery_id
		) values (?, ?, ?, ?, NOW(), ?)
		on duplicate key update
			cluster_name = values(cluster_name),
			analysis = values(analysis),
			last_blocked_timestamp = values(last_blocked_timestamp),
			blocking_recovery_id = values(blocking_recovery_id)
	`, input.Hostname, input.Port, input.ClusterName, input.Analysis, input.BlockingRecoveryID)
	return err
}

// ExpireBlockedRecoveries removes audit rows whose blocking recovery was
// acknowledged and rows which have not been refreshed recently.
func ExpireBlockedRecoveries(ctx context.Context, staleSeconds uint) error {
	rows, err := database.QueryOrchestratorRows[modeldo.InstanceKey](ctx, `
		select
			blocked_topology_recovery.hostname,
			blocked_topology_recovery.port
		from blocked_topology_recovery
		left join topology_recovery on (
			blocking_recovery_id = topology_recovery.id and acknowledged = 0
		)
		where acknowledged is null
	`)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if _, err := database.ExecOrchestratorContext(ctx, `
			delete from blocked_topology_recovery where hostname = ? and port = ?
		`, row.Hostname, row.Port); err != nil {
			return err
		}
	}
	_, err = database.ExecOrchestratorContext(ctx, `
		delete from blocked_topology_recovery
		where last_blocked_timestamp < NOW() - interval ? second
	`, staleSeconds)
	return err
}

type RecoveryAcknowledgementKind uint8

const (
	AcknowledgeAll RecoveryAcknowledgementKind = iota
	AcknowledgeByID
	AcknowledgeByUID
	AcknowledgeByClusterName
	AcknowledgeByClusterAlias
	AcknowledgeByInstance
	AcknowledgeCompletedByInstance
	AcknowledgeCrashed
)

type RecoveryAcknowledgementFilter struct {
	Kind         RecoveryAcknowledgementKind
	ID           int64
	UID          string
	ClusterName  string
	ClusterAlias string
	Hostname     string
	Port         int
}

func recoveryAcknowledgementCondition(filter RecoveryAcknowledgementFilter) (condition string, args []any, markEndRecovery bool, err error) {
	switch filter.Kind {
	case AcknowledgeAll:
		return "1 = 1", nil, false, nil
	case AcknowledgeByID:
		return "id = ?", []any{filter.ID}, false, nil
	case AcknowledgeByUID:
		return "uid = ?", []any{filter.UID}, false, nil
	case AcknowledgeByClusterName:
		return "cluster_name = ?", []any{filter.ClusterName}, false, nil
	case AcknowledgeByClusterAlias:
		return "cluster_alias = ? and cluster_alias != ''", []any{filter.ClusterAlias}, false, nil
	case AcknowledgeByInstance:
		return "hostname = ? and port = ?", []any{filter.Hostname, filter.Port}, false, nil
	case AcknowledgeCompletedByInstance:
		return "hostname = ? and port = ? and end_recovery is not null", []any{filter.Hostname, filter.Port}, false, nil
	case AcknowledgeCrashed:
		return `
			in_active_period = 1
			and end_recovery is null
			and concat(processing_node_hostname, ':', processcing_node_token) not in (
				select concat(hostname, ':', token) from node_health
			)
		`, nil, true, nil
	default:
		return "", nil, false, fmt.Errorf("unsupported recovery acknowledgement kind: %d", filter.Kind)
	}
}

func AcknowledgeRecoveries(ctx context.Context, owner, comment string, filter RecoveryAcknowledgementFilter) (int64, error) {
	condition, args, markEndRecovery, err := recoveryAcknowledgementCondition(filter)
	if err != nil {
		return 0, err
	}
	endRecovery := ""
	if markEndRecovery {
		endRecovery = "end_recovery = IFNULL(end_recovery, NOW()),"
	}
	args = append([]any{owner, comment}, args...)
	result, err := database.ExecOrchestratorContext(ctx, fmt.Sprintf(`
		update topology_recovery set
			in_active_period = 0,
			end_active_period_unixtime = case
				when end_active_period_unixtime = 0 then UNIX_TIMESTAMP()
				else end_active_period_unixtime
			end,
			%s
			acknowledged = 1,
			acknowledged_at = NOW(),
			acknowledged_by = ?,
			acknowledge_comment = ?
		where acknowledged = 0 and %s
	`, endRecovery, condition), args...)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

type ResolveTopologyRecoveryInput struct {
	Successful             bool
	SuccessorHostname      string
	SuccessorPort          int
	SuccessorAlias         string
	LostReplicas           string
	ParticipatingInstances string
	AllErrors              string
	UID                    string
}

func ResolveTopologyRecovery(ctx context.Context, input ResolveTopologyRecoveryInput) error {
	_, err := database.ExecOrchestratorContext(ctx, `
		update topology_recovery set
			is_successful = ?,
			successor_hostname = ?,
			successor_port = ?,
			successor_alias = ?,
			lost_slaves = ?,
			participating_instances = ?,
			all_errors = ?,
			end_recovery = NOW()
		where uid = ?
	`, input.Successful, input.SuccessorHostname, input.SuccessorPort,
		input.SuccessorAlias, input.LostReplicas, input.ParticipatingInstances,
		input.AllErrors, input.UID)
	return err
}

type RecoveryReadKind uint8

const (
	ReadActiveRecoveries RecoveryReadKind = iota
	ReadActiveClusterRecovery
	ReadInActivePeriodClusterRecovery
	ReadRecentlyActiveClusterRecovery
	ReadInActivePeriodSuccessorRecovery
	ReadRecentlyActiveSuccessorRecovery
	ReadCompletedRecoveries
	ReadRecoveryByID
	ReadRecoveryByUID
	ReadRecentRecoveries
)

type RecoveryReadFilter struct {
	Kind               RecoveryReadKind
	ClusterName        string
	ClusterAlias       string
	Hostname           string
	Port               int
	ID                 int64
	UID                string
	UnacknowledgedOnly bool
	Limit              int
	Offset             int
}

func recoveryReadCondition(filter RecoveryReadFilter) (where string, limit string, args []any, err error) {
	switch filter.Kind {
	case ReadActiveRecoveries:
		where = "where in_active_period = 1 and end_recovery is null"
	case ReadActiveClusterRecovery:
		where, args = "where in_active_period = 1 and end_recovery is null and cluster_name = ?", []any{filter.ClusterName}
	case ReadInActivePeriodClusterRecovery:
		where, args = "where in_active_period = 1 and cluster_name = ?", []any{filter.ClusterName}
	case ReadRecentlyActiveClusterRecovery:
		where, args = "where end_recovery > now() - interval 5 minute and cluster_name = ?", []any{filter.ClusterName}
	case ReadInActivePeriodSuccessorRecovery:
		where, args = "where in_active_period = 1 and successor_hostname = ? and successor_port = ?", []any{filter.Hostname, filter.Port}
	case ReadRecentlyActiveSuccessorRecovery:
		where, args = "where end_recovery > now() - interval 5 minute and successor_hostname = ? and successor_port = ?", []any{filter.Hostname, filter.Port}
	case ReadCompletedRecoveries:
		where = "where end_recovery is not null"
		limit, args = "limit ? offset ?", []any{filter.Limit, filter.Offset}
	case ReadRecoveryByID:
		where, args = "where id = ?", []any{filter.ID}
	case ReadRecoveryByUID:
		where, args = "where uid = ?", []any{filter.UID}
	case ReadRecentRecoveries:
		conditions := []string{}
		if filter.UnacknowledgedOnly {
			conditions = append(conditions, "acknowledged = 0")
		}
		if filter.ClusterName != "" {
			conditions = append(conditions, "cluster_name = ?")
			args = append(args, filter.ClusterName)
		} else if filter.ClusterAlias != "" {
			conditions = append(conditions, "cluster_alias = ?")
			args = append(args, filter.ClusterAlias)
		}
		if len(conditions) > 0 {
			where = "where " + strings.Join(conditions, " and ")
		}
		limit = "limit ? offset ?"
		args = append(args, filter.Limit, filter.Offset)
	default:
		err = fmt.Errorf("unsupported recovery read kind: %d", filter.Kind)
	}
	return where, limit, args, err
}

func ReadTopologyRecoveries(ctx context.Context, filter RecoveryReadFilter) ([]modeldomain.TopologyRecoveryRecord, error) {
	where, limit, args, err := recoveryReadCondition(filter)
	if err != nil {
		return nil, err
	}
	rows, err := database.QueryOrchestratorRows[modeldo.TopologyRecovery](ctx, fmt.Sprintf(`
		select
			id,
			uid,
			hostname,
			port,
			(IFNULL(end_active_period_unixtime, 0) = 0) as is_active,
			start_active_period,
			IFNULL(end_active_period_unixtime, 0) as end_active_period_unixtime,
			IFNULL(end_recovery, '') as end_recovery,
			is_successful,
			processing_node_hostname,
			processcing_node_token,
			policy_revision,
			hook_assignment_revision,
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
		from topology_recovery
		%s
		order by id desc
		%s
	`, where, limit), args...)
	result := make([]modeldomain.TopologyRecoveryRecord, 0, len(rows))
	for _, row := range rows {
		result = append(result, modeldomain.TopologyRecoveryRecord(row))
	}
	return result, err
}

type FailureDetectionReadFilter struct {
	ClusterAlias string
	ID           int64
	Limit        int
	Offset       int
}

func ReadFailureDetections(ctx context.Context, filter FailureDetectionReadFilter) ([]modeldomain.FailureDetectionRecord, error) {
	where := ""
	args := []any{}
	if filter.ID != 0 {
		where, args = "where id = ?", append(args, filter.ID)
	} else if filter.ClusterAlias != "" {
		where, args = "where cluster_alias = ?", append(args, filter.ClusterAlias)
	}
	limit := ""
	if filter.Limit > 0 {
		limit = "limit ? offset ?"
		args = append(args, filter.Limit, filter.Offset)
	}
	rows, err := database.QueryOrchestratorRows[modeldo.FailureDetection](ctx, fmt.Sprintf(`
		select
			id,
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
			(select max(id) from topology_recovery where topology_recovery.last_detection_id = topology_failure_detection.id) as related_recovery_id
		from topology_failure_detection
		%s
		order by id desc
		%s
	`, where, limit), args...)
	result := make([]modeldomain.FailureDetectionRecord, 0, len(rows))
	for _, row := range rows {
		result = append(result, modeldomain.FailureDetectionRecord(row))
	}
	return result, err
}

func ReadBlockedRecoveryRows(ctx context.Context, clusterName string) ([]modeldomain.BlockedRecoveryRecord, error) {
	where := ""
	args := []any{}
	if clusterName != "" {
		where, args = "where cluster_name = ?", append(args, clusterName)
	}
	rows, err := database.QueryOrchestratorRows[modeldo.BlockedRecovery](ctx, fmt.Sprintf(`
		select hostname, port, cluster_name, analysis, last_blocked_timestamp, blocking_recovery_id
		from blocked_topology_recovery
		%s
		order by last_blocked_timestamp desc
	`, where), args...)
	result := make([]modeldomain.BlockedRecoveryRecord, 0, len(rows))
	for _, row := range rows {
		result = append(result, modeldomain.BlockedRecoveryRecord(row))
	}
	return result, err
}

func WriteTopologyRecoveryStep(ctx context.Context, step modeldomain.TopologyRecoveryStepRecord) (int64, error) {
	result, err := database.ExecOrchestratorSQLContext(ctx, `
		insert ignore into topology_recovery_steps (id, recovery_uid, audit_at, message)
		values (?, ?, now(), ?)
	`, nilIfZero(step.ID), step.RecoveryUID, step.Message)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func ReadTopologyRecoveryStepRows(ctx context.Context, recoveryUID string) ([]modeldomain.TopologyRecoveryStepRecord, error) {
	rows, err := database.QueryOrchestratorRows[modeldo.TopologyRecoveryStep](ctx, `
		select id, recovery_uid, audit_at, message
		from topology_recovery_steps
		where recovery_uid = ?
		order by id asc
	`, recoveryUID)
	result := make([]modeldomain.TopologyRecoveryStepRecord, 0, len(rows))
	for _, row := range rows {
		result = append(result, modeldomain.TopologyRecoveryStepRecord(row))
	}
	return result, err
}

func ExpireFailureDetectionHistory(ctx context.Context, purgeDays uint) error {
	return expireRecoveryTable(ctx, "topology_failure_detection", "start_active_period", purgeDays)
}

func ExpireTopologyRecoveryHistory(ctx context.Context, purgeDays uint) error {
	return expireRecoveryTable(ctx, "topology_recovery", "start_active_period", purgeDays)
}

func ExpireTopologyRecoveryStepsHistory(ctx context.Context, purgeDays uint) error {
	return expireRecoveryTable(ctx, "topology_recovery_steps", "audit_at", purgeDays)
}

func expireRecoveryTable(ctx context.Context, tableName, timestampColumn string, purgeDays uint) error {
	_, err := database.ExecOrchestratorContext(ctx, fmt.Sprintf(`
		delete from %s where %s < NOW() - INTERVAL ? DAY
	`, tableName, timestampColumn), purgeDays)
	return err
}

func nilIfZero(value int64) any {
	if value == 0 {
		return nil
	}
	return value
}
