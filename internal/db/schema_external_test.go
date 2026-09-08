package db

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	metadataschema "github.com/openark/orchestrator/docs/schema"
	"github.com/openark/orchestrator/internal/config"
)

func TestCanonicalMetadataSchemaExternalMySQL(t *testing.T) {
	dsn := os.Getenv("ORCHESTRATOR_METADATA_SCHEMA_TEST_DSN")
	if dsn == "" {
		t.Skip("ORCHESTRATOR_METADATA_SCHEMA_TEST_DSN is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	database, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("open external metadata schema database: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close external metadata schema database: %v", err)
		}
	})
	if err := database.PingContext(ctx); err != nil {
		t.Fatalf("ping external metadata schema database: %v", err)
	}

	var existingTables int
	if err := database.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM information_schema.tables
		WHERE table_schema = DATABASE()
	`).Scan(&existingTables); err != nil {
		t.Fatalf("inspect external metadata schema database: %v", err)
	}
	if existingTables != 0 {
		t.Fatalf("external metadata schema database is not empty: %d tables", existingTables)
	}
	layout, err := detectMetadataSchemaLayoutContext(ctx, database)
	if err != nil {
		t.Fatalf("detect empty external metadata schema: %v", err)
	}
	if layout != metadataSchemaBootstrap {
		t.Fatalf("empty external metadata schema layout = %d; want bootstrap", layout)
	}

	previousBackend := config.Config.BackendDB
	previousPanicIfDifferent := config.Config.PanicIfDifferentDatabaseDeploy
	previousVersion := config.RuntimeCLIFlags.ConfiguredVersion
	config.Config.BackendDB = "mysql"
	config.Config.PanicIfDifferentDatabaseDeploy = false
	config.RuntimeCLIFlags.ConfiguredVersion = "metadata-schema-compatibility-test"
	t.Cleanup(func() {
		config.Config.BackendDB = previousBackend
		config.Config.PanicIfDifferentDatabaseDeploy = previousPanicIfDifferent
		config.RuntimeCLIFlags.ConfiguredVersion = previousVersion
	})
	statements := metadataschema.Statements()
	if err := deployCanonicalStatementsContext(ctx, database, statements[:12]); err != nil {
		t.Fatalf("simulate interrupted canonical bootstrap: %v", err)
	}
	layout, err = detectMetadataSchemaLayoutContext(ctx, database)
	if err != nil {
		t.Fatalf("detect interrupted external metadata schema: %v", err)
	}
	if layout != metadataSchemaBootstrap {
		t.Fatalf("interrupted external metadata schema layout = %d; want bootstrap", layout)
	}
	if err := initOrchestratorDBContext(ctx, database); err != nil {
		t.Fatalf("initialize canonical metadata schema: %v", err)
	}

	var version string
	if err := database.QueryRowContext(ctx, `SELECT VERSION()`).Scan(&version); err != nil {
		t.Fatalf("read external database version: %v", err)
	}
	t.Logf("validated canonical metadata schema on %s", version)

	var tableCount int
	var commentedTables int
	var normalizedTables int
	if err := database.QueryRowContext(ctx, `
		SELECT
			COUNT(*),
			SUM(CASE WHEN table_comment <> '' THEN 1 ELSE 0 END),
			SUM(CASE WHEN table_collation = 'utf8mb4_general_ci' THEN 1 ELSE 0 END)
		FROM information_schema.tables
		WHERE table_schema = DATABASE()
	`).Scan(&tableCount, &commentedTables, &normalizedTables); err != nil {
		t.Fatalf("inspect external metadata tables: %v", err)
	}
	wantTables := len(metadataschema.ManagedTables())
	if tableCount != wantTables || commentedTables != wantTables || normalizedTables != wantTables {
		t.Fatalf(
			"external tables total/commented/normalized = %d/%d/%d; want %d/%d/%d",
			tableCount,
			commentedTables,
			normalizedTables,
			wantTables,
			wantTables,
			wantTables,
		)
	}

	var columns int
	var commentedColumns int
	if err := database.QueryRowContext(ctx, `
		SELECT
			COUNT(*),
			SUM(CASE WHEN column_comment <> '' THEN 1 ELSE 0 END)
		FROM information_schema.columns
		WHERE table_schema = DATABASE()
	`).Scan(&columns, &commentedColumns); err != nil {
		t.Fatalf("inspect external metadata columns: %v", err)
	}
	if columns == 0 || commentedColumns != columns {
		t.Fatalf("external columns total/commented = %d/%d", columns, commentedColumns)
	}
	var characterColumns int
	var utf8mb4Columns int
	var utf8mb4BinaryColumns int
	if err := database.QueryRowContext(ctx, `
		SELECT
			COUNT(*),
			SUM(CASE WHEN character_set_name = 'utf8mb4' THEN 1 ELSE 0 END),
			SUM(CASE WHEN collation_name = 'utf8mb4_bin' THEN 1 ELSE 0 END)
		FROM information_schema.columns
		WHERE table_schema = DATABASE()
			AND character_set_name IS NOT NULL
	`).Scan(&characterColumns, &utf8mb4Columns, &utf8mb4BinaryColumns); err != nil {
		t.Fatalf("inspect external character column collations: %v", err)
	}
	if characterColumns == 0 || utf8mb4Columns != characterColumns || utf8mb4BinaryColumns == 0 {
		t.Fatalf(
			"external character/utf8mb4/utf8mb4_bin column counts = %d/%d/%d",
			characterColumns,
			utf8mb4Columns,
			utf8mb4BinaryColumns,
		)
	}

	var migrationCount int
	var pendingMigrationCount int
	if err := database.QueryRowContext(ctx, `
		SELECT
			SUM(CASE WHEN migration_id = ? THEN 1 ELSE 0 END),
			SUM(CASE WHEN migration_id = ? THEN 1 ELSE 0 END)
		FROM orchestrator_schema_migrations
	`, metadataschema.CanonicalMigration, metadataschema.CanonicalMigrationPending).Scan(
		&migrationCount,
		&pendingMigrationCount,
	); err != nil {
		t.Fatalf("read canonical migration marker: %v", err)
	}
	if migrationCount != 1 || pendingMigrationCount != 0 {
		t.Fatalf("canonical final/pending marker counts = %d/%d; want 1/0", migrationCount, pendingMigrationCount)
	}

	var secondaryIndexCount int
	if err := database.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM information_schema.statistics
		WHERE table_schema = DATABASE()
			AND index_name <> 'PRIMARY'
			AND seq_in_index = 1
	`).Scan(&secondaryIndexCount); err != nil {
		t.Fatalf("count external metadata secondary indexes: %v", err)
	}
	if secondaryIndexCount != 75 {
		t.Fatalf("external secondary index count = %d; want 75", secondaryIndexCount)
	}
}
