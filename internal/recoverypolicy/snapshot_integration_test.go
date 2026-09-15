package recoverypolicy

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/models/domain"
	"github.com/openark/orchestrator/internal/models/dto"
	"github.com/openark/orchestrator/internal/repository"
	"github.com/openark/orchestrator/internal/repository/metadata"
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
	t.Run("explicit alias immediately updates current projection", func(t *testing.T) {
		for _, alias := range []string{"orders-a", "orders-b"} {
			if err := SetExplicitAlias(ctx, "orders:3306", alias); err != nil {
				t.Fatal(err)
			}
			current, err := metadata.ReadClusterAlias(ctx, "orders:3306")
			if err != nil {
				t.Fatal(err)
			}
			if len(current) != 1 || current[0].Alias != alias {
				t.Fatalf("current alias projection=%v, want %q", current, alias)
			}
		}
		if err := SetExplicitAlias(ctx, "payments:3306", "orders-b"); err == nil {
			t.Fatal("duplicate explicit alias was accepted")
		}
		current, err := metadata.ReadClusterAlias(ctx, "orders:3306")
		if err != nil || len(current) != 1 || current[0].Alias != "orders-b" {
			t.Fatalf("duplicate attempt changed current alias: %v, %v", current, err)
		}
	})
}
