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
	"fmt"
	"strings"
	"time"

	"github.com/openark/golib/log"
	"github.com/openark/golib/sqlutils"
	"github.com/openark/orchestrator/go/config"
)

var EmptyArgs []interface{}

type DummySqlResult struct {
}

func (this DummySqlResult) LastInsertId() (int64, error) {
	return 0, nil
}

func (this DummySqlResult) RowsAffected() (int64, error) {
	return 1, nil
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
	logger := SqlUtilsLogger{client_context: cfg.Addr, backend_connection: false}
	return processDatabaseRuntime.openTopologyPool(ctx, role, cfg, logger)
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
		statement = sqlutils.ToSqlite3Dialect(statement)
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

// registerOrchestratorDeployment updates the orchestrator_metadata table upon successful deployment
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

// deployStatements will issue given sql queries that are not already known to be deployed.
// This iterates both lists (to-run and already-deployed) and also verifies no contraditions.
func deployStatements(db *sql.DB, queries []string) error {
	return deployStatementsContext(context.Background(), db, queries)
}

func deployStatementsContext(ctx context.Context, db *sql.DB, queries []string) error {
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
	if config.Config.IsMySQL() {
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
			if !sqlutils.IsAlterTable(query) && !sqlutils.IsCreateIndex(query) && !sqlutils.IsDropIndex(query) {
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
	if config.Config.IsMySQL() {
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
	if versionAlreadyDeployed && config.RuntimeCLIFlags.ConfiguredVersion != "" && err == nil {
		// Already deployed with this version
		return nil
	}
	if config.Config.PanicIfDifferentDatabaseDeploy && config.RuntimeCLIFlags.ConfiguredVersion != "" && !versionAlreadyDeployed {
		return fmt.Errorf("PanicIfDifferentDatabaseDeploy is set: configured version %s is not present in the database", config.RuntimeCLIFlags.ConfiguredVersion)
	}
	log.Debugf("Migrating database schema")
	if err := deployStatementsContext(ctx, db, generateSQLBase); err != nil {
		return err
	}
	if err := deployStatementsContext(ctx, db, generateSQLPatches); err != nil {
		return err
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
	return sqlutils.ExecNoPrepareContext(ctx, db, translated, args...)
}

// ExecOrchestrator will execute given query on the orchestrator backend database.
func ExecOrchestrator(query string, args ...interface{}) (sql.Result, error) {
	return ExecOrchestratorContext(context.Background(), query, args...)
}

// ExecOrchestratorContext executes a backend statement with caller-provided
// cancellation and deadline semantics.
func ExecOrchestratorContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	translated, err := translateStatement(query)
	if err != nil {
		return nil, err
	}
	database, err := OpenOrchestratorContext(ctx)
	if err != nil {
		return nil, err
	}
	return sqlutils.ExecNoPrepareContext(ctx, database, translated, args...)
}

// QueryRowsMapOrchestrator
func QueryOrchestratorRowsMap(query string, on_row func(sqlutils.RowMap) error) error {
	query, err := translateStatement(query)
	if err != nil {
		return fmt.Errorf("translate orchestrator query %q: %w", query, err)
	}
	db, err := OpenOrchestrator()
	if err != nil {
		return err
	}

	return sqlutils.QueryRowsMap(db, query, on_row)
}

// QueryOrchestrator
func QueryOrchestrator(query string, argsArray []interface{}, on_row func(sqlutils.RowMap) error) error {
	query, err := translateStatement(query)
	if err != nil {
		return fmt.Errorf("translate orchestrator query %q: %w", query, err)
	}
	db, err := OpenOrchestrator()
	if err != nil {
		return err
	}

	return log.Criticale(sqlutils.QueryRowsMap(db, query, on_row, argsArray...))
}

// QueryOrchestratorRowsMapBuffered
func QueryOrchestratorRowsMapBuffered(query string, on_row func(sqlutils.RowMap) error) error {
	query, err := translateStatement(query)
	if err != nil {
		return fmt.Errorf("translate orchestrator query %q: %w", query, err)
	}
	db, err := OpenOrchestrator()
	if err != nil {
		return err
	}

	return sqlutils.QueryRowsMapBuffered(db, query, on_row)
}

// QueryOrchestratorBuffered
func QueryOrchestratorBuffered(query string, argsArray []interface{}, on_row func(sqlutils.RowMap) error) error {
	query, err := translateStatement(query)
	if err != nil {
		return fmt.Errorf("translate orchestrator query %q: %w", query, err)
	}
	db, err := OpenOrchestrator()
	if err != nil {
		return err
	}

	if argsArray == nil {
		argsArray = EmptyArgs
	}
	return log.Criticale(sqlutils.QueryRowsMapBuffered(db, query, on_row, argsArray...))
}

// ReadTimeNow reads and returns the current timestamp as string. This is an unfortunate workaround
// to support both MySQL and SQLite in all possible timezones. SQLite only speaks UTC where MySQL has
// timezone support. By reading the time as string we get the database's de-facto notion of the time,
// which we can then feed back to it.
func ReadTimeNow() (timeNow string, err error) {
	err = QueryOrchestrator(`select now() as time_now`, nil, func(m sqlutils.RowMap) error {
		timeNow = m.GetString("time_now")
		return nil
	})
	return timeNow, err
}
