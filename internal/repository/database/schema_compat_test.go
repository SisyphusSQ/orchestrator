package database

import (
	"context"
	"database/sql"

	repositoryschema "github.com/openark/orchestrator/internal/repository/schema"
)

const (
	metadataSchemaBootstrap = repositoryschema.LayoutBootstrap
	metadataSchemaLegacy    = repositoryschema.LayoutLegacy
	metadataSchemaCanonical = repositoryschema.LayoutCanonical
	metadataSchemaUpgrade   = repositoryschema.LayoutUpgrade
)

var generateSQLBase = repositoryschema.LegacyBaseStatements()
var generateSQLPatches = repositoryschema.LegacyPatchStatements()

func IsCreateTable(statement string) bool { return repositoryschema.IsCreateTable(statement) }
func IsCreateIndex(statement string) bool { return repositoryschema.IsCreateIndex(statement) }
func IsDropIndex(statement string) bool   { return repositoryschema.IsDropIndex(statement) }
func IsAlterTable(statement string) bool  { return repositoryschema.IsAlterTable(statement) }
func IsInsert(statement string) bool      { return repositoryschema.IsInsert(statement) }
func ToSQLiteDialect(statement string) string {
	return repositoryschema.ToSQLiteDialect(statement)
}

func detectMetadataSchemaLayoutContext(ctx context.Context, database *sql.DB) (repositoryschema.Layout, error) {
	return repositoryschema.DetectLayout(ctx, database)
}

func deployStatements(database *sql.DB, queries []string) error {
	return repositoryschema.DeployStatements(context.Background(), database, queries)
}

func deployStatementsContext(ctx context.Context, database *sql.DB, queries []string) error {
	return repositoryschema.DeployStatements(ctx, database, queries)
}

func deployCanonicalStatementsContext(ctx context.Context, database *sql.DB, queries []string) error {
	return repositoryschema.DeployCanonicalStatements(ctx, database, queries)
}

func registerOrchestratorDeploymentContext(ctx context.Context, database *sql.DB) error {
	return repositoryschema.RegisterDeployment(ctx, database)
}

func validateMetadataIDs(ctx context.Context, database *sql.DB) error {
	return repositoryschema.ValidateMetadataIDs(ctx, database)
}

func migrateMetadataIDs(ctx context.Context, database *sql.DB) error {
	return repositoryschema.MigrateMetadataIDs(ctx, database)
}

func prepareMetadataIDMigration(ctx context.Context, database *sql.DB, layout repositoryschema.Layout) error {
	return repositoryschema.PrepareMetadataIDMigration(ctx, database, layout)
}
