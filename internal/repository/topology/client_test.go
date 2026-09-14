package topology

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestClientConvenienceMethodsUseBoundContext(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithCancel(context.Background())
	client := &Client{db: db, ctx: ctx}
	var value int
	if err := client.Read("select 1", &value); err != nil {
		t.Fatal(err)
	}
	cancel()
	checks := map[string]func() error{
		"read":    func() error { return client.Read("select 1", &value) },
		"row":     func() error { return client.ReadRow("select 1").Decode(&value) },
		"args":    func() error { return client.ReadArgs("select ?", []any{1}, &value) },
		"execute": func() error { return client.Execute("create table should_not_exist(id integer)") },
	}
	for name, run := range checks {
		t.Run(name, func(t *testing.T) {
			if err := run(); !errors.Is(err, context.Canceled) {
				t.Fatalf("expected cancellation, got %v", err)
			}
		})
	}
}
