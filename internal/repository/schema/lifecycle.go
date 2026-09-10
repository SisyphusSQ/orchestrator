package schema

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/go-sql-driver/mysql"
	metadataschema "github.com/openark/orchestrator/docs/schema"
	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/golib/log"
)

type Layout uint8

const (
	LayoutBootstrap Layout = iota
	LayoutLegacy
	LayoutCanonical
	LayoutUpgrade
)

func LegacyBaseStatements() []string {
	return generateSQLBase
}

func LegacyPatchStatements() []string {
	return generateSQLPatches
}

func translate(statement string) (string, error) {
	if config.Config.IsSQLite() {
		statement = ToSQLiteDialect(statement)
	}
	return statement, nil
}

func execContext(ctx context.Context, database *sql.DB, query string, args ...any) (sql.Result, error) {
	query, err := translate(query)
	if err != nil {
		return nil, err
	}
	return database.ExecContext(ctx, query, args...)
}

func versionIsDeployedContext(ctx context.Context, database *sql.DB) (bool, error) {
	var result bool
	err := database.QueryRowContext(ctx, `
		select count(*) as is_deployed
		from orchestrator_db_deployments
		where deployed_version = ?
	`, config.RuntimeCLIFlags.ConfiguredVersion).Scan(&result)
	return result, err
}

func registerDeploymentContext(ctx context.Context, database *sql.DB) error {
	if _, err := execContext(ctx, database, `
		replace into orchestrator_db_deployments (
			deployed_version, deployed_timestamp
		) values (?, NOW())
	`, config.RuntimeCLIFlags.ConfiguredVersion); err != nil {
		return fmt.Errorf("write orchestrator deployment metadata: %w", err)
	}
	log.Debugf("Migrated database schema to version [%+v]", config.RuntimeCLIFlags.ConfiguredVersion)
	return nil
}

func RegisterDeployment(ctx context.Context, database *sql.DB) error {
	return registerDeploymentContext(ctx, database)
}

