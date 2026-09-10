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

package database

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/mattn/go-sqlite3"
	"github.com/openark/orchestrator/internal/config"
	repositoryschema "github.com/openark/orchestrator/internal/repository/schema"
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
		time.Duration(config.Config.Topology.MySQL.DiscoveryReadTimeoutSeconds)*time.Second,
	)
}

// OpenTopologyContext is the context-aware form of OpenTopology.
func OpenTopologyContext(ctx context.Context, host string, port int) (*sql.DB, error) {
	return openTopologyContext(
		ctx,
		topologyConnectionOperation,
		host,
		port,
		time.Duration(config.Config.Topology.MySQL.ReadTimeoutSeconds)*time.Second,
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
	if config.Config.Topology.MySQL.UseMutualTLS {
		if err := configureTopologyTLS(cfg); err != nil {
			return nil, err
		}
	} else if config.Config.Topology.MySQL.UseMixedTLS {
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
	return config.Config.IsSQLite() && strings.Contains(config.Config.Metadata.SQLite.DataFile, ":memory:")
}

// OpenOrchestrator returns the process-owned orchestrator backend pool.
// New code should use OpenOrchestratorContext so cancellation reaches the driver.
func OpenOrchestrator() (*sql.DB, error) {
	return OpenOrchestratorContext(context.Background())
}

func translateStatement(statement string) (string, error) {
	if IsSQLite() {
		statement = repositoryschema.ToSQLiteDialect(statement)
	}
	return statement, nil
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
