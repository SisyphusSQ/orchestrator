package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func openDynamicSQLiteFixture(t *testing.T) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open SQLite fixture: %v", err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close SQLite fixture: %v", err)
		}
	})
	return database
}

func TestQueryDynamicRowsPreservesColumnsNullAndContext(t *testing.T) {
	database := openDynamicSQLiteFixture(t)
	ctx := context.Background()
	if _, err := database.ExecContext(ctx, `create table dynamic_fixture (id integer, value text)`); err != nil {
		t.Fatalf("create fixture: %v", err)
	}
	if _, err := database.ExecContext(ctx, `insert into dynamic_fixture values (7, null)`); err != nil {
		t.Fatalf("insert fixture: %v", err)
	}

	seen := false
	err := QueryDynamicRowsContext(ctx, database, `select id, value from dynamic_fixture`, func(row DynamicRow) error {
		seen = true
		if got := row.GetInt("id"); got != 7 {
			t.Fatalf("id = %d; want 7", got)
		}
		if cell := row["value"]; cell.Valid {
			t.Fatalf("NULL cell = %#v; want invalid", cell)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("QueryDynamicRowsContext() error: %v", err)
	}
	if !seen {
		t.Fatal("QueryDynamicRowsContext() did not visit the fixture row")
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := QueryDynamicRowsContext(canceled, database, `select id from dynamic_fixture`, func(DynamicRow) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled query error = %v; want context.Canceled", err)
	}
}

func TestNamedResultDataRoundTripsNullSnapshotCells(t *testing.T) {
	database := openDynamicSQLiteFixture(t)
	ctx := context.Background()
	if _, err := database.ExecContext(ctx, `create table source_table (id integer, value text)`); err != nil {
		t.Fatalf("create source: %v", err)
	}
	if _, err := database.ExecContext(ctx, `create table target_table (id integer, value text)`); err != nil {
		t.Fatalf("create target: %v", err)
	}
	if _, err := database.ExecContext(ctx, `insert into source_table values (1, null), (2, 'two')`); err != nil {
		t.Fatalf("insert source: %v", err)
	}

	data, err := ScanTableContext(ctx, database, "source_table")
	if err != nil {
		t.Fatalf("ScanTableContext() error: %v", err)
	}
	payload, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal snapshot data: %v", err)
	}
	var decoded NamedResultData
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal snapshot data: %v", err)
	}
	if err := WriteTableContext(ctx, database, "target_table", decoded); err != nil {
		t.Fatalf("WriteTableContext() error: %v", err)
	}

	var count, nulls int
	if err := database.QueryRowContext(ctx, `select count(*), sum(value is null) from target_table`).Scan(&count, &nulls); err != nil {
		t.Fatalf("read target: %v", err)
	}
	if count != 2 || nulls != 1 {
		t.Fatalf("target count/nulls = %d/%d; want 2/1", count, nulls)
	}
}

func TestSQLiteDialectTranslationStaysScopedToBackendSQL(t *testing.T) {
	got := ToSQLiteDialect(`insert ignore into sample(id, updated_at) values (?, now())`)
	want := `insert or ignore into sample(id, updated_at) values (?, datetime('now'))`
	if got != want {
		t.Fatalf("ToSQLiteDialect() = %q; want %q", got, want)
	}
	if !IsAlterTable(` alter table sample add column value int`) {
		t.Fatal("IsAlterTable() did not identify an ALTER TABLE statement")
	}
}
