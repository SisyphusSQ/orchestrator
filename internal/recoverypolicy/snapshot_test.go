package recoverypolicy

import (
	"context"
	"testing"

	"github.com/openark/orchestrator/internal/models/domain"
	"github.com/openark/orchestrator/internal/repository/metadata"
)

func TestExecutionSnapshotPinsPolicyHooksAndFingerprints(t *testing.T) {
	lag := 7
	inputs := metadata.RecoveryInputs{Alias: "payments",
		Policies:    []domain.RecoveryPolicyRecord{{ScopeType: domain.ScopeCluster, ScopeKey: "payments", Overrides: domain.RecoveryPolicyPatch{ReasonableReplicationLagSeconds: &lag}}},
		Profiles:    []domain.RecoveryHookProfile{{ID: "fence", Name: "fence", Enabled: true, Revision: 3, Commands: []string{"old command"}, TimeoutSeconds: 10, FailurePolicy: "abort", OutputLimitBytes: 1024}},
		GlobalHooks: []domain.RecoveryHookAssignment{{Phase: "pre_failover", Mode: "replace", ProfileIDs: []string{"fence"}}},
	}
	first, err := buildSnapshot("cluster", inputs)
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithSnapshot(context.Background(), first)
	lag = 19
	inputs.Profiles[0].Commands[0] = "new command"
	inputs.Profiles[0].Revision = 4
	second, err := buildSnapshot("cluster", inputs)
	if err != nil {
		t.Fatal(err)
	}
	if FromContext(ctx, "cluster").ReasonableReplicationLagSeconds != 7 {
		t.Fatal("running operation adopted updated policy")
	}
	if SnapshotFromContext(ctx).Hooks["pre_failover"][0].Commands[0] != "old command" {
		t.Fatal("running operation adopted updated hook")
	}
	if first.PolicyRevision == second.PolicyRevision || first.HookRevision == second.HookRevision {
		t.Fatal("fingerprints did not reflect changed inputs")
	}
	repeated, err := buildSnapshot("cluster", inputs)
	if err != nil {
		t.Fatal(err)
	}
	if repeated.PolicyRevision != second.PolicyRevision || repeated.HookRevision != second.HookRevision {
		t.Fatal("fingerprints are not deterministic")
	}
}
func TestExecutionSnapshotRejectsMissingHook(t *testing.T) {
	_, err := buildSnapshot("cluster", metadata.RecoveryInputs{GlobalHooks: []domain.RecoveryHookAssignment{{Phase: "pre_failover", Mode: "replace", ProfileIDs: []string{"missing"}}}})
	if err == nil {
		t.Fatal("missing configured hook silently ignored")
	}
}
func TestExecutionSnapshotResolvesClusterDisable(t *testing.T) {
	snapshot, err := buildSnapshot("cluster", metadata.RecoveryInputs{Profiles: []domain.RecoveryHookProfile{{ID: "hook", Enabled: true}}, GlobalHooks: []domain.RecoveryHookAssignment{{Phase: "pre_failover", Mode: "replace", ProfileIDs: []string{"hook"}}}, ClusterHooks: []domain.RecoveryHookAssignment{{Phase: "pre_failover", Mode: "disable"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Hooks["pre_failover"]) != 0 {
		t.Fatal("cluster disable did not override global hook")
	}
}