func DetectLayout(ctx context.Context, database *sql.DB) (Layout, error) {
	tables := metadataschema.ManagedTables()
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(tables)), ",")
	args := make([]any, 0, len(tables)+1)
	args = append(args, "orchestrator_schema_migrations")
	for _, table := range tables {
		args = append(args, table)
	}

	tableNameColumn := "table_name"
	tableSource := "information_schema.tables"
	tableFilter := "table_schema = DATABASE()"
	if config.Config.IsSQLite() {
		tableNameColumn = "name"
		tableSource = "sqlite_master"
		tableFilter = "type = 'table'"
	}
	query := fmt.Sprintf(`
		SELECT COUNT(*), COALESCE(SUM(CASE WHEN %s = ? THEN 1 ELSE 0 END), 0)
		FROM %s
		WHERE %s AND %s IN (%s)
	`, tableNameColumn, tableSource, tableFilter, tableNameColumn, placeholders)

	var managedTableCount int
	var migrationTableCount int
	if err := database.QueryRowContext(ctx, query, args...).Scan(&managedTableCount, &migrationTableCount); err != nil {
		return LayoutBootstrap, fmt.Errorf("inspect orchestrator metadata tables: %w", err)
	}
	if managedTableCount == 0 {
		return LayoutBootstrap, nil
	}
	if migrationTableCount == 0 {
		return LayoutLegacy, nil
	}

	var canonicalMigrationCount, pendingMigrationCount, previousMigrationCount, upgradePendingCount int
	if err := database.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(CASE WHEN migration_id = ? THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN migration_id = ? THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN migration_id IN (?, ?) THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN migration_id = ? THEN 1 ELSE 0 END), 0)
		FROM orchestrator_schema_migrations
	`, metadataschema.CanonicalMigration, metadataschema.CanonicalMigrationPending,
		metadataschema.PreviousCanonicalMigration, metadataschema.PreviousCanonicalMigration+"-pending", metadataschema.UpgradeMigrationPending).Scan(
		&canonicalMigrationCount, &pendingMigrationCount, &previousMigrationCount, &upgradePendingCount); err != nil {
		return LayoutBootstrap, fmt.Errorf("inspect canonical schema migration: %w", err)
	}
	if upgradePendingCount > 0 {
		return LayoutUpgrade, nil
	}
	if canonicalMigrationCount > 0 {
		if managedTableCount != len(tables) {
			return LayoutBootstrap, fmt.Errorf("canonical metadata schema has %d of %d managed tables", managedTableCount, len(tables))
		}
		if err := validateMetadataIDs(ctx, database); err != nil {
			return LayoutCanonical, err
		}
		return LayoutCanonical, nil
	}
	if previousMigrationCount > 0 {
		return LayoutUpgrade, nil
	}
	if pendingMigrationCount > 0 {
		return LayoutBootstrap, nil
	}
	if managedTableCount == 1 {
		columns, err := metadataColumns(ctx, database, "orchestrator_schema_migrations")
		if err != nil {
			return LayoutBootstrap, err
		}
		for _, column := range columns {
			if column.Name == "id" {
				return LayoutBootstrap, nil
			}
		}
		return LayoutUpgrade, nil
	}
	return LayoutLegacy, nil
}

func isDuplicateIndexError(err error) bool {
	var mysqlError *mysql.MySQLError
	if errors.As(err, &mysqlError) {
		return mysqlError.Number == 1061
	}
	return config.Config.IsSQLite() && strings.Contains(strings.ToLower(err.Error()), "already exists")
}

func deployStatementsWithPolicyContext(ctx context.Context, database *sql.DB, queries []string, legacyCompatibilityMode bool) error {
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin orchestrator deployment transaction: %w", err)
	}
	defer tx.Rollback()
	originalSQLMode := ""
	if config.Config.IsMySQL() && legacyCompatibilityMode {
		if err := tx.QueryRowContext(ctx, `select @@session.sql_mode`).Scan(&originalSQLMode); err != nil {
			return fmt.Errorf("read SQL mode before orchestrator deployment: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `set @@session.sql_mode=REPLACE(@@session.sql_mode, 'NO_ZERO_DATE', '')`); err != nil {
			return fmt.Errorf("relax NO_ZERO_DATE for orchestrator deployment: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `set @@session.sql_mode=REPLACE(@@session.sql_mode, 'NO_ZERO_IN_DATE', '')`); err != nil {
			return fmt.Errorf("relax NO_ZERO_IN_DATE for orchestrator deployment: %w", err)
		}
	}
	for _, query := range queries {
		translated, err := translate(query)
		if err != nil {
			return fmt.Errorf("translate orchestrator deployment query %q: %w", query, err)
		}
		if _, err := tx.ExecContext(ctx, translated); err != nil {
			if strings.Contains(err.Error(), "syntax error") {
				return fmt.Errorf("execute orchestrator deployment query %q: %w", translated, err)
			}
			if !legacyCompatibilityMode {
				if IsCreateIndex(translated) && isDuplicateIndexError(err) {
					continue
				}
				return fmt.Errorf("execute canonical metadata schema query %q: %w", translated, err)
			}
			if !IsAlterTable(translated) && !IsCreateIndex(translated) && !IsDropIndex(translated) {
				return fmt.Errorf("execute orchestrator deployment query %q: %w", translated, err)
			}
			if !strings.Contains(err.Error(), "duplicate column name") &&
				!strings.Contains(err.Error(), "Duplicate column name") &&
				!strings.Contains(err.Error(), "check that column/key exists") &&
				!strings.Contains(err.Error(), "already exists") &&
				!strings.Contains(err.Error(), "Duplicate key name") {
				log.Errorf("Error initiating orchestrator: %+v; query=%+v", err, translated)
			}
		}
	}
	if config.Config.IsMySQL() && legacyCompatibilityMode {
		if _, err := tx.ExecContext(ctx, `set session sql_mode=?`, originalSQLMode); err != nil {
			return fmt.Errorf("restore SQL mode after orchestrator deployment: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit orchestrator deployment: %w", err)
	}
	return nil
}

func DeployStatements(ctx context.Context, database *sql.DB, queries []string) error {
	return deployStatementsWithPolicyContext(ctx, database, queries, true)
}

func DeployCanonicalStatements(ctx context.Context, database *sql.DB, queries []string) error {
	return deployStatementsWithPolicyContext(ctx, database, queries, false)
}

func ValidateMetadataIDs(ctx context.Context, database *sql.DB) error {
	return validateMetadataIDs(ctx, database)
}

func MigrateMetadataIDs(ctx context.Context, database *sql.DB) error {
	return migrateMetadataIDs(ctx, database)
}

func PrepareMetadataIDMigration(ctx context.Context, database *sql.DB, layout Layout) error {
	return prepareMetadataIDMigration(ctx, database, layout)
}

func Initialize(ctx context.Context, database *sql.DB) error {
	log.Debug("Initializing orchestrator")
	versionAlreadyDeployed, deploymentErr := versionIsDeployedContext(ctx, database)
	layout, err := DetectLayout(ctx, database)
	if err != nil {
		return err
	}
	if layout == LayoutLegacy || layout == LayoutUpgrade {
		if !config.RuntimeCLIFlags.MigrateMetadataIDs {
			return fmt.Errorf("metadata schema requires explicit id migration; stop all writers, back up the database, then run orchestrator admin migrate-metadata-id --config=<config>")
		}
		if err := PrepareMetadataIDMigration(ctx, database, layout); err != nil {
			return err
		}
		if err := MigrateMetadataIDs(ctx, database); err != nil {
			return err
		}
		layout = LayoutCanonical
	}
	if layout == LayoutCanonical && versionAlreadyDeployed && config.RuntimeCLIFlags.ConfiguredVersion != "" && deploymentErr == nil {
		return nil
	}
	if config.Config.Metadata.Schema.PanicOnDifferentDeployment && config.RuntimeCLIFlags.ConfiguredVersion != "" && !versionAlreadyDeployed {
		return fmt.Errorf("PanicIfDifferentDatabaseDeploy is set: configured version %s is not present in the database", config.RuntimeCLIFlags.ConfiguredVersion)
	}
	switch layout {
	case LayoutBootstrap:
		log.Debug("Bootstrapping or resuming canonical metadata schema")
		statements := metadataschema.Statements()
		if err := DeployCanonicalStatements(ctx, database, statements[:len(statements)-2]); err != nil {
			return err
		}
		if err := ValidateMetadataIDs(ctx, database); err != nil {
			return err
		}
		if err := DeployCanonicalStatements(ctx, database, statements[len(statements)-2:]); err != nil {
			return err
		}
	case LayoutCanonical:
		log.Debug("Canonical metadata schema is already bootstrapped")
	default:
		return fmt.Errorf("unsupported metadata schema layout: %d", layout)
	}
	if err := registerDeploymentContext(ctx, database); err != nil {
		return err
	}
	if config.Config.IsSQLite() {
		if _, err := execContext(ctx, database, `PRAGMA journal_mode = WAL`); err != nil {
			return fmt.Errorf("enable SQLite WAL mode: %w", err)
		}
		if _, err := execContext(ctx, database, `PRAGMA synchronous = NORMAL`); err != nil {
			return fmt.Errorf("configure SQLite synchronous mode: %w", err)
		}
	}
	return nil
}
