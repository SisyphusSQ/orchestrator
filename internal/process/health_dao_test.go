package process

import (
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestHealthyHTTPTokenQueryIsExecutable(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open SQLite fixture: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close SQLite fixture: %v", err)
		}
	})
	if _, err := database.Exec(`create table node_health (token text, extra_info text)`); err != nil {
		t.Fatalf("create fixture: %v", err)
	}
	if _, err := database.Exec(`insert into node_health (token, extra_info) values ('token-a', 'http')`); err != nil {
		t.Fatalf("insert fixture: %v", err)
	}

	var token string
	if err := database.QueryRow(healthyHTTPTokenQuery, "token-a", "http").Scan(&token); err != nil {
		t.Fatalf("healthyHTTPTokenQuery error: %v", err)
	}
	if token != "token-a" {
		t.Fatalf("token = %q; want token-a", token)
	}
}
