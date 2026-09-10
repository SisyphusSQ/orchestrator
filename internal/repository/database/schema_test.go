package database

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	metadataschema "github.com/openark/orchestrator/docs/schema"
	"github.com/openark/orchestrator/internal/config"
)

func TestMetadataDDLUsesUtf8mb4Only(t *testing.T) {
	legacyCharset := regexp.MustCompile(`(?i)\b(?:character\s+set|charset|collate)\s*=?\s*(?:ascii|latin1|utf8)\b`)
	for source, statements := range map[string][]string{
		"base":    generateSQLBase,
		"patches": generateSQLPatches,
	} {
		if match := legacyCharset.FindString(strings.Join(statements, "\n")); match != "" {
			t.Errorf("%s metadata DDL uses a non-utf8mb4 character set or collation: %q", source, match)
		}
	}
}

func openMetadataSchemaSQLite(t *testing.T) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open SQLite metadata schema fixture: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close SQLite metadata schema fixture: %v", err)
		}
	})
	return database
}

func useSQLiteMetadataBackend(t *testing.T) {
	t.Helper()
	previousBackend := config.Config.Metadata.Type
	config.Config.Metadata.Type = "sqlite3"
	t.Cleanup(func() {
		config.Config.Metadata.Type = previousBackend
	})
}

