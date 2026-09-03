package sqlutils

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func TestGetSQLiteDBDoesNotCacheConnections(t *testing.T) {
	first, firstCached, err := GetSQLiteDB(":memory:", nil)
	if err != nil {
		t.Fatalf("first GetSQLiteDB() error: %v", err)
	}
	t.Cleanup(func() {
		if err := first.Close(); err != nil {
			t.Errorf("close first database: %v", err)
		}
	})
	second, secondCached, err := GetSQLiteDB(":memory:", nil)
	if err != nil {
		t.Fatalf("second GetSQLiteDB() error: %v", err)
	}
	t.Cleanup(func() {
		if second != first {
			if err := second.Close(); err != nil {
				t.Errorf("close second database: %v", err)
			}
		}
	})

	if firstCached || secondCached {
		t.Fatalf("GetSQLiteDB() cached flags = %t/%t; want false/false", firstCached, secondCached)
	}
	if first == second {
		t.Fatal("GetSQLiteDB() returned the same process-global connection pool")
	}
}

func TestExecNoPrepareContextPropagatesCancellation(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open SQLite fixture: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close SQLite fixture: %v", err)
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := ExecNoPrepareContext(ctx, database, "select 1"); !errors.Is(err, context.Canceled) {
		t.Fatalf("ExecNoPrepareContext() error = %v; want context.Canceled", err)
	}
}
