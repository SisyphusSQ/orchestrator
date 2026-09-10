package database

import (
	"context"
	"database/sql"

	repositoryschema "github.com/openark/orchestrator/internal/repository/schema"
)

func initOrchestratorDB(database *sql.DB) error {
	return initOrchestratorDBContext(context.Background(), database)
}

func initOrchestratorDBContext(ctx context.Context, database *sql.DB) error {
	return repositoryschema.Initialize(ctx, database)
}
