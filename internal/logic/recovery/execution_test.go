package recovery

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/inst/analysis"
	instmodel "github.com/openark/orchestrator/internal/inst/instance"
	"github.com/openark/orchestrator/internal/models/domain"
	"github.com/openark/orchestrator/internal/models/dto"
	"github.com/openark/orchestrator/internal/recoverypolicy"
	"github.com/openark/orchestrator/internal/repository"
)

func TestHookExecutionUsesCapturedContentAndAuditRevision(t *testing.T) {
	directory := t.TempDir()
	previous := config.Current()
	config.TestUpdate(func(c *config.Configuration) {
		c.Metadata.Type = "sqlite3"
		c.Metadata.SQLite.DataFile = filepath.Join(directory, "metadata.sqlite")
		c.Metadata.Schema.SkipUpdate = false
		c.Audit.ToBackend = true
	})
	t.Cleanup(func() {
		_ = repository.Close()
		config.TestUpdate(func(c *config.Configuration) { *c = *previous })
		recoverypolicy.Invalidate()
	})
	ctx := context.Background()
	if err := repository.InitializeMetadata(ctx); err != nil {
		t.Fatal(err)
	}
	profile := domain.RecoveryHookProfile{ID: "fixture", Name: "fixture", Enabled: true, Commands: []string{`printf old >> "$ORC_SUCCESSOR_ALIAS"`}, TimeoutSeconds: 1, OutputLimitBytes: 1024, FailurePolicy: "abort", ChangeReason: "isolated test"}
	if err := recoverypolicy.SaveHookProfile(ctx, dto.SaveRecoveryHookProfileCommand{Profile: profile}); err != nil {
		t.Fatal(err)
	}
	assignment := domain.RecoveryHookAssignment{ScopeType: domain.ScopeGlobal, ScopeKey: domain.GlobalKey, Phase: "pre_failover", Mode: "replace", ProfileIDs: []string{profile.ID}, ChangeReason: "isolated test"}
	if err := recoverypolicy.SaveHookAssignment(ctx, dto.SaveRecoveryHookAssignmentCommand{Assignment: assignment}); err != nil {
		t.Fatal(err)
	}
	first, err := pinRecoveryExecution(ctx, "fixture-cluster")
	if err != nil {
		t.Fatal(err)
	}
	profile.Commands = []string{`printf new >> "$ORC_SUCCESSOR_ALIAS"`}
	if err := recoverypolicy.SaveHookProfile(ctx, dto.SaveRecoveryHookProfileCommand{Profile: profile, ExpectedRevision: 1}); err != nil {
		t.Fatal(err)
	}
	second, err := pinRecoveryExecution(ctx, "fixture-cluster")
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(directory, "hook-output")
	for _, execution := range []context.Context{first, second} {
		recovery := NewTopologyRecovery(analysis.ReplicationAnalysis{})
		recovery.SuccessorKey = &instmodel.InstanceKey{Hostname: "fixture", Port: 3306}
		recovery.SuccessorAlias = output
		if err := executeHookPhaseContext(execution, "pre_failover", "test", recovery); err != nil {
			t.Fatal(err)
		}
		snapshot := recoverypolicy.SnapshotFromContext(execution)
		if recovery.PolicyRevision != snapshot.PolicyRevision || recovery.HookAssignmentRevision != snapshot.HookRevision {
			t.Fatal("audit revision differs from executed snapshot")
		}
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "oldnew" {
		t.Fatalf("running recovery adopted new hook: %q", data)
	}
	if recoverypolicy.SnapshotFromContext(first).HookRevision == recoverypolicy.SnapshotFromContext(second).HookRevision {
		t.Fatal("changed hook did not change fingerprint")
	}
}

func TestRecoveryRegistrationRequiresSnapshotBeforeRepositoryAccess(t *testing.T) {
	if _, err := AttemptRecoveryRegistration(context.Background(), &analysis.ReplicationAnalysis{}, false, false); err == nil {
		t.Fatal("missing execution snapshot accepted")
	}
}
