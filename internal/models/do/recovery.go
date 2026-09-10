package do

import "database/sql"

type TopologyRecovery struct {
	RecoveryID             int64          `gorm:"column:id"`
	UID                    string         `gorm:"column:uid"`
	Hostname               string         `gorm:"column:hostname"`
	Port                   int            `gorm:"column:port"`
	Active                 bool           `gorm:"column:is_active"`
	StartActivePeriod      string         `gorm:"column:start_active_period"`
	EndRecovery            string         `gorm:"column:end_recovery"`
	Successful             bool           `gorm:"column:is_successful"`
	ProcessingNodeHostname string         `gorm:"column:processing_node_hostname"`
	ProcessingNodeToken    string         `gorm:"column:processcing_node_token"`
	PolicyRevision         int64          `gorm:"column:policy_revision"`
	HookAssignmentRevision int64          `gorm:"column:hook_assignment_revision"`
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

type FailureDetection struct {
	DetectionID            int64         `gorm:"column:id"`
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

type BlockedRecovery struct {
	Hostname           string        `gorm:"column:hostname"`
	Port               int           `gorm:"column:port"`
	ClusterName        string        `gorm:"column:cluster_name"`
	Analysis           string        `gorm:"column:analysis"`
	LastBlockedAt      string        `gorm:"column:last_blocked_timestamp"`
	BlockingRecoveryID sql.NullInt64 `gorm:"column:blocking_recovery_id"`
}

type TopologyRecoveryStep struct {
	ID          int64  `gorm:"column:id"`
	RecoveryUID string `gorm:"column:recovery_uid"`
	AuditAt     string `gorm:"column:audit_at"`
	Message     string `gorm:"column:message"`
}
