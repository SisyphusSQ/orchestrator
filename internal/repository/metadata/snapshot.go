package metadata

import (
	"context"

	modeldomain "github.com/openark/orchestrator/internal/models/domain"
	"github.com/openark/orchestrator/internal/repository/database"
)

// ReadSnapshotTable exports a managed metadata table using the stable snapshot shape.
func ReadSnapshotTable(ctx context.Context, tableName string) (modeldomain.NamedResultData, error) {
	pool, err := database.OpenOrchestratorContext(ctx)
	if err != nil {
		return modeldomain.NamedResultData{}, err
	}
	return database.ScanTableContext(ctx, pool, tableName)
}

// WriteSnapshotTable restores a managed metadata table using the stable snapshot shape.
func WriteSnapshotTable(ctx context.Context, tableName string, data modeldomain.NamedResultData) error {
	pool, err := database.OpenOrchestratorContext(ctx)
	if err != nil {
		return err
	}
	return database.WriteTableContext(ctx, pool, tableName, data)
}
