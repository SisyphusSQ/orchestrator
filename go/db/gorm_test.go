package db

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/openark/orchestrator/go/config"
	"gorm.io/gorm"
)

func useSQLiteBackendRuntime(t *testing.T) {
	t.Helper()
	previousBackendDB := config.Config.BackendDB
	previousSQLiteFile := config.Config.SQLite3DataFile
	previousSkipUpdate := config.Config.SkipOrchestratorDatabaseUpdate
	previousRuntime := processDatabaseRuntime
	config.Config.BackendDB = "sqlite3"
	config.Config.SQLite3DataFile = ":memory:"
	config.Config.SkipOrchestratorDatabaseUpdate = true
	processDatabaseRuntime = newDatabaseRuntime()
	t.Cleanup(func() {
		_ = processDatabaseRuntime.Close()
		processDatabaseRuntime = previousRuntime
		config.Config.BackendDB = previousBackendDB
		config.Config.SQLite3DataFile = previousSQLiteFile
		config.Config.SkipOrchestratorDatabaseUpdate = previousSkipUpdate
	})
}

func TestOpenOrchestratorGORMContextReusesRuntimeOwnedPool(t *testing.T) {
	useSQLiteBackendRuntime(t)
	ctx := context.Background()

	database, err := OpenOrchestratorContext(ctx)
	if err != nil {
		t.Fatalf("OpenOrchestratorContext() error: %v", err)
	}
	orm, err := OpenOrchestratorGORMContext(ctx)
	if err != nil {
		t.Fatalf("OpenOrchestratorGORMContext() error: %v", err)
	}
	underlying, err := orm.DB()
	if err != nil {
		t.Fatalf("gorm DB() error: %v", err)
	}
	if underlying != database {
		t.Fatal("GORM did not reuse the runtime-owned backend pool")
	}
	if !orm.Config.SkipDefaultTransaction {
		t.Fatal("GORM default write transactions are enabled")
	}
	if orm.Config.PrepareStmt {
		t.Fatal("GORM prepared statement cache is enabled")
	}
	if !orm.Config.DisableAutomaticPing {
		t.Fatal("GORM automatic ping is enabled")
	}
	if orm.Config.TranslateError {
		t.Fatal("GORM error translation is enabled")
	}
	if err := processDatabaseRuntime.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}
	if _, err := OpenOrchestratorGORMContext(ctx); !errors.Is(err, ErrDatabaseRuntimeClosed) {
		t.Fatalf("OpenOrchestratorGORMContext() after Close() error = %v; want ErrDatabaseRuntimeClosed", err)
	}
}

func TestQueryOrchestratorRowsScansTypedDTOAndPropagatesContext(t *testing.T) {
	useSQLiteBackendRuntime(t)
	ctx := context.Background()
	database, err := OpenOrchestratorContext(ctx)
	if err != nil {
		t.Fatalf("OpenOrchestratorContext() error: %v", err)
	}
	if _, err := database.ExecContext(ctx, `create table typed_fixture (id integer primary key, name text, optional text, optional_number integer)`); err != nil {
		t.Fatalf("create fixture: %v", err)
	}
	if _, err := database.ExecContext(ctx, `insert into typed_fixture (id, name, optional, optional_number) values (1, 'first', null, null)`); err != nil {
		t.Fatalf("insert fixture: %v", err)
	}

	type fixtureRow struct {
		ID             int64          `gorm:"column:id"`
		Name           string         `gorm:"column:name"`
		Optional       sql.NullString `gorm:"column:optional"`
		OptionalNumber sql.NullInt64  `gorm:"column:optional_number"`
	}
	rows, err := QueryOrchestratorRows[fixtureRow](ctx, `select id, name, optional, optional_number from typed_fixture where id = ?`, 1)
	if err != nil {
		t.Fatalf("QueryOrchestratorRows() error: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != 1 || rows[0].Name != "first" || rows[0].Optional.Valid || rows[0].OptionalNumber.Valid {
		t.Fatalf("typed rows = %#v; want one row with a NULL optional value", rows)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = QueryOrchestratorRows[fixtureRow](canceled, `select id, name, optional, optional_number from typed_fixture`)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled query error = %v; want context.Canceled", err)
	}
}

func TestConcurrentGORMOpenCannotOutliveRuntimeClose(t *testing.T) {
	useSQLiteBackendRuntime(t)
	const callers = 32
	start := make(chan struct{})
	errs := make(chan error, callers)
	var wait sync.WaitGroup
	wait.Add(callers)
	for range callers {
		go func() {
			defer wait.Done()
			<-start
			_, err := OpenOrchestratorGORMContext(context.Background())
			if err != nil && !errors.Is(err, ErrDatabaseRuntimeClosed) {
				errs <- err
			}
		}()
	}
	close(start)
	if err := processDatabaseRuntime.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}
	wait.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent OpenOrchestratorGORMContext() error: %v", err)
	}
	if processDatabaseRuntime.gorm != nil {
		t.Fatal("runtime close left a cached GORM handle")
	}
	if _, err := OpenOrchestratorGORMContext(context.Background()); !errors.Is(err, ErrDatabaseRuntimeClosed) {
		t.Fatalf("OpenOrchestratorGORMContext() after concurrent close error = %v; want ErrDatabaseRuntimeClosed", err)
	}
}