func TestCanonicalMetadataSchemaInitializesSQLite(t *testing.T) {
	useSQLiteMetadataBackend(t)
	database := openMetadataSchemaSQLite(t)

	if err := deployCanonicalStatementsContext(context.Background(), database, metadataschema.Statements()); err != nil {
		t.Fatalf("deploy canonical metadata schema: %v", err)
	}
	if err := deployCanonicalStatementsContext(context.Background(), database, metadataschema.Statements()); err != nil {
		t.Fatalf("redeploy canonical metadata schema: %v", err)
	}

	var tableCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`).Scan(&tableCount); err != nil {
		t.Fatalf("count SQLite metadata tables: %v", err)
	}
	if want := len(metadataschema.ManagedTables()); tableCount != want {
		t.Fatalf("SQLite metadata table count = %d; want %d", tableCount, want)
	}
	layout, err := detectMetadataSchemaLayoutContext(context.Background(), database)
	if err != nil {
		t.Fatalf("detect canonical SQLite layout: %v", err)
	}
	if layout != metadataSchemaCanonical {
		t.Fatalf("SQLite metadata layout = %d; want canonical", layout)
	}
}

func TestDetectMetadataSchemaLayout(t *testing.T) {
	useSQLiteMetadataBackend(t)

	t.Run("empty", func(t *testing.T) {
		database := openMetadataSchemaSQLite(t)
		layout, err := detectMetadataSchemaLayoutContext(context.Background(), database)
		if err != nil {
			t.Fatalf("detect empty layout: %v", err)
		}
		if layout != metadataSchemaBootstrap {
			t.Fatalf("layout = %d; want empty", layout)
		}
	})

	t.Run("legacy", func(t *testing.T) {
		database := openMetadataSchemaSQLite(t)
		if _, err := database.Exec(`CREATE TABLE database_instance (hostname TEXT NOT NULL)`); err != nil {
			t.Fatalf("create legacy marker table: %v", err)
		}
		layout, err := detectMetadataSchemaLayoutContext(context.Background(), database)
		if err != nil {
			t.Fatalf("detect legacy layout: %v", err)
		}
		if layout != metadataSchemaLegacy {
			t.Fatalf("layout = %d; want legacy", layout)
		}
	})

	for name, statementCount := range map[string]int{
		"migration ledger only": 1,
		"pending canonical":     2,
	} {
		t.Run(name, func(t *testing.T) {
			database := openMetadataSchemaSQLite(t)
			statements := metadataschema.Statements()
			if err := deployCanonicalStatementsContext(context.Background(), database, statements[:statementCount]); err != nil {
				t.Fatalf("deploy partial canonical schema: %v", err)
			}
			layout, err := detectMetadataSchemaLayoutContext(context.Background(), database)
			if err != nil {
				t.Fatalf("detect partial canonical layout: %v", err)
			}
			if layout != metadataSchemaBootstrap {
				t.Fatalf("layout = %d; want bootstrap", layout)
			}
		})
	}

	t.Run("damaged canonical", func(t *testing.T) {
		database := openMetadataSchemaSQLite(t)
		if err := deployCanonicalStatementsContext(context.Background(), database, metadataschema.Statements()); err != nil {
			t.Fatalf("deploy canonical schema: %v", err)
		}
		previousVersion := config.RuntimeCLIFlags.ConfiguredVersion
		config.RuntimeCLIFlags.ConfiguredVersion = "damaged-canonical-test"
		t.Cleanup(func() {
			config.RuntimeCLIFlags.ConfiguredVersion = previousVersion
		})
		if err := registerOrchestratorDeploymentContext(context.Background(), database); err != nil {
			t.Fatalf("register canonical application version: %v", err)
		}
		if _, err := database.Exec(`DROP TABLE audit`); err != nil {
			t.Fatalf("drop canonical table: %v", err)
		}
		if err := initOrchestratorDBContext(context.Background(), database); err == nil {
			t.Fatal("damaged canonical schema unexpectedly passed initialization")
		}
	})
}

func TestCanonicalMetadataSchemaResumesSQLite(t *testing.T) {
	useSQLiteMetadataBackend(t)
	database := openMetadataSchemaSQLite(t)
	ctx := context.Background()
	statements := metadataschema.Statements()
	if err := deployCanonicalStatementsContext(ctx, database, statements[:12]); err != nil {
		t.Fatalf("deploy interrupted canonical schema: %v", err)
	}

	previousPanicIfDifferent := config.Config.Metadata.Schema.PanicOnDifferentDeployment
	previousVersion := config.RuntimeCLIFlags.ConfiguredVersion
	config.Config.Metadata.Schema.PanicOnDifferentDeployment = false
	config.RuntimeCLIFlags.ConfiguredVersion = "canonical-resume-test"
	t.Cleanup(func() {
		config.Config.Metadata.Schema.PanicOnDifferentDeployment = previousPanicIfDifferent
		config.RuntimeCLIFlags.ConfiguredVersion = previousVersion
	})
	if err := initOrchestratorDBContext(ctx, database); err != nil {
		t.Fatalf("resume canonical metadata schema: %v", err)
	}

	var finalCount int
	var pendingCount int
	if err := database.QueryRow(`
		SELECT
			SUM(CASE WHEN migration_id = ? THEN 1 ELSE 0 END),
			SUM(CASE WHEN migration_id = ? THEN 1 ELSE 0 END)
		FROM orchestrator_schema_migrations
	`, metadataschema.CanonicalMigration, metadataschema.CanonicalMigrationPending).Scan(&finalCount, &pendingCount); err != nil {
		t.Fatalf("read canonical migration markers: %v", err)
	}
	if finalCount != 1 || pendingCount != 0 {
		t.Fatalf("canonical final/pending marker counts = %d/%d; want 1/0", finalCount, pendingCount)
	}
	config.RuntimeCLIFlags.ConfiguredVersion = "canonical-followup-test"
	if err := initOrchestratorDBContext(ctx, database); err != nil {
		t.Fatalf("initialize a new application version on canonical schema: %v", err)
	}
	var secondaryIndexCount int
	if err := database.QueryRow(`
		SELECT COUNT(*)
		FROM sqlite_master
		WHERE type = 'index'
			AND name NOT LIKE 'sqlite_autoindex_%'
	`).Scan(&secondaryIndexCount); err != nil {
		t.Fatalf("count canonical SQLite secondary indexes: %v", err)
	}
	if secondaryIndexCount != 108 {
		t.Fatalf("canonical SQLite secondary index count after follow-up version = %d; want 108", secondaryIndexCount)
	}
}

func TestCanonicalMetadataSchemaRejectsUnexpectedIndexErrors(t *testing.T) {
	useSQLiteMetadataBackend(t)

	t.Run("duplicate index is resumable", func(t *testing.T) {
		database := openMetadataSchemaSQLite(t)
		if _, err := database.Exec(`CREATE TABLE sample (id INTEGER NOT NULL)`); err != nil {
			t.Fatalf("create sample table: %v", err)
		}
		statements := []string{`CREATE INDEX idx_sample_id ON sample (id)`}
		if err := deployCanonicalStatementsContext(context.Background(), database, statements); err != nil {
			t.Fatalf("create sample index: %v", err)
		}
		if err := deployCanonicalStatementsContext(context.Background(), database, statements); err != nil {
			t.Fatalf("resume after sample index: %v", err)
		}
	})

	t.Run("invalid index fails", func(t *testing.T) {
		database := openMetadataSchemaSQLite(t)
		if _, err := database.Exec(`CREATE TABLE sample (id INTEGER NOT NULL)`); err != nil {
			t.Fatalf("create sample table: %v", err)
		}
		err := deployCanonicalStatementsContext(
			context.Background(),
			database,
			[]string{`CREATE INDEX idx_sample_missing ON sample (missing)`},
		)
		if err == nil {
			t.Fatal("invalid canonical index unexpectedly succeeded")
		}
	})
}

func sqliteTableShape(t *testing.T, database *sql.DB, table string) []string {
	t.Helper()
	rows, err := database.Query(fmt.Sprintf(`PRAGMA table_info(%q)`, table))
	if err != nil {
		t.Fatalf("read %s table shape: %v", table, err)
	}
	defer rows.Close()

	shape := []string{}
	for rows.Next() {
		var columnID int
		var name string
		var dataType string
		var notNull int
		var defaultValue sql.NullString
		var primaryKeyPosition int
		if err := rows.Scan(&columnID, &name, &dataType, &notNull, &defaultValue, &primaryKeyPosition); err != nil {
			t.Fatalf("scan %s table shape: %v", table, err)
		}
		shape = append(shape, fmt.Sprintf("%s|notnull=%d|pk=%d", name, notNull, primaryKeyPosition))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate %s table shape: %v", table, err)
	}
	sort.Strings(shape)
	return shape
}

func sqliteSecondaryIndexContracts(t *testing.T, database *sql.DB, table string) []string {
	t.Helper()
	rows, err := database.Query(fmt.Sprintf(`PRAGMA index_list(%q)`, table))
	if err != nil {
		t.Fatalf("read %s index list: %v", table, err)
	}
	type indexDefinition struct {
		name   string
		unique int
	}
	definitions := []indexDefinition{}
	for rows.Next() {
		var sequence int
		var definition indexDefinition
		var origin string
		var partial int
		if err := rows.Scan(&sequence, &definition.name, &definition.unique, &origin, &partial); err != nil {
			rows.Close()
			t.Fatalf("scan %s index list: %v", table, err)
		}
		if origin != "pk" {
			definitions = append(definitions, definition)
		}
	}
	if err := rows.Close(); err != nil {
		t.Fatalf("close %s index list: %v", table, err)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate %s index list: %v", table, err)
	}

	contracts := make(map[string]struct{}, len(definitions))
	for _, definition := range definitions {
		indexRows, err := database.Query(fmt.Sprintf(`PRAGMA index_info(%q)`, definition.name))
		if err != nil {
			t.Fatalf("read %s.%s index columns: %v", table, definition.name, err)
		}
		columns := []string{}
		for indexRows.Next() {
			var sequence int
			var columnID int
			var column string
			if err := indexRows.Scan(&sequence, &columnID, &column); err != nil {
				indexRows.Close()
				t.Fatalf("scan %s.%s index columns: %v", table, definition.name, err)
			}
			columns = append(columns, column)
		}
		if err := indexRows.Close(); err != nil {
			t.Fatalf("close %s.%s index columns: %v", table, definition.name, err)
		}
		if err := indexRows.Err(); err != nil {
			t.Fatalf("iterate %s.%s index columns: %v", table, definition.name, err)
		}
		contracts[fmt.Sprintf("unique=%d|%v", definition.unique, columns)] = struct{}{}
	}

	result := make([]string, 0, len(contracts))
	for contract := range contracts {
		result = append(result, contract)
	}
	sort.Strings(result)
	return result
}

func TestMigratedMetadataSchemaMatchesCanonicalContract(t *testing.T) {
	useSQLiteMetadataBackend(t)
	ctx := context.Background()

	canonical := openMetadataSchemaSQLite(t)
	if err := deployCanonicalStatementsContext(ctx, canonical, metadataschema.Statements()); err != nil {
		t.Fatalf("deploy canonical schema: %v", err)
	}
	legacy := openMetadataSchemaSQLite(t)
	if err := deployStatementsContext(ctx, legacy, generateSQLBase); err != nil {
		t.Fatalf("deploy legacy base schema: %v", err)
	}
	if err := deployStatementsContext(ctx, legacy, generateSQLPatches); err != nil {
		t.Fatalf("deploy legacy patch schema: %v", err)
	}
	var legacyTableCount int
	if err := legacy.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`).Scan(&legacyTableCount); err != nil {
		t.Fatalf("count legacy metadata tables: %v", err)
	}
	if want := len(metadataschema.ManagedTables()); legacyTableCount != want {
		t.Fatalf("legacy metadata table count = %d; want %d", legacyTableCount, want)
	}

	if err := migrateMetadataIDs(ctx, legacy); err != nil {
		t.Fatalf("migrate legacy ids: %v", err)
	}

	for _, table := range metadataschema.ManagedTables() {
		canonicalShape := sqliteTableShape(t, canonical, table)
		legacyShape := sqliteTableShape(t, legacy, table)
		if !reflect.DeepEqual(canonicalShape, legacyShape) {
			t.Errorf("%s canonical shape = %v; legacy shape = %v", table, canonicalShape, legacyShape)
		}
		canonicalIndexes := sqliteSecondaryIndexContracts(t, canonical, table)
		legacyIndexes := sqliteSecondaryIndexContracts(t, legacy, table)
		if !reflect.DeepEqual(canonicalIndexes, legacyIndexes) {
			t.Errorf("%s canonical index contracts = %v; legacy index contracts = %v", table, canonicalIndexes, legacyIndexes)
		}
	}
}

