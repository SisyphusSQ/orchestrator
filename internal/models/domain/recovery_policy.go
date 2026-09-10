package domain

const (
	ScopeGlobal  = "global"
	ScopeCluster = "cluster"
	GlobalKey    = "*"
)

// RecoveryPolicy contains effective recovery behavior used by orchestration code.
type RecoveryPolicy struct {
	AutoMasterRecovery                         bool     `json:"autoMasterRecovery"`
	AutoIntermediateMasterRecovery             bool     `json:"autoIntermediateMasterRecovery"`
	RecoveryIgnoreHostnameFilters              []string `json:"recoveryIgnoreHostnameFilters"`
	PromotionIgnoreHostnameFilters             []string `json:"promotionIgnoreHostnameFilters"`
	ProblemIgnoreHostnameFilters               []string `json:"problemIgnoreHostnameFilters"`
	FailureDetectionPeriodBlockMinutes         int      `json:"failureDetectionPeriodBlockMinutes"`
	RecoveryPeriodBlockSeconds                 int      `json:"recoveryPeriodBlockSeconds"`
	ReasonableReplicationLagSeconds            int      `json:"reasonableReplicationLagSeconds"`
	ReasonableMaintenanceReplicationLagSeconds int      `json:"reasonableMaintenanceReplicationLagSeconds"`
	VerifyReplicationFilters                   bool     `json:"verifyReplicationFilters"`
	FailMasterPromotionOnLagMinutes            int      `json:"failMasterPromotionOnLagMinutes"`
	SQLThreadPromotionPolicy                   string   `json:"sqlThreadPromotionPolicy"`
	RecoverNonWriteableMaster                  bool     `json:"recoverNonWriteableMaster"`
	CoMasterRecoveryMustPromoteOtherCoMaster   bool     `json:"coMasterRecoveryMustPromoteOtherCoMaster"`
	DetachLostReplicasAfterMasterFailover      bool     `json:"detachLostReplicasAfterMasterFailover"`
	ApplyMySQLPromotionAfterMasterFailover     bool     `json:"applyMySQLPromotionAfterMasterFailover"`
	PreventCrossDataCenterMasterFailover       bool     `json:"preventCrossDataCenterMasterFailover"`
	PreventCrossRegionMasterFailover           bool     `json:"preventCrossRegionMasterFailover"`
	MasterFailoverDetachReplicaMasterHost      bool     `json:"masterFailoverDetachReplicaMasterHost"`
	PostponeReplicaRecoveryOnLagMinutes        int      `json:"postponeReplicaRecoveryOnLagMinutes"`
	EnforceExactSemiSyncReplicas               bool     `json:"enforceExactSemiSyncReplicas"`
	RecoverLockedSemiSyncMaster                bool     `json:"recoverLockedSemiSyncMaster"`
	ReasonableLockedSemiSyncMasterSeconds      int      `json:"reasonableLockedSemiSyncMasterSeconds"`
}

