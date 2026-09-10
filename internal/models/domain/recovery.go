package domain

import "database/sql"

// TopologyRecoveryRecord is the repository-neutral recovery history record.
type TopologyRecoveryRecord struct {
	RecoveryID             int64
	UID                    string
	Hostname               string
	Port                   int
	Active                 bool
	StartActivePeriod      string
	EndRecovery            string
	Successful             bool
	ProcessingNodeHostname string
	ProcessingNodeToken    string
	PolicyRevision         int64
	HookAssignmentRevision int64
	SuccessorHostname      string
	SuccessorPort          int
	SuccessorAlias         string
	Analysis               string
	ClusterName            string
	ClusterAlias           string
	CountAffectedReplicas  uint
	ReplicaHosts           string
	ParticipatingInstances sql.NullString
	LostReplicas           sql.NullString
	AllErrors              sql.NullString
	Acknowledged           bool
	AcknowledgedAt         sql.NullString
	AcknowledgedBy         sql.NullString
	AcknowledgedComment    sql.NullString
	LastDetectionID        int64
}

type FailureDetectionRecord struct {
	DetectionID            int64
	Hostname               string
	Port                   int
	Active                 bool
	StartActivePeriod      string
	ProcessingNodeHostname string
	ProcessingNodeToken    string
	Analysis               string
	ClusterName            string
	ClusterAlias           string
	CountAffectedReplicas  uint
	ReplicaHosts           string
	RelatedRecoveryID      sql.NullInt64
}

type BlockedRecoveryRecord struct {
	Hostname           string
	Port               int
	ClusterName        string
	Analysis           string
	LastBlockedAt      string
	BlockingRecoveryID sql.NullInt64
}

type TopologyRecoveryStepRecord struct {
	ID          int64
	RecoveryUID string
	AuditAt     string
	Message     string
}
