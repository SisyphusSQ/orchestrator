package recovery

import (
	"context"

	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/recoverypolicy"
)

func pinRecoveryExecution(ctx context.Context, clusterName string) (context.Context, error) {
	ctx = config.WithSnapshot(ctx)
	if recoverypolicy.SnapshotFromContext(ctx) != nil {
		return ctx, nil
	}
	snapshot, err := recoverypolicy.Capture(ctx, clusterName)
	if err != nil {
		return ctx, err
	}
	return recoverypolicy.WithSnapshot(ctx, snapshot), nil
}
