package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/openark/golib/log"
	"gorm.io/driver/mysql"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

const backendSlowQueryThreshold = 200 * time.Millisecond

// ErrLastInsertIDUnavailable documents the boundary between ordinary backend
// writes (GORM) and the few statements that require a driver LastInsertId.
var ErrLastInsertIDUnavailable = errors.New("last insert ID is unavailable for GORM backend execution")

type backendResult struct {
	rowsAffected int64
}

func (result backendResult) LastInsertId() (int64, error) {
	return 0, ErrLastInsertIDUnavailable
}

func (result backendResult) RowsAffected() (int64, error) {
	return result.rowsAffected, nil
}

type backendGORMLogger struct {
	level         gormlogger.LogLevel
	slowThreshold time.Duration
}

func newBackendGORMLogger() gormlogger.Interface {
	return backendGORMLogger{
		level:         gormlogger.Warn,
		slowThreshold: backendSlowQueryThreshold,
	}
}

func (logger backendGORMLogger) LogMode(level gormlogger.LogLevel) gormlogger.Interface {
	logger.level = level
	return logger
}

func (logger backendGORMLogger) Info(_ context.Context, _ string, _ ...interface{}) {
	if logger.level >= gormlogger.Info {
		log.Sugar().Debug("GORM backend info")
	}
}

func (logger backendGORMLogger) Warn(_ context.Context, _ string, _ ...interface{}) {
	if logger.level >= gormlogger.Warn {
		log.Sugar().Warn("GORM backend warning")
	}
}

func (logger backendGORMLogger) Error(_ context.Context, _ string, _ ...interface{}) {
	if logger.level >= gormlogger.Error {
		log.Sugar().Error("GORM backend error")
	}
}

func (logger backendGORMLogger) Trace(
	_ context.Context,
	begin time.Time,
	_ func() (sql string, rowsAffected int64),
	err error,
) {
	if logger.level == gormlogger.Silent {
		return
	}
	elapsed := time.Since(begin)
	if err != nil && logger.level >= gormlogger.Error && !errors.Is(err, gorm.ErrRecordNotFound) {
		log.Sugar().Errorw("GORM backend query failed",
			"duration", elapsed,
			"error", err,
		)
		return
	}
	if elapsed > logger.slowThreshold && logger.level >= gormlogger.Warn {
		log.Sugar().Warnw("GORM backend slow query",
			"duration", elapsed,
		)
	}
}

func newBackendGORM(database *sql.DB, sqliteBackend bool) (*gorm.DB, error) {
	if database == nil {
		return nil, errors.New("backend database pool is nil")
	}
	var dialector gorm.Dialector
	if sqliteBackend {
		dialector = sqlite.New(sqlite.Config{Conn: database})
	} else {
		dialector = mysql.New(mysql.Config{
			Conn:                      database,
			SkipInitializeWithVersion: true,
		})
	}
	handle, err := gorm.Open(dialector, &gorm.Config{
		SkipDefaultTransaction: true,
		PrepareStmt:            false,
		DisableAutomaticPing:   true,
		TranslateError:         false,
		Logger:                 newBackendGORMLogger(),
	})
	if err != nil {
		return nil, fmt.Errorf("initialize GORM backend: %w", err)
	}
	return handle, nil
}

// OpenOrchestratorGORMContext returns a request-scoped GORM session backed by
// the single process-owned *sql.DB. GORM never owns or closes that pool.
func OpenOrchestratorGORMContext(ctx context.Context) (*gorm.DB, error) {
	if ctx == nil {
		return nil, errors.New("GORM backend context is nil")
	}
	return processDatabaseRuntime.openBackendGORM(ctx)
}

// QueryOrchestratorRows executes a stable backend query into a query-specific
// DTO slice. Callers should use explicit gorm column tags for every field.
func QueryOrchestratorRows[T any](ctx context.Context, query string, args ...interface{}) ([]T, error) {
	translated, err := translateStatement(query)
	if err != nil {
		return nil, fmt.Errorf("translate orchestrator query: %w", err)
	}
	handle, err := OpenOrchestratorGORMContext(ctx)
	if err != nil {
		return nil, err
	}
	rows := make([]T, 0)
	result := handle.Raw(translated, args...).Scan(&rows)
	if result.Error != nil {
		return nil, fmt.Errorf("query orchestrator backend: %w", result.Error)
	}
	return rows, nil
}

// ExecOrchestratorGORM executes backend SQL through an existing GORM session,
// preserving backend dialect translation inside explicit transactions.
func ExecOrchestratorGORM(handle *gorm.DB, query string, args ...interface{}) (int64, error) {
	if handle == nil {
		return 0, errors.New("GORM backend handle is nil")
	}
	translated, err := translateStatement(query)
	if err != nil {
		return 0, fmt.Errorf("translate orchestrator statement: %w", err)
	}
	result := handle.Exec(translated, args...)
	if result.Error != nil {
		return 0, fmt.Errorf("execute orchestrator backend: %w", result.Error)
	}
	return result.RowsAffected, nil
}

// ExecOrchestratorSQLContext is reserved for backend statements whose caller
// requires a real driver sql.Result (notably LastInsertId).
func ExecOrchestratorSQLContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	translated, err := translateStatement(query)
	if err != nil {
		return nil, err
	}
	database, err := OpenOrchestratorContext(ctx)
	if err != nil {
		return nil, err
	}
	result, err := database.ExecContext(ctx, translated, args...)
	if err != nil {
		return nil, fmt.Errorf("execute orchestrator SQL adapter: %w", err)
	}
	return result, nil
}
