package metadata

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	modeldo "github.com/openark/orchestrator/internal/models/do"
	"github.com/openark/orchestrator/internal/models/domain"
	"github.com/openark/orchestrator/internal/repository/database"
)

// ErrRevisionConflict indicates an optimistic recovery configuration update lost a race.
var ErrRevisionConflict = errors.New("recovery configuration revision conflict")

// ReadRecoveryPolicies reads every persisted recovery policy.
func ReadRecoveryPolicies(ctx context.Context) ([]domain.RecoveryPolicyRecord, error) {
	rows, err := database.QueryOrchestratorRows[modeldo.RecoveryPolicy](ctx, `
		select scope_type, scope_key, policy_json, revision, updated_by, change_reason, updated_at
		from recovery_policy
	`)
	return recoveryPoliciesFromRows(rows, err)
}

// ReadRecoveryPolicy reads one persisted recovery policy.
func ReadRecoveryPolicy(ctx context.Context, scopeType, scopeKey string) ([]domain.RecoveryPolicyRecord, error) {
	rows, err := database.QueryOrchestratorRows[modeldo.RecoveryPolicy](ctx, `
		select scope_type, scope_key, policy_json, revision, updated_by, change_reason, updated_at
		from recovery_policy
		where scope_type = ? and scope_key = ?
	`, scopeType, scopeKey)
	return recoveryPoliciesFromRows(rows, err)
}

func recoveryPoliciesFromRows(rows []modeldo.RecoveryPolicy, queryErr error) ([]domain.RecoveryPolicyRecord, error) {
	if queryErr != nil {
		return nil, queryErr
	}
	result := make([]domain.RecoveryPolicyRecord, 0, len(rows))
	for _, row := range rows {
		record := domain.RecoveryPolicyRecord{
			ScopeType:    row.ScopeType,
			ScopeKey:     row.ScopeKey,
			Revision:     row.Revision,
			UpdatedBy:    row.UpdatedBy,
			ChangeReason: row.ChangeReason,
			UpdatedAt:    row.UpdatedAt,
		}
		if err := json.Unmarshal([]byte(row.PolicyJSON), &record.Overrides); err != nil {
			return nil, fmt.Errorf("decode recovery policy %s/%s: %w", row.ScopeType, row.ScopeKey, err)
		}
		result = append(result, record)
	}
	return result, nil
}

// ReadExplicitClusterAliases reads every non-empty explicit alias.
func ReadExplicitClusterAliases(ctx context.Context) ([]domain.ClusterAlias, error) {
	rows, err := database.QueryOrchestratorRows[modeldo.ClusterAlias](ctx, `
		select cluster_name, alias
		from cluster_alias_override
		where alias <> ''
	`)
	result := make([]domain.ClusterAlias, 0, len(rows))
	for _, row := range rows {
		result = append(result, domain.ClusterAlias{ClusterName: row.ClusterName, Alias: row.Alias})
	}
	return result, err
}

// CountExplicitClusterAlias counts owners of an explicit alias.
func CountExplicitClusterAlias(ctx context.Context, alias string) (int, error) {
	rows, err := database.QueryOrchestratorRows[modeldo.RowCount](ctx, `
		select count(*) as row_count
		from cluster_alias_override
		where alias = ?
	`, alias)
	if err != nil || len(rows) == 0 {
		return 0, err
	}
	return rows[0].Count, nil
}

// ReadExplicitClusterAlias reads the non-empty alias for one cluster.
func ReadExplicitClusterAlias(ctx context.Context, clusterName string) ([]domain.ClusterAlias, error) {
	rows, err := database.QueryOrchestratorRows[modeldo.ClusterAlias](ctx, `
		select cluster_name, alias
		from cluster_alias_override
		where cluster_name = ? and alias <> ''
	`, clusterName)
	result := make([]domain.ClusterAlias, 0, len(rows))
	for _, row := range rows {
		result = append(result, domain.ClusterAlias{ClusterName: row.ClusterName, Alias: row.Alias})
	}
	return result, err
}

