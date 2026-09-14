package recoverypolicy

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/models/domain"
	"github.com/openark/orchestrator/internal/models/dto"
	"github.com/openark/orchestrator/internal/repository"
)

func TestCaptureUsesPersistedInputsAndKeepsPriorExecution(t *testing.T) {
	previous := config.Current()
	config.TestUpdate(func(c *config.Configuration) {
		c.Metadata.Type = "sqlite3"
		c.Metadata.SQLite.DataFile = filepath.Join(t.TempDir(), "snapshot.sqlite")
		c.Metadata.Schema.SkipUpdate = false
	})
	t.Cleanup(func() {
		_ = repository.Close()
		config.TestUpdate(func(c *config.Configuration) { *c = *previous })
		Invalidate()
	})
	ctx := context.Background()
	if err := repository.InitializeMetadata(ctx); err != nil {
		t.Fatal(err)
	}
	lag := 7
	if err := SavePolicy(ctx, dto.SaveRecoveryPolicyCommand{ScopeType: domain.ScopeGlobal, ScopeKey: domain.GlobalKey, Overrides: domain.RecoveryPolicyPatch{ReasonableReplicationLagSeconds: &lag}, UpdatedBy: "test", ChangeReason: "snapshot test"}); err != nil {
		t.Fatal(err)
	}
	first, err := Capture(ctx, "test-cluster")
	if err != nil {
		t.Fatal(err)
	}
	lag = 29
	if err := SavePolicy(ctx, dto.SaveRecoveryPolicyCommand{ScopeType: domain.ScopeGlobal, ScopeKey: domain.GlobalKey, ExpectedRevision: 1, Overrides: domain.RecoveryPolicyPatch{ReasonableReplicationLagSeconds: &lag}, UpdatedBy: "test", ChangeReason: "next execution"}); err != nil {
		t.Fatal(err)
	}
	second, err := Capture(ctx, "test-cluster")
	if err != nil {
		t.Fatal(err)
	}
	if first.Policy.ReasonableReplicationLagSeconds != 7 || second.Policy.ReasonableReplicationLagSeconds != 29 {
		t.Fatalf("snapshots first=%d second=%d", first.Policy.ReasonableReplicationLagSeconds, second.Policy.ReasonableReplicationLagSeconds)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := Capture(canceled, "test-cluster"); err == nil {
		t.Fatal("canceled capture silently used defaults")
	}
}
