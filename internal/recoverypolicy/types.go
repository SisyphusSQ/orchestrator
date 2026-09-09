package recoverypolicy

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	ScopeGlobal  = "global"
	ScopeCluster = "cluster"
	GlobalKey    = "*"
)

// Policy contains the effective recovery behavior used by orchestration code.
// Defaults are deliberately code-owned; persisted rows only contain overrides.
type Policy struct {
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

// PolicyPatch is sparse so a cluster can inherit any individual global value.
type PolicyPatch struct {
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

type PolicyDocument struct {
	ScopeType    string      `json:"scopeType"`
	ScopeKey     string      `json:"scopeKey"`
	Revision     int64       `json:"revision"`
	Overrides    PolicyPatch `json:"overrides"`
	Inherited    Policy      `json:"inherited"`
	Effective    Policy      `json:"effective"`
	UpdatedBy    string      `json:"updatedBy,omitempty"`
	ChangeReason string      `json:"changeReason,omitempty"`
	UpdatedAt    string      `json:"updatedAt,omitempty"`
}

type SavePolicyCommand struct {
	ScopeType        string      `json:"scopeType"`
	ScopeKey         string      `json:"scopeKey"`
	ExpectedRevision int64       `json:"expectedRevision"`
	Overrides        PolicyPatch `json:"overrides"`
	UpdatedBy        string      `json:"updatedBy"`
	ChangeReason     string      `json:"changeReason"`
}

type HookProfile struct {
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

type HookAssignment struct {
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

type SaveHookProfileCommand struct {
	Profile          HookProfile `json:"profile"`
	ExpectedRevision int64       `json:"expectedRevision"`
}

type SaveHookAssignmentCommand struct {
	Assignment       HookAssignment `json:"assignment"`
	ExpectedRevision int64          `json:"expectedRevision"`
}

var HookPhases = []string{
	"failure_detection", "pre_failover", "post_master_failover",
	"post_intermediate_master_failover", "post_failover",
	"post_unsuccessful_failover", "pre_graceful_takeover",
	"post_graceful_takeover", "post_take_master",
}

var secretOutputPattern = regexp.MustCompile(`(?i)(password|passwd|secret|token|api[_-]?key)(\s*[:=]\s*)[^\s,;]+`)

// RedactOutput removes common credential assignments before hook output enters
// recovery or operation audit logs.
func RedactOutput(output string) string {
	return secretOutputPattern.ReplaceAllString(output, `$1$2[REDACTED]`)
}

func Defaults() Policy {
	return Policy{
		RecoveryIgnoreHostnameFilters:              []string{},
		PromotionIgnoreHostnameFilters:             []string{},
		ProblemIgnoreHostnameFilters:               []string{},
		FailureDetectionPeriodBlockMinutes:         60,
		RecoveryPeriodBlockSeconds:                 3600,
		ReasonableReplicationLagSeconds:            10,
		ReasonableMaintenanceReplicationLagSeconds: 20,
		SQLThreadPromotionPolicy:                   "allow",
		CoMasterRecoveryMustPromoteOtherCoMaster:   true,
		DetachLostReplicasAfterMasterFailover:      true,
		ApplyMySQLPromotionAfterMasterFailover:     true,
	}
}

func Apply(base Policy, patch PolicyPatch) Policy {
	setBool := func(value *bool, target *bool) {
		if value != nil {
			*target = *value
		}
	}
	setInt := func(value *int, target *int) {
		if value != nil {
			*target = *value
		}
	}
	setStrings := func(value *[]string, target *[]string) {
		if value != nil {
			*target = append([]string(nil), (*value)...)
		}
	}
	setBool(patch.AutoMasterRecovery, &base.AutoMasterRecovery)
	setBool(patch.AutoIntermediateMasterRecovery, &base.AutoIntermediateMasterRecovery)
	setStrings(patch.RecoveryIgnoreHostnameFilters, &base.RecoveryIgnoreHostnameFilters)
	setStrings(patch.PromotionIgnoreHostnameFilters, &base.PromotionIgnoreHostnameFilters)
	setStrings(patch.ProblemIgnoreHostnameFilters, &base.ProblemIgnoreHostnameFilters)
	setInt(patch.FailureDetectionPeriodBlockMinutes, &base.FailureDetectionPeriodBlockMinutes)
	setInt(patch.RecoveryPeriodBlockSeconds, &base.RecoveryPeriodBlockSeconds)
	setInt(patch.ReasonableReplicationLagSeconds, &base.ReasonableReplicationLagSeconds)
	setInt(patch.ReasonableMaintenanceReplicationLagSeconds, &base.ReasonableMaintenanceReplicationLagSeconds)
	setBool(patch.VerifyReplicationFilters, &base.VerifyReplicationFilters)
	setInt(patch.FailMasterPromotionOnLagMinutes, &base.FailMasterPromotionOnLagMinutes)
	if patch.SQLThreadPromotionPolicy != nil {
		base.SQLThreadPromotionPolicy = *patch.SQLThreadPromotionPolicy
	}
	setBool(patch.RecoverNonWriteableMaster, &base.RecoverNonWriteableMaster)
	setBool(patch.CoMasterRecoveryMustPromoteOtherCoMaster, &base.CoMasterRecoveryMustPromoteOtherCoMaster)
	setBool(patch.DetachLostReplicasAfterMasterFailover, &base.DetachLostReplicasAfterMasterFailover)
	setBool(patch.ApplyMySQLPromotionAfterMasterFailover, &base.ApplyMySQLPromotionAfterMasterFailover)
	setBool(patch.PreventCrossDataCenterMasterFailover, &base.PreventCrossDataCenterMasterFailover)
	setBool(patch.PreventCrossRegionMasterFailover, &base.PreventCrossRegionMasterFailover)
	setBool(patch.MasterFailoverDetachReplicaMasterHost, &base.MasterFailoverDetachReplicaMasterHost)
	setInt(patch.PostponeReplicaRecoveryOnLagMinutes, &base.PostponeReplicaRecoveryOnLagMinutes)
	setBool(patch.EnforceExactSemiSyncReplicas, &base.EnforceExactSemiSyncReplicas)
	setBool(patch.RecoverLockedSemiSyncMaster, &base.RecoverLockedSemiSyncMaster)
	setInt(patch.ReasonableLockedSemiSyncMasterSeconds, &base.ReasonableLockedSemiSyncMasterSeconds)
	return base
}

func ValidateScope(scopeType, scopeKey string) error {
	if scopeType != ScopeGlobal && scopeType != ScopeCluster {
		return fmt.Errorf("invalid scope type %q", scopeType)
	}
	if scopeType == ScopeGlobal && scopeKey != GlobalKey {
		return fmt.Errorf("global scope key must be %q", GlobalKey)
	}
	if scopeType == ScopeCluster && strings.TrimSpace(scopeKey) == "" {
		return fmt.Errorf("cluster scope requires an explicit alias")
	}
	return nil
}

func ValidatePolicy(policy Policy) error {
	if policy.FailureDetectionPeriodBlockMinutes < 0 || policy.RecoveryPeriodBlockSeconds < 0 ||
		policy.ReasonableReplicationLagSeconds < 0 || policy.ReasonableMaintenanceReplicationLagSeconds < 0 ||
		policy.FailMasterPromotionOnLagMinutes < 0 || policy.PostponeReplicaRecoveryOnLagMinutes < 0 ||
		policy.ReasonableLockedSemiSyncMasterSeconds < 0 {
		return fmt.Errorf("time values cannot be negative")
	}
	switch policy.SQLThreadPromotionPolicy {
	case "allow", "wait", "reject":
	default:
		return fmt.Errorf("invalid SQL thread promotion policy %q", policy.SQLThreadPromotionPolicy)
	}
	return nil
}

func ValidateHookProfile(profile HookProfile) error {
	if strings.TrimSpace(profile.ID) == "" || strings.TrimSpace(profile.Name) == "" {
		return fmt.Errorf("hook profile id and name are required")
	}
	if profile.TimeoutSeconds < 1 || profile.TimeoutSeconds > 3600 {
		return fmt.Errorf("hook timeout must be between 1 and 3600 seconds")
	}
	if profile.OutputLimitBytes < 1024 || profile.OutputLimitBytes > 1048576 {
		return fmt.Errorf("hook output limit must be between 1024 and 1048576 bytes")
	}
	if profile.FailurePolicy != "continue" && profile.FailurePolicy != "abort" {
		return fmt.Errorf("invalid hook failure policy %q", profile.FailurePolicy)
	}
	if len(profile.Commands) == 0 {
		return fmt.Errorf("hook profile requires at least one command")
	}
	for _, command := range profile.Commands {
		if strings.TrimSpace(command) == "" {
			return fmt.Errorf("hook commands cannot be blank")
		}
	}
	return nil
}

func ValidateHookAssignment(assignment HookAssignment) error {
	if err := ValidateScope(assignment.ScopeType, assignment.ScopeKey); err != nil {
		return err
	}
	validPhase := false
	for _, phase := range HookPhases {
		if assignment.Phase == phase {
			validPhase = true
			break
		}
	}
	if !validPhase {
		return fmt.Errorf("invalid hook phase %q", assignment.Phase)
	}
	if assignment.Mode != "inherit" && assignment.Mode != "replace" && assignment.Mode != "disable" {
		return fmt.Errorf("invalid hook mode %q", assignment.Mode)
	}
	if assignment.Mode == "replace" && len(assignment.ProfileIDs) == 0 {
		return fmt.Errorf("replace mode requires at least one hook profile")
	}
	if assignment.Mode != "replace" && len(assignment.ProfileIDs) != 0 {
		return fmt.Errorf("only replace mode accepts hook profiles")
	}
	return nil
}
