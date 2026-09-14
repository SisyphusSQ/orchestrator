package database

import (
	"context"
	"database/sql"

	"github.com/openark/orchestrator/internal/config"
	"gorm.io/gorm"
)

type readSnapshotKey struct{}

// WithReadSnapshot keeps related repository reads on the same database snapshot.
// The transaction never escapes the callback or owns the shared connection pool.
func WithReadSnapshot(ctx context.Context, read func(context.Context) error) error {
	handle, err := OpenOrchestratorGORMContext(ctx)
	if err != nil {
		return err
	}
	options := &sql.TxOptions{ReadOnly: true}
	if !config.FromContext(ctx).IsSQLite() {
		options.Isolation = sql.LevelRepeatableRead
	}
	return handle.Transaction(func(tx *gorm.DB) error {
		return read(context.WithValue(ctx, readSnapshotKey{}, tx))
	}, options)
}
