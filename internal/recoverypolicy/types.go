package recoverypolicy

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/openark/orchestrator/internal/models/domain"
)

var secretOutputPattern = regexp.MustCompile(`(?i)(password|passwd|secret|token|api[_-]?key)(\s*[:=]\s*)[^\s,;]+`)

// RedactOutput removes common credential assignments before hook output enters
// recovery or operation audit logs.
func RedactOutput(output string) string {
	return secretOutputPattern.ReplaceAllString(output, `$1$2[REDACTED]`)
}

func Defaults() domain.RecoveryPolicy {
	return domain.RecoveryPolicy{
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

func Apply(base domain.RecoveryPolicy, patch domain.RecoveryPolicyPatch) domain.RecoveryPolicy {
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
	if scopeType != domain.ScopeGlobal && scopeType != domain.ScopeCluster {
		return fmt.Errorf("invalid scope type %q", scopeType)
	}
	if scopeType == domain.ScopeGlobal && scopeKey != domain.GlobalKey {
		return fmt.Errorf("global scope key must be %q", domain.GlobalKey)
	}
	if scopeType == domain.ScopeCluster && strings.TrimSpace(scopeKey) == "" {
		return fmt.Errorf("cluster scope requires an explicit alias")
	}
	return nil
}

func ValidatePolicy(policy domain.RecoveryPolicy) error {
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

func ValidateHookProfile(profile domain.RecoveryHookProfile) error {
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

func ValidateHookAssignment(assignment domain.RecoveryHookAssignment) error {
	if err := ValidateScope(assignment.ScopeType, assignment.ScopeKey); err != nil {
		return err
	}
	validPhase := slices.Contains(domain.RecoveryHookPhases, assignment.Phase)
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
