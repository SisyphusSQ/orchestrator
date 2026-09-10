package metadata

import (
	"context"

	modeldo "github.com/openark/orchestrator/internal/models/do"
	"github.com/openark/orchestrator/internal/repository/database"
)

// RecoveryDisabled reports whether the global recovery brake is enabled.
func RecoveryDisabled(ctx context.Context) (bool, error) {
	rows, err := database.QueryOrchestratorRows[modeldo.RecoveryDisabled](ctx, `
		select count(*) as disabled_count
		from global_recovery_disable
		where disable_recovery = ?
	`, 1)
	if err != nil || len(rows) == 0 {
		return false, err
	}
	return rows[0].Count > 0, nil
}

// SetRecoveryDisabled changes the global recovery brake.
func SetRecoveryDisabled(ctx context.Context, disabled bool) error {
	query := `delete from global_recovery_disable where disable_recovery >= 0`
	if disabled {
		query = `insert ignore into global_recovery_disable (disable_recovery) values (1)`
	}
	_, err := database.ExecOrchestratorContext(ctx, query)
	return err
}