// RecoveryPolicyPatch is sparse so cluster policy can inherit global values.
type RecoveryPolicyPatch struct {
	AutoMasterRecovery                         *bool     `json:"autoMasterRecovery,omitempty"`
	AutoIntermediateMasterRecovery             *bool     `json:"autoIntermediateMasterRecovery,omitempty"`
	RecoveryIgnoreHostnameFilters              *[]string `json:"recoveryIgnoreHostnameFilters,omitempty"`
	PromotionIgnoreHostnameFilters             *[]string `json:"promotionIgnoreHostnameFilters,omitempty"`
	ProblemIgnoreHostnameFilters               *[]string `json:"problemIgnoreHostnameFilters,omitempty"`
	FailureDetectionPeriodBlockMinutes         *int      `json:"failureDetectionPeriodBlockMinutes,omitempty"`
	RecoveryPeriodBlockSeconds                 *int      `json:"recoveryPeriodBlockSeconds,omitempty"`
	ReasonableReplicationLagSeconds            *int      `json:"reasonableReplicationLagSeconds,omitempty"`
	ReasonableMaintenanceReplicationLagSeconds *int      `json:"reasonableMaintenanceReplicationLagSeconds,omitempty"`
	VerifyReplicationFilters                   *bool     `json:"verifyReplicationFilters,omitempty"`
	FailMasterPromotionOnLagMinutes            *int      `json:"failMasterPromotionOnLagMinutes,omitempty"`
	SQLThreadPromotionPolicy                   *string   `json:"sqlThreadPromotionPolicy,omitempty"`
	RecoverNonWriteableMaster                  *bool     `json:"recoverNonWriteableMaster,omitempty"`
	CoMasterRecoveryMustPromoteOtherCoMaster   *bool     `json:"coMasterRecoveryMustPromoteOtherCoMaster,omitempty"`
	DetachLostReplicasAfterMasterFailover      *bool     `json:"detachLostReplicasAfterMasterFailover,omitempty"`
	ApplyMySQLPromotionAfterMasterFailover     *bool     `json:"applyMySQLPromotionAfterMasterFailover,omitempty"`
	PreventCrossDataCenterMasterFailover       *bool     `json:"preventCrossDataCenterMasterFailover,omitempty"`
	PreventCrossRegionMasterFailover           *bool     `json:"preventCrossRegionMasterFailover,omitempty"`
	MasterFailoverDetachReplicaMasterHost      *bool     `json:"masterFailoverDetachReplicaMasterHost,omitempty"`
	PostponeReplicaRecoveryOnLagMinutes        *int      `json:"postponeReplicaRecoveryOnLagMinutes,omitempty"`
	EnforceExactSemiSyncReplicas               *bool     `json:"enforceExactSemiSyncReplicas,omitempty"`
	RecoverLockedSemiSyncMaster                *bool     `json:"recoverLockedSemiSyncMaster,omitempty"`
	ReasonableLockedSemiSyncMasterSeconds      *int      `json:"reasonableLockedSemiSyncMasterSeconds,omitempty"`
}

// RecoveryPolicyRecord is the repository-facing form of one persisted policy.
// The repository owns JSON encoding of Overrides.
type RecoveryPolicyRecord struct {
	ScopeType    string
	ScopeKey     string
	Overrides    RecoveryPolicyPatch
	Revision     int64
	UpdatedBy    string
	ChangeReason string
	UpdatedAt    string
}

// RecoveryHookProfile describes one reusable recovery hook profile.
type RecoveryHookProfile struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Commands         []string `json:"commands"`
	TimeoutSeconds   int      `json:"timeoutSeconds"`
	FailurePolicy    string   `json:"failurePolicy"`
	OutputLimitBytes int      `json:"outputLimitBytes"`
	Enabled          bool     `json:"enabled"`
	Revision         int64    `json:"revision"`
	UpdatedBy        string   `json:"updatedBy,omitempty"`
	ChangeReason     string   `json:"changeReason,omitempty"`
	UpdatedAt        string   `json:"updatedAt,omitempty"`
}

// RecoveryHookAssignment binds hook profiles to a recovery phase and scope.
type RecoveryHookAssignment struct {
	ScopeType    string   `json:"scopeType"`
	ScopeKey     string   `json:"scopeKey"`
	Phase        string   `json:"phase"`
	Mode         string   `json:"mode"`
	ProfileIDs   []string `json:"profileIds"`
	Revision     int64    `json:"revision"`
	UpdatedBy    string   `json:"updatedBy,omitempty"`
	ChangeReason string   `json:"changeReason,omitempty"`
	UpdatedAt    string   `json:"updatedAt,omitempty"`
}

var RecoveryHookPhases = []string{
	"failure_detection", "pre_failover", "post_master_failover",
	"post_intermediate_master_failover", "post_failover",
	"post_unsuccessful_failover", "pre_graceful_takeover",
	"post_graceful_takeover", "post_take_master",
}