// SaveRecoveryPolicy performs optimistic update-or-create in one transaction.
func SaveRecoveryPolicy(ctx context.Context, record domain.RecoveryPolicyRecord, expectedRevision int64) error {
	payload, err := json.Marshal(record.Overrides)
	if err != nil {
		return err
	}
	return inTransaction(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `
			update recovery_policy
			set policy_json = ?, revision = revision + 1, updated_by = ?, change_reason = ?, updated_at = current_timestamp
			where scope_type = ? and scope_key = ? and revision = ?
		`, string(payload), record.UpdatedBy, record.ChangeReason, record.ScopeType, record.ScopeKey, expectedRevision)
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if changed == 1 {
			return nil
		}
		if expectedRevision != 0 {
			return ErrRevisionConflict
		}
		_, err = tx.ExecContext(ctx, `
			insert into recovery_policy (
				scope_type, scope_key, policy_json, revision, updated_by, change_reason, updated_at
			) values (?, ?, ?, 1, ?, ?, current_timestamp)
		`, record.ScopeType, record.ScopeKey, string(payload), record.UpdatedBy, record.ChangeReason)
		if database.IsDuplicateKeyError(err) {
			return ErrRevisionConflict
		}
		if err != nil {
			return fmt.Errorf("insert recovery policy: %w", err)
		}
		return nil
	})
}

// SetExplicitClusterAlias changes an alias and moves its scoped configuration atomically.
func SetExplicitClusterAlias(ctx context.Context, clusterName, alias, clusterScope string) error {
	return inTransaction(ctx, func(tx *sql.Tx) error {
		var oldAlias string
		err := tx.QueryRowContext(ctx, `
			select alias from cluster_alias_override where cluster_name = ?
		`, clusterName).Scan(&oldAlias)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}

		var ownerCount int
		if err := tx.QueryRowContext(ctx, `
			select count(*) from cluster_alias_override where alias = ? and cluster_name <> ?
		`, alias, clusterName).Scan(&ownerCount); err != nil {
			return err
		}
		if ownerCount > 0 {
			return fmt.Errorf("explicit cluster alias %q is already in use", alias)
		}

		if oldAlias != "" && oldAlias != alias {
			if _, err := tx.ExecContext(ctx, `
				update recovery_policy set scope_key = ? where scope_type = ? and scope_key = ?
			`, alias, clusterScope, oldAlias); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `
				update recovery_hook_assignment set scope_key = ? where scope_type = ? and scope_key = ?
			`, alias, clusterScope, oldAlias); err != nil {
				return err
			}
		}

		result, err := tx.ExecContext(ctx, `
			update cluster_alias_override set alias = ? where cluster_name = ?
		`, alias, clusterName)
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if changed == 0 {
			_, err = tx.ExecContext(ctx, `
				insert into cluster_alias_override (cluster_name, alias) values (?, ?)
			`, clusterName, alias)
		}
		return err
	})
}

// ReadRecoveryHookProfiles reads every hook profile.
func ReadRecoveryHookProfiles(ctx context.Context) ([]domain.RecoveryHookProfile, error) {
	rows, err := database.QueryOrchestratorRows[modeldo.RecoveryHookProfile](ctx, `
		select profile_id, profile_name, commands_json, timeout_seconds, failure_policy,
			output_limit_bytes, enabled, revision, updated_by, change_reason, updated_at
		from recovery_hook_profile
		order by profile_name, profile_id
	`)
	if err != nil {
		return nil, err
	}
	result := make([]domain.RecoveryHookProfile, 0, len(rows))
	for _, row := range rows {
		profile := domain.RecoveryHookProfile{
			ID:               row.ID,
			Name:             row.Name,
			TimeoutSeconds:   row.TimeoutSeconds,
			FailurePolicy:    row.FailurePolicy,
			OutputLimitBytes: row.OutputLimitBytes,
			Enabled:          row.Enabled,
			Revision:         row.Revision,
			UpdatedBy:        row.UpdatedBy,
			ChangeReason:     row.ChangeReason,
			UpdatedAt:        row.UpdatedAt,
		}
		if err := json.Unmarshal([]byte(row.CommandsJSON), &profile.Commands); err != nil {
			return nil, fmt.Errorf("decode hook profile %s: %w", row.ID, err)
		}
		result = append(result, profile)
	}
	return result, nil
}

