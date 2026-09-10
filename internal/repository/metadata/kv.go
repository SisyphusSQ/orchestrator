package metadata

import (
	"context"

	modeldo "github.com/openark/orchestrator/internal/models/do"
	"github.com/openark/orchestrator/internal/repository/database"
)

// PutKeyValue stores an internal relational key/value pair.
func PutKeyValue(ctx context.Context, key, value string) error {
	_, err := database.ExecOrchestratorContext(ctx, `
		replace into kv_store (
			store_key, store_value, last_updated
		) values (
			?, ?, now()
		)
	`, key, value)
	return err
}

// ReadKeyValue reads an internal relational key/value pair.
func ReadKeyValue(ctx context.Context, key string) (string, bool, error) {
	rows, err := database.QueryOrchestratorRows[modeldo.KVStoreEntry](ctx, `
		select store_value
		from kv_store
		where store_key = ?
	`, key)
	if err != nil || len(rows) == 0 {
		return "", false, err
	}
	return rows[0].Value, true, nil
}
