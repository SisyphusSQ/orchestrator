package repository

import (
	"context"

	"github.com/openark/orchestrator/internal/repository/database"
)

// InitializeMetadata opens the process-owned metadata pool and runs the
// configured schema lifecycle before returning.
func InitializeMetadata(ctx context.Context) error {
	_, err := database.OpenOrchestratorContext(ctx)
	return err
}

func PingMetadata(ctx context.Context) error {
	db, err := database.OpenOrchestratorContext(ctx)
	if err != nil {
		return err
	}
	return db.PingContext(ctx)
}

func Close() error {
	return database.Close()
}