// SaveRecoveryHookProfile performs optimistic update-or-create in one transaction.
func SaveRecoveryHookProfile(ctx context.Context, profile domain.RecoveryHookProfile, expectedRevision int64) error {
	commands, err := json.Marshal(profile.Commands)
	if err != nil {
		return err
	}
	return inTransaction(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `
			update recovery_hook_profile
			set profile_name = ?, commands_json = ?, timeout_seconds = ?, failure_policy = ?,
				output_limit_bytes = ?, enabled = ?, revision = revision + 1,
				updated_by = ?, change_reason = ?, updated_at = current_timestamp
			where profile_id = ? and revision = ?
		`, profile.Name, string(commands), profile.TimeoutSeconds, profile.FailurePolicy, profile.OutputLimitBytes,
			profile.Enabled, profile.UpdatedBy, profile.ChangeReason, profile.ID, expectedRevision)
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if changed == 1 {
			return nil
		}
		if expectedRevision != 0 {
			return ErrRevisionConflict
		}
		_, err = tx.ExecContext(ctx, `
			insert into recovery_hook_profile (
				profile_id, profile_name, commands_json, timeout_seconds, failure_policy,
				output_limit_bytes, enabled, revision, updated_by, change_reason, updated_at
			) values (?, ?, ?, ?, ?, ?, ?, 1, ?, ?, current_timestamp)
		`, profile.ID, profile.Name, string(commands), profile.TimeoutSeconds, profile.FailurePolicy,
			profile.OutputLimitBytes, profile.Enabled, profile.UpdatedBy, profile.ChangeReason)
		if database.IsDuplicateKeyError(err) {
			return ErrRevisionConflict
		}
		return err
	})
}

// ReadRecoveryHookAssignments reads hook assignments for one scope.
func ReadRecoveryHookAssignments(ctx context.Context, scopeType, scopeKey string) ([]domain.RecoveryHookAssignment, error) {
	rows, err := database.QueryOrchestratorRows[modeldo.RecoveryHookAssignment](ctx, `
		select scope_type, scope_key, phase, mode, profile_ids_json, revision, updated_by, change_reason, updated_at
		from recovery_hook_assignment
		where scope_type = ? and scope_key = ?
		order by phase
	`, scopeType, scopeKey)
	if err != nil {
		return nil, err
	}
	result := make([]domain.RecoveryHookAssignment, 0, len(rows))
	for _, row := range rows {
		assignment := domain.RecoveryHookAssignment{
			ScopeType:    row.ScopeType,
			ScopeKey:     row.ScopeKey,
			Phase:        row.Phase,
			Mode:         row.Mode,
			Revision:     row.Revision,
			UpdatedBy:    row.UpdatedBy,
			ChangeReason: row.ChangeReason,
			UpdatedAt:    row.UpdatedAt,
		}
		if err := json.Unmarshal([]byte(row.ProfileIDsJSON), &assignment.ProfileIDs); err != nil {
			return nil, fmt.Errorf("decode hook assignment %s: %w", row.Phase, err)
		}
		result = append(result, assignment)
	}
	return result, nil
}

// SaveRecoveryHookAssignment performs optimistic update-or-create in one transaction.
func SaveRecoveryHookAssignment(ctx context.Context, assignment domain.RecoveryHookAssignment, expectedRevision int64) error {
	profileIDs, err := json.Marshal(assignment.ProfileIDs)
	if err != nil {
		return err
	}
	return inTransaction(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `
			update recovery_hook_assignment
			set mode = ?, profile_ids_json = ?, revision = revision + 1,
				updated_by = ?, change_reason = ?, updated_at = current_timestamp
			where scope_type = ? and scope_key = ? and phase = ? and revision = ?
		`, assignment.Mode, string(profileIDs), assignment.UpdatedBy, assignment.ChangeReason,
			assignment.ScopeType, assignment.ScopeKey, assignment.Phase, expectedRevision)
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if changed == 1 {
			return nil
		}
		if expectedRevision != 0 {
			return ErrRevisionConflict
		}
		_, err = tx.ExecContext(ctx, `
			insert into recovery_hook_assignment (
				scope_type, scope_key, phase, mode, profile_ids_json, revision, updated_by, change_reason, updated_at
			) values (?, ?, ?, ?, ?, 1, ?, ?, current_timestamp)
		`, assignment.ScopeType, assignment.ScopeKey, assignment.Phase, assignment.Mode, string(profileIDs), assignment.UpdatedBy, assignment.ChangeReason)
		if database.IsDuplicateKeyError(err) {
			return ErrRevisionConflict
		}
		return err
	})
}

func inTransaction(ctx context.Context, fn func(*sql.Tx) error) error {
	pool, err := database.OpenOrchestratorContext(ctx)
	if err != nil {
		return err
	}
	tx, err := pool.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