func TestQueryOrchestratorRowsScansTimestampsAndNulls(t *testing.T) {
	useSQLiteBackendRuntime(t)
	ctx := context.Background()
	database, err := OpenOrchestratorContext(ctx)
	if err != nil {
		t.Fatalf("OpenOrchestratorContext() error: %v", err)
	}
	if _, err := database.ExecContext(ctx, `create table timestamp_fixture (created_at timestamp not null, optional_at timestamp null)`); err != nil {
		t.Fatalf("create fixture: %v", err)
	}
	if _, err := database.ExecContext(ctx, `insert into timestamp_fixture (created_at, optional_at) values (datetime('now'), null)`); err != nil {
		t.Fatalf("insert fixture: %v", err)
	}

	type timestampRow struct {
		CreatedAt  string         `gorm:"column:created_at"`
		OptionalAt sql.NullString `gorm:"column:optional_at"`
	}
	rows, err := QueryOrchestratorRows[timestampRow](ctx, `select created_at, optional_at from timestamp_fixture`)
	if err != nil {
		t.Fatalf("QueryOrchestratorRows() error: %v", err)
	}
	if len(rows) != 1 || rows[0].CreatedAt == "" || rows[0].OptionalAt.Valid {
		t.Fatalf("timestamp rows = %#v; want one non-empty timestamp and one NULL", rows)
	}
}

func TestBackendGORMLoggerNeverMaterializesSQL(t *testing.T) {
	logger := backendGORMLogger{level: 4, slowThreshold: time.Nanosecond}
	queryCalled := false
	query := func() (string, int64) {
		queryCalled = true
		return "select 'secret'", 1
	}
	logger.Trace(context.Background(), time.Now().Add(-time.Second), query, errors.New("driver failure"))
	if queryCalled {
		t.Fatal("backend logger materialized SQL and bound values")
	}
}

func TestBackendExecSeparatesRowsAffectedFromLastInsertID(t *testing.T) {
	useSQLiteBackendRuntime(t)
	ctx := context.Background()
	database, err := OpenOrchestratorContext(ctx)
	if err != nil {
		t.Fatalf("OpenOrchestratorContext() error: %v", err)
	}
	if _, err := database.ExecContext(ctx, `create table write_fixture (id integer primary key autoincrement, name text)`); err != nil {
		t.Fatalf("create fixture: %v", err)
	}

	result, err := ExecOrchestratorContext(ctx, `insert into write_fixture (name) values (?)`, "gorm")
	if err != nil {
		t.Fatalf("ExecOrchestratorContext() error: %v", err)
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		t.Fatalf("RowsAffected() = %d, %v; want 1, nil", affected, err)
	}
	if _, err := result.LastInsertId(); !errors.Is(err, ErrLastInsertIDUnavailable) {
		t.Fatalf("LastInsertId() error = %v; want ErrLastInsertIDUnavailable", err)
	}

	result, err = ExecOrchestratorSQLContext(ctx, `insert into write_fixture (name) values (?)`, "database/sql")
	if err != nil {
		t.Fatalf("ExecOrchestratorSQLContext() error: %v", err)
	}
	if id, err := result.LastInsertId(); err != nil || id != 2 {
		t.Fatalf("LastInsertId() = %d, %v; want 2, nil", id, err)
	}
}

func TestExecOrchestratorGORMLocalizesDialectTranslationInsideTransaction(t *testing.T) {
	useSQLiteBackendRuntime(t)
	ctx := context.Background()
	database, err := OpenOrchestratorContext(ctx)
	if err != nil {
		t.Fatalf("OpenOrchestratorContext() error: %v", err)
	}
	if _, err := database.ExecContext(ctx, `create table transaction_fixture (name text, registered_at text)`); err != nil {
		t.Fatalf("create fixture: %v", err)
	}
	handle, err := OpenOrchestratorGORMContext(ctx)
	if err != nil {
		t.Fatalf("OpenOrchestratorGORMContext() error: %v", err)
	}
	if err := handle.Transaction(func(tx *gorm.DB) error {
		_, err := ExecOrchestratorGORM(tx, `insert into transaction_fixture (name, registered_at) values (?, now())`, "entry")
		return err
	}); err != nil {
		t.Fatalf("transactional backend write: %v", err)
	}
	var registeredAt string
	if err := database.QueryRowContext(ctx, `select registered_at from transaction_fixture where name = 'entry'`).Scan(&registeredAt); err != nil {
		t.Fatalf("read transaction fixture: %v", err)
	}
	if registeredAt == "" {
		t.Fatal("translated now() produced an empty timestamp")
	}
}
