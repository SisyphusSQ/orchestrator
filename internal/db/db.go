/*
   Copyright 2014 Outbrain Inc.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/mattn/go-sqlite3"
	metadataschema "github.com/openark/orchestrator/docs/schema"
	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/golib/log"
)

// IsDuplicateKeyError identifies backend uniqueness violations without relying
// on error text so optimistic-create callers can report a stable conflict.
func IsDuplicateKeyError(err error) bool {
	var mysqlError *mysql.MySQLError
	if errors.As(err, &mysqlError) {
		return mysqlError.Number == 1062
	}
	var sqliteError sqlite3.Error
	if errors.As(err, &sqliteError) {
		return sqliteError.ExtendedCode == sqlite3.ErrConstraintPrimaryKey || sqliteError.ExtendedCode == sqlite3.ErrConstraintUnique
	}
	return false
}

// OpenDiscovery returns a DB instance to access a topology instance.
// It has lower read timeout than OpenTopology and is intended to
// be used with low-latency discovery queries.
func OpenDiscovery(host string, port int) (*sql.DB, error) {
	return OpenDiscoveryContext(context.Background(), host, port)
}

// OpenTopology returns a DB instance to access a topology instance.
func OpenTopology(host string, port int) (*sql.DB, error) {
	return OpenTopologyContext(context.Background(), host, port)
}

// OpenDiscoveryContext is the context-aware form of OpenDiscovery.
func OpenDiscoveryContext(ctx context.Context, host string, port int) (*sql.DB, error) {
	return openTopologyContext(
		ctx,
		topologyConnectionDiscovery,
		host,
		port,
		time.Duration(config.Config.MySQLDiscoveryReadTimeoutSeconds)*time.Second,
	)
}

// OpenTopologyContext is the context-aware form of OpenTopology.
func OpenTopologyContext(ctx context.Context, host string, port int) (*sql.DB, error) {
	return openTopologyContext(
		ctx,
		topologyConnectionOperation,
		host,
		port,
		time.Duration(config.Config.MySQLTopologyReadTimeoutSeconds)*time.Second,
	)
}

func openTopologyContext(
	ctx context.Context,
	role topologyConnectionRole,
	host string,
	port int,
	readTimeout time.Duration,
) (*sql.DB, error) {
	cfg := newTopologyMySQLConfig(host, port, readTimeout)
	if config.Config.MySQLTopologyUseMutualTLS {
		if err := configureTopologyTLS(cfg); err != nil {
			return nil, err
		}
	} else if config.Config.MySQLTopologyUseMixedTLS {
		required, err := requiresTLSContext(ctx, host, port, cfg)
		if err != nil {
			return nil, err
		}
		if required {
			if err := configureTopologyTLS(cfg); err != nil {
				return nil, err
			}
		}
	}
	return processDatabaseRuntime.openTopologyPool(ctx, role, cfg)
}

func IsSQLite() bool {
	return config.Config.IsSQLite()
}

func isInMemorySQLite() bool {
	return config.Config.IsSQLite() && strings.Contains(config.Config.SQLite3DataFile, ":memory:")
}

// OpenOrchestrator returns the process-owned orchestrator backend pool.
// New code should use OpenOrchestratorContext so cancellation reaches the driver.
func OpenOrchestrator() (*sql.DB, error) {
	return OpenOrchestratorContext(context.Background())
}

func translateStatement(statement string) (string, error) {
	if IsSQLite() {
		statement = ToSQLiteDialect(statement)
	}
	return statement, nil
}

// versionIsDeployed checks if given version has already been deployed
func versionIsDeployed(db *sql.DB) (result bool, err error) {
	return versionIsDeployedContext(context.Background(), db)
}

func versionIsDeployedContext(ctx context.Context, db *sql.DB) (result bool, err error) {
	query := `
		select
			count(*) as is_deployed
		from
			orchestrator_db_deployments
		where
			deployed_version = ?
		`
	err = db.QueryRowContext(ctx, query, config.RuntimeCLIFlags.ConfiguredVersion).Scan(&result)
	// err means the table 'orchestrator_db_deployments' does not even exist, in which case we proceed
	// to deploy.
	// If there's another error to this, like DB gone bad, then we're about to find out anyway.
	return result, err
}

// registerOrchestratorDeployment records the application version after successful schema initialization.
func registerOrchestratorDeployment(db *sql.DB) error {
	return registerOrchestratorDeploymentContext(context.Background(), db)
}

func registerOrchestratorDeploymentContext(ctx context.Context, db *sql.DB) error {
	query := `
    	replace into orchestrator_db_deployments (
				deployed_version, deployed_timestamp
			) values (
				?, NOW()
			)
				`
	if _, err := execInternalContext(ctx, db, query, config.RuntimeCLIFlags.ConfiguredVersion); err != nil {
		return fmt.Errorf("write orchestrator deployment metadata: %w", err)
	}
	log.Debugf("Migrated database schema to version [%+v]", config.RuntimeCLIFlags.ConfiguredVersion)
	return nil
}

type metadataSchemaLayout uint8

const (
	metadataSchemaBootstrap metadataSchemaLayout = iota
	metadataSchemaLegacy
	metadataSchemaCanonical
)

func detectMetadataSchemaLayoutContext(ctx context.Context, db *sql.DB) (metadataSchemaLayout, error) {
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
	if IsSQLite() {
		tableNameColumn = "name"
		tableSource = "sqlite_master"
		tableFilter = "type = 'table'"
	}
	query := fmt.Sprintf(`
		SELECT
			COUNT(*),
			COALESCE(SUM(CASE WHEN %s = ? THEN 1 ELSE 0 END), 0)
		FROM %s
		WHERE %s
			AND %s IN (%s)
	`, tableNameColumn, tableSource, tableFilter, tableNameColumn, placeholders)

	var managedTableCount int
	var migrationTableCount int
	if err := db.QueryRowContext(ctx, query, args...).Scan(&managedTableCount, &migrationTableCount); err != nil {
		return metadataSchemaBootstrap, fmt.Errorf("inspect orchestrator metadata tables: %w", err)
	}
	if managedTableCount == 0 {
		return metadataSchemaBootstrap, nil
	}
	if migrationTableCount == 0 {
		return metadataSchemaLegacy, nil
	}

	var canonicalMigrationCount int
	var pendingMigrationCount int
	if err := db.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(CASE WHEN migration_id = ? THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN migration_id = ? THEN 1 ELSE 0 END), 0)
		FROM orchestrator_schema_migrations
	`, metadataschema.CanonicalMigration, metadataschema.CanonicalMigrationPending).Scan(
		&canonicalMigrationCount,
		&pendingMigrationCount,
	); err != nil {
		return metadataSchemaBootstrap, fmt.Errorf("inspect canonical schema migration: %w", err)
	}
	if canonicalMigrationCount > 0 {
		if managedTableCount != len(tables) {
			return metadataSchemaBootstrap, fmt.Errorf(
				"canonical metadata schema has %d of %d managed tables",
				managedTableCount,
				len(tables),
			)
		}
		return metadataSchemaCanonical, nil
	}
	if pendingMigrationCount > 0 || managedTableCount == 1 {
		return metadataSchemaBootstrap, nil
	}
	return metadataSchemaLegacy, nil
}

// deployStatements will issue given sql queries that are not already known to be deployed.
// This iterates both lists (to-run and already-deployed) and also verifies no contraditions.
func deployStatements(db *sql.DB, queries []string) error {
	return deployStatementsContext(context.Background(), db, queries)
}

func deployStatementsContext(ctx context.Context, db *sql.DB, queries []string) error {
	return deployStatementsWithPolicyContext(ctx, db, queries, true)
}

func deployCanonicalStatementsContext(ctx context.Context, db *sql.DB, queries []string) error {
	return deployStatementsWithPolicyContext(ctx, db, queries, false)
}

func isDuplicateIndexError(err error) bool {
	var mysqlError *mysql.MySQLError
	if errors.As(err, &mysqlError) {
		return mysqlError.Number == 1061
	}
	return IsSQLite() && strings.Contains(strings.ToLower(err.Error()), "already exists")
}

func deployStatementsWithPolicyContext(ctx context.Context, db *sql.DB, queries []string, legacyCompatibilityMode bool) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin orchestrator deployment transaction: %w", err)
	}
	defer tx.Rollback()
	// Ugly workaround ahead.
	// Origin of this workaround is the existence of some "timestamp NOT NULL," column definitions,
	// where in NO_ZERO_IN_DATE,NO_ZERO_DATE sql_mode are invalid (since default is implicitly "0")
	// This means installation of orchestrator fails on such configured servers, and in particular on 5.7
	// where this setting is the dfault.
	// For purpose of backwards compatibility, what we do is force sql_mode to be more relaxed, create the schemas
	// along with the "invalid" definition, and then go ahead and fix those definitions via following ALTER statements.
	// My bad.
	originalSqlMode := ""
	if config.Config.IsMySQL() && legacyCompatibilityMode {
		err = tx.QueryRowContext(ctx, `select @@session.sql_mode`).Scan(&originalSqlMode)
		if err != nil {
			return fmt.Errorf("read SQL mode before orchestrator deployment: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `set @@session.sql_mode=REPLACE(@@session.sql_mode, 'NO_ZERO_DATE', '')`); err != nil {
			return fmt.Errorf("relax NO_ZERO_DATE for orchestrator deployment: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `set @@session.sql_mode=REPLACE(@@session.sql_mode, 'NO_ZERO_IN_DATE', '')`); err != nil {
			return fmt.Errorf("relax NO_ZERO_IN_DATE for orchestrator deployment: %w", err)
		}
	}
	for i, query := range queries {
		if i == 0 {
			//log.Debugf("sql_mode is: %+v", originalSqlMode)
		}

		query, err := translateStatement(query)
		if err != nil {
			return fmt.Errorf("translate orchestrator deployment query %q: %w", query, err)
		}
		if _, err := tx.ExecContext(ctx, query); err != nil {
			if strings.Contains(err.Error(), "syntax error") {
				return fmt.Errorf("execute orchestrator deployment query %q: %w", query, err)
			}
			if !legacyCompatibilityMode {
				if IsCreateIndex(query) && isDuplicateIndexError(err) {
					continue
				}
				return fmt.Errorf("execute canonical metadata schema query %q: %w", query, err)
			}
			if !IsAlterTable(query) && !IsCreateIndex(query) && !IsDropIndex(query) {
				return fmt.Errorf("execute orchestrator deployment query %q: %w", query, err)
			}
			if !strings.Contains(err.Error(), "duplicate column name") &&
				!strings.Contains(err.Error(), "Duplicate column name") &&
				!strings.Contains(err.Error(), "check that column/key exists") &&
				!strings.Contains(err.Error(), "already exists") &&
				!strings.Contains(err.Error(), "Duplicate key name") {
				log.Errorf("Error initiating orchestrator: %+v; query=%+v", err, query)
			}
		}
	}
	if config.Config.IsMySQL() && legacyCompatibilityMode {
		if _, err := tx.ExecContext(ctx, `set session sql_mode=?`, originalSqlMode); err != nil {
			return fmt.Errorf("restore SQL mode after orchestrator deployment: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit orchestrator deployment: %w", err)
	}
	return nil
}

// initOrchestratorDB attempts to create/upgrade the orchestrator backend database. It is created once in the
// application's lifetime.
func initOrchestratorDB(db *sql.DB) error {
	return initOrchestratorDBContext(context.Background(), db)
}

func initOrchestratorDBContext(ctx context.Context, db *sql.DB) error {
	log.Debug("Initializing orchestrator")

	versionAlreadyDeployed, err := versionIsDeployedContext(ctx, db)
	layout, layoutErr := detectMetadataSchemaLayoutContext(ctx, db)
	if layoutErr != nil {
		return layoutErr
	}
	if versionAlreadyDeployed && config.RuntimeCLIFlags.ConfiguredVersion != "" && err == nil {
		// Already deployed with this version
		return nil
	}
	if config.Config.PanicIfDifferentDatabaseDeploy && config.RuntimeCLIFlags.ConfiguredVersion != "" && !versionAlreadyDeployed {
		return fmt.Errorf("PanicIfDifferentDatabaseDeploy is set: configured version %s is not present in the database", config.RuntimeCLIFlags.ConfiguredVersion)
	}
	switch layout {
	case metadataSchemaBootstrap:
		log.Debug("Bootstrapping or resuming canonical metadata schema")
		if err := deployCanonicalStatementsContext(ctx, db, metadataschema.Statements()); err != nil {
			return err
		}
	case metadataSchemaLegacy:
		log.Debug("Migrating legacy metadata schema")
		if err := deployStatementsContext(ctx, db, generateSQLBase); err != nil {
			return err
		}
		if err := deployStatementsContext(ctx, db, generateSQLPatches); err != nil {
			return err
		}
	case metadataSchemaCanonical:
		log.Debug("Canonical metadata schema is already bootstrapped")
	default:
		return fmt.Errorf("unsupported metadata schema layout: %d", layout)
	}
	if err := registerOrchestratorDeploymentContext(ctx, db); err != nil {
		return err
	}

	if IsSQLite() {
		if _, err := execInternalContext(ctx, db, `PRAGMA journal_mode = WAL`); err != nil {
			return fmt.Errorf("enable SQLite WAL mode: %w", err)
		}
		if _, err := execInternalContext(ctx, db, `PRAGMA synchronous = NORMAL`); err != nil {
			return fmt.Errorf("configure SQLite synchronous mode: %w", err)
		}
	}

	return nil
}

// execInternal
func execInternal(db *sql.DB, query string, args ...interface{}) (sql.Result, error) {
	return execInternalContext(context.Background(), db, query, args...)
}

func execInternalContext(ctx context.Context, db *sql.DB, query string, args ...interface{}) (sql.Result, error) {
	translated, err := translateStatement(query)
	if err != nil {
		return nil, err
	}
	return db.ExecContext(ctx, translated, args...)
}

// ExecOrchestrator will execute given query on the orchestrator backend database.
func ExecOrchestrator(query string, args ...interface{}) (sql.Result, error) {
	return ExecOrchestratorContext(context.Background(), query, args...)
}

// ExecOrchestratorContext executes a backend statement with caller-provided
// cancellation and deadline semantics.
func ExecOrchestratorContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	handle, err := OpenOrchestratorGORMContext(ctx)
	if err != nil {
		return nil, err
	}
	rowsAffected, err := ExecOrchestratorGORM(handle, query, args...)
	if err != nil {
		return nil, err
	}
	return backendResult{rowsAffected: rowsAffected}, nil
}

// ReadTimeNow reads and returns the current timestamp as string. This is an unfortunate workaround
// to support both MySQL and SQLite in all possible timezones. SQLite only speaks UTC where MySQL has
// timezone support. By reading the time as string we get the database's de-facto notion of the time,
// which we can then feed back to it.
func ReadTimeNow() (timeNow string, err error) {
	type timeRow struct {
		TimeNow string `gorm:"column:time_now"`
	}
	rows, err := QueryOrchestratorRows[timeRow](context.Background(), `select now() as time_now`)
	if err != nil || len(rows) == 0 {
		return "", err
	}
	return rows[0].TimeNow, nil
}
