package recoverypolicy

import (
	"reflect"
	"strings"
	"testing"
)

func TestDefaults(t *testing.T) {
	want := Policy{
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
	if got := Defaults(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Defaults() = %#v, want %#v", got, want)
	}
}

func TestApplyOnlyOverridesPresentFields(t *testing.T) {
	base := Defaults()
	enabled := true
	zero := 0
	filters := []string{"db-test-*"}
	policy := Apply(base, PolicyPatch{
		AutoMasterRecovery:           &enabled,
		RecoveryPeriodBlockSeconds:   &zero,
		ProblemIgnoreHostnameFilters: &filters,
	})
	if !policy.AutoMasterRecovery || policy.RecoveryPeriodBlockSeconds != 0 {
		t.Fatalf("explicit overrides not applied: %#v", policy)
	}
	if !reflect.DeepEqual(policy.ProblemIgnoreHostnameFilters, filters) {
		t.Fatalf("filter override = %v", policy.ProblemIgnoreHostnameFilters)
	}
	if policy.FailureDetectionPeriodBlockMinutes != base.FailureDetectionPeriodBlockMinutes {
		t.Fatal("an absent override changed an inherited field")
	}
	filters[0] = "changed"
	if policy.ProblemIgnoreHostnameFilters[0] != "db-test-*" {
		t.Fatal("Apply retained the caller's mutable slice")
	}
}

func TestValidatePolicy(t *testing.T) {
	for _, mode := range []string{"allow", "wait", "reject"} {
		policy := Defaults()
		policy.SQLThreadPromotionPolicy = mode
		if err := ValidatePolicy(policy); err != nil {
			t.Fatalf("valid mode %q rejected: %v", mode, err)
		}
	}
	policy := Defaults()
	policy.SQLThreadPromotionPolicy = "skip"
	if err := ValidatePolicy(policy); err == nil {
		t.Fatal("invalid SQL thread promotion policy accepted")
	}
	policy = Defaults()
	policy.RecoveryPeriodBlockSeconds = -1
	if err := ValidatePolicy(policy); err == nil {
		t.Fatal("negative duration accepted")
	}
}

func TestValidateHookAssignment(t *testing.T) {
	for _, mode := range []string{"inherit", "disable"} {
		assignment := HookAssignment{ScopeType: ScopeGlobal, ScopeKey: GlobalKey, Phase: HookPhases[0], Mode: mode}
		if err := ValidateHookAssignment(assignment); err != nil {
			t.Fatalf("valid %s assignment rejected: %v", mode, err)
		}
	}
	replace := HookAssignment{ScopeType: ScopeCluster, ScopeKey: "orders", Phase: HookPhases[1], Mode: "replace", ProfileIDs: []string{"notify"}}
	if err := ValidateHookAssignment(replace); err != nil {
		t.Fatalf("valid replace assignment rejected: %v", err)
	}
	replace.ProfileIDs = nil
	if err := ValidateHookAssignment(replace); err == nil {
		t.Fatal("replace assignment without profiles accepted")
	}
}

func TestValidateHookProfileRejectsEmptyCommands(t *testing.T) {
	profile := HookProfile{ID: "notify", Name: "Notify", TimeoutSeconds: 30, FailurePolicy: "abort", OutputLimitBytes: 1024, Enabled: true}
	if err := ValidateHookProfile(profile); err == nil {
		t.Fatal("hook profile without commands accepted")
	}
}

func TestRedactOutput(t *testing.T) {
	input := "password=hello token: abc api-key=xyz safe=value"
	output := RedactOutput(input)
	for _, secret := range []string{"hello", "abc", "xyz"} {
		if strings.Contains(output, secret) {
			t.Fatalf("secret %q remained in %q", secret, output)
		}
	}
	if !strings.Contains(output, "safe=value") || strings.Count(output, "[REDACTED]") != 3 {
		t.Fatalf("unexpected redaction output %q", output)
	}
}
