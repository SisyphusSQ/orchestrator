package database

import (
	"context"
	"database/sql"

	repositoryschema "github.com/openark/orchestrator/internal/repository/schema"
)

func initOrchestratorDBContext(ctx context.Context, database *sql.DB) error {
	return repositoryschema.Initialize(ctx, database)
}