func TestLegacyMetadataSchemaRequiresExplicitMigration(t *testing.T) {
	useSQLiteMetadataBackend(t)
	database := openMetadataSchemaSQLite(t)
	ctx := context.Background()

	if err := deployStatementsContext(ctx, database, generateSQLBase); err != nil {
		t.Fatalf("deploy legacy base schema: %v", err)
	}
	legacyPatches := generateSQLPatches[:len(generateSQLPatches)-2]
	if err := deployStatementsContext(ctx, database, legacyPatches); err != nil {
		t.Fatalf("deploy historical legacy patches: %v", err)
	}
	layout, err := detectMetadataSchemaLayoutContext(ctx, database)
	if err != nil {
		t.Fatalf("detect historical legacy layout: %v", err)
	}
	if layout != metadataSchemaLegacy {
		t.Fatalf("historical layout = %d; want legacy", layout)
	}

	previousPanicIfDifferent := config.Config.Metadata.Schema.PanicOnDifferentDeployment
	previousVersion := config.RuntimeCLIFlags.ConfiguredVersion
	config.Config.Metadata.Schema.PanicOnDifferentDeployment = false
	config.RuntimeCLIFlags.ConfiguredVersion = "legacy-schema-test"
	t.Cleanup(func() {
		config.Config.Metadata.Schema.PanicOnDifferentDeployment = previousPanicIfDifferent
		config.RuntimeCLIFlags.ConfiguredVersion = previousVersion
	})
	if err := initOrchestratorDBContext(ctx, database); err == nil {
		t.Fatal("legacy schema upgraded without explicit authorization")
	}
	previousMigration := config.RuntimeCLIFlags.MigrateMetadataIDs
	config.RuntimeCLIFlags.MigrateMetadataIDs = true
	t.Cleanup(func() { config.RuntimeCLIFlags.MigrateMetadataIDs = previousMigration })
	if err := initOrchestratorDBContext(ctx, database); err != nil {
		t.Fatalf("upgrade legacy metadata schema: %v", err)
	}

	var markerCount int
	if err := database.QueryRow(`
		SELECT COUNT(*)
		FROM orchestrator_schema_migrations
		WHERE migration_id = ?
	`, metadataschema.LegacyMigration).Scan(&markerCount); err != nil {
		t.Fatalf("read legacy migration marker: %v", err)
	}
	if markerCount != 1 {
		t.Fatalf("legacy migration marker count = %d; want 1", markerCount)
	}
	layout, err = detectMetadataSchemaLayoutContext(ctx, database)
	if err != nil {
		t.Fatalf("detect upgraded legacy layout: %v", err)
	}
	if layout != metadataSchemaCanonical {
		t.Fatalf("upgraded layout = %d; want canonical", layout)
	}
}

func TestPendingBootstrapIsNotSkippedByDeploymentVersion(t *testing.T) {
	useSQLiteMetadataBackend(t)
	database := openMetadataSchemaSQLite(t)
	ctx := context.Background()
	previousVersion := config.RuntimeCLIFlags.ConfiguredVersion
	previousPanic := config.Config.Metadata.Schema.PanicOnDifferentDeployment
	config.RuntimeCLIFlags.ConfiguredVersion = "already-recorded"
	config.Config.Metadata.Schema.PanicOnDifferentDeployment = false
	t.Cleanup(func() {
		config.RuntimeCLIFlags.ConfiguredVersion = previousVersion
		config.Config.Metadata.Schema.PanicOnDifferentDeployment = previousPanic
	})
	if err := deployCanonicalStatementsContext(ctx, database, metadataschema.Statements()[:12]); err != nil {
		t.Fatal(err)
	}
	if err := registerOrchestratorDeploymentContext(ctx, database); err != nil {
		t.Fatal(err)
	}
	if err := initOrchestratorDBContext(ctx, database); err != nil {
		t.Fatal(err)
	}
	if err := validateMetadataIDs(ctx, database); err != nil {
		t.Fatal(err)
	}
}
