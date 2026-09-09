package recoverypolicy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"sync"

	"github.com/openark/orchestrator/internal/db"
)

var ErrRevisionConflict = errors.New("recovery configuration revision conflict")

type policyRow struct {
	ScopeType    string `gorm:"column:scope_type"`
	ScopeKey     string `gorm:"column:scope_key"`
	PolicyJSON   string `gorm:"column:policy_json"`
	Revision     int64  `gorm:"column:revision"`
	UpdatedBy    string `gorm:"column:updated_by"`
	ChangeReason string `gorm:"column:change_reason"`
	UpdatedAt    string `gorm:"column:updated_at"`
}

type hookProfileRow struct {
	ID               string `gorm:"column:profile_id"`
	Name             string `gorm:"column:profile_name"`
	CommandsJSON     string `gorm:"column:commands_json"`
	TimeoutSeconds   int    `gorm:"column:timeout_seconds"`
	FailurePolicy    string `gorm:"column:failure_policy"`
	OutputLimitBytes int    `gorm:"column:output_limit_bytes"`
	Enabled          bool   `gorm:"column:enabled"`
	Revision         int64  `gorm:"column:revision"`
	UpdatedBy        string `gorm:"column:updated_by"`
	ChangeReason     string `gorm:"column:change_reason"`
	UpdatedAt        string `gorm:"column:updated_at"`
}

type hookAssignmentRow struct {
	ScopeType      string `gorm:"column:scope_type"`
	ScopeKey       string `gorm:"column:scope_key"`
	Phase          string `gorm:"column:phase"`
	Mode           string `gorm:"column:mode"`
	ProfileIDsJSON string `gorm:"column:profile_ids_json"`
	Revision       int64  `gorm:"column:revision"`
	UpdatedBy      string `gorm:"column:updated_by"`
	ChangeReason   string `gorm:"column:change_reason"`
	UpdatedAt      string `gorm:"column:updated_at"`
}

type cacheState struct {
	global   PolicyPatch
	clusters map[string]PolicyPatch
	aliases  map[string]string
}

var policyCache struct {
	sync.RWMutex
	loaded bool
	state  cacheState
}

func Invalidate() {
	policyCache.Lock()
	policyCache.loaded = false
	policyCache.Unlock()
}

func readPolicyRows(ctx context.Context) ([]policyRow, error) {
	return db.QueryOrchestratorRows[policyRow](ctx, `
		SELECT scope_type, scope_key, policy_json, revision, updated_by, change_reason, updated_at
		FROM recovery_policy`)
}

func loadCache(ctx context.Context) (cacheState, error) {
	rows, err := readPolicyRows(ctx)
	if err != nil {
		return cacheState{}, err
	}
	state := cacheState{clusters: map[string]PolicyPatch{}, aliases: map[string]string{}}
	for _, row := range rows {
		var patch PolicyPatch
		if err := json.Unmarshal([]byte(row.PolicyJSON), &patch); err != nil {
			return cacheState{}, fmt.Errorf("decode recovery policy %s/%s: %w", row.ScopeType, row.ScopeKey, err)
		}
		if row.ScopeType == ScopeGlobal {
			state.global = patch
		} else if row.ScopeType == ScopeCluster {
			state.clusters[row.ScopeKey] = patch
		}
	}
	type aliasRow struct {
		ClusterName string `gorm:"column:cluster_name"`
		Alias       string `gorm:"column:alias"`
	}
	aliases, err := db.QueryOrchestratorRows[aliasRow](ctx, `SELECT cluster_name, alias FROM cluster_alias_override WHERE alias <> ''`)
	if err != nil {
		return cacheState{}, err
	}
	for _, row := range aliases {
		state.aliases[row.ClusterName] = row.Alias
	}
	return state, nil
}

func ensureCache(ctx context.Context) (cacheState, error) {
	policyCache.RLock()
	if policyCache.loaded {
		state := policyCache.state
		policyCache.RUnlock()
		return state, nil
	}
	policyCache.RUnlock()
	state, err := loadCache(ctx)
	if err != nil {
		return cacheState{}, err
	}
	policyCache.Lock()
	policyCache.state = state
	policyCache.loaded = true
	policyCache.Unlock()
	return state, nil
}

// Current returns the last persisted effective policy. Before the backend is
// available it deliberately falls back to safe code defaults.
func Current(clusterName string) Policy {
	state, err := ensureCache(context.Background())
	if err != nil {
		return Defaults()
	}
	policy := Apply(Defaults(), state.global)
	if alias := state.aliases[clusterName]; alias != "" {
		policy = Apply(policy, state.clusters[alias])
	}
	return policy
}

func GetPolicy(ctx context.Context, scopeType, scopeKey string) (PolicyDocument, error) {
	if err := ValidateScope(scopeType, scopeKey); err != nil {
		return PolicyDocument{}, err
	}
	rows, err := db.QueryOrchestratorRows[policyRow](ctx, `
		SELECT scope_type, scope_key, policy_json, revision, updated_by, change_reason, updated_at
		FROM recovery_policy WHERE scope_type = ? AND scope_key = ?`, scopeType, scopeKey)
	if err != nil {
		return PolicyDocument{}, err
	}
	doc := PolicyDocument{ScopeType: scopeType, ScopeKey: scopeKey}
	if len(rows) > 0 {
		row := rows[0]
		if err := json.Unmarshal([]byte(row.PolicyJSON), &doc.Overrides); err != nil {
			return PolicyDocument{}, fmt.Errorf("decode recovery policy: %w", err)
		}
		doc.Revision, doc.UpdatedBy, doc.ChangeReason, doc.UpdatedAt = row.Revision, row.UpdatedBy, row.ChangeReason, row.UpdatedAt
	}
	base := Defaults()
	if scopeType == ScopeCluster {
		global, err := GetPolicy(ctx, ScopeGlobal, GlobalKey)
		if err != nil {
			return PolicyDocument{}, err
		}
		base = global.Effective
	}
	doc.Inherited = base
	doc.Effective = Apply(base, doc.Overrides)
	return doc, ValidatePolicy(doc.Effective)
}

func SavePolicy(ctx context.Context, command SavePolicyCommand) error {
	if err := ValidateScope(command.ScopeType, command.ScopeKey); err != nil {
		return err
	}
	base := Defaults()
	if command.ScopeType == ScopeCluster {
		if err := RequireExplicitAlias(ctx, command.ScopeKey); err != nil {
			return err
		}
		global, err := GetPolicy(ctx, ScopeGlobal, GlobalKey)
		if err != nil {
			return err
		}
		base = global.Effective
	}
	if err := ValidatePolicy(Apply(base, command.Overrides)); err != nil {
		return err
	}
	payload, err := json.Marshal(command.Overrides)
	if err != nil {
		return err
	}
	return inTransaction(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE recovery_policy SET policy_json = ?, revision = revision + 1, updated_by = ?, change_reason = ?, updated_at = CURRENT_TIMESTAMP WHERE scope_type = ? AND scope_key = ? AND revision = ?`, string(payload), command.UpdatedBy, command.ChangeReason, command.ScopeType, command.ScopeKey, command.ExpectedRevision)
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
		if command.ExpectedRevision != 0 {
			return ErrRevisionConflict
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO recovery_policy (scope_type, scope_key, policy_json, revision, updated_by, change_reason, updated_at) VALUES (?, ?, ?, 1, ?, ?, CURRENT_TIMESTAMP)`, command.ScopeType, command.ScopeKey, string(payload), command.UpdatedBy, command.ChangeReason)
		if err != nil {
			if db.IsDuplicateKeyError(err) {
				return ErrRevisionConflict
			}
			return fmt.Errorf("insert recovery policy: %w", err)
		}
		return nil
	})
}

func RequireExplicitAlias(ctx context.Context, alias string) error {
	type countRow struct {
		Count int `gorm:"column:alias_count"`
	}
	rows, err := db.QueryOrchestratorRows[countRow](ctx, `SELECT COUNT(*) AS alias_count FROM cluster_alias_override WHERE alias = ?`, alias)
	if err != nil {
		return err
	}
	count := 0
	if len(rows) == 1 {
		count = rows[0].Count
	}
	if count != 1 {
		return fmt.Errorf("cluster override requires one explicit unique alias; %q matched %d", alias, count)
	}
	return nil
}

// SetExplicitAlias moves cluster-scoped configuration in the same transaction
// as the manual alias change, preserving the binding across alias renames.
func SetExplicitAlias(ctx context.Context, clusterName, alias string) error {
	if clusterName == "" || alias == "" {
		return fmt.Errorf("cluster name and explicit alias are required")
	}
	return inTransaction(ctx, func(tx *sql.Tx) error {
		var oldAlias string
		err := tx.QueryRowContext(ctx, `SELECT alias FROM cluster_alias_override WHERE cluster_name = ?`, clusterName).Scan(&oldAlias)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		var ownerCount int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM cluster_alias_override WHERE alias = ? AND cluster_name <> ?`, alias, clusterName).Scan(&ownerCount); err != nil {
			return err
		}
		if ownerCount > 0 {
			return fmt.Errorf("explicit cluster alias %q is already in use", alias)
		}
		if oldAlias != "" && oldAlias != alias {
			if _, err := tx.ExecContext(ctx, `UPDATE recovery_policy SET scope_key = ? WHERE scope_type = ? AND scope_key = ?`, alias, ScopeCluster, oldAlias); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE recovery_hook_assignment SET scope_key = ? WHERE scope_type = ? AND scope_key = ?`, alias, ScopeCluster, oldAlias); err != nil {
				return err
			}
		}
		result, err := tx.ExecContext(ctx, `UPDATE cluster_alias_override SET alias = ? WHERE cluster_name = ?`, alias, clusterName)
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if changed == 0 {
			_, err = tx.ExecContext(ctx, `INSERT INTO cluster_alias_override (cluster_name, alias) VALUES (?, ?)`, clusterName, alias)
		}
		return err
	})
}

func ListHookProfiles(ctx context.Context) ([]HookProfile, error) {
	rows, err := db.QueryOrchestratorRows[hookProfileRow](ctx, `SELECT profile_id, profile_name, commands_json, timeout_seconds, failure_policy, output_limit_bytes, enabled, revision, updated_by, change_reason, updated_at FROM recovery_hook_profile ORDER BY profile_name, profile_id`)
	if err != nil {
		return nil, err
	}
	profiles := make([]HookProfile, 0, len(rows))
	for _, row := range rows {
		profile := HookProfile{ID: row.ID, Name: row.Name, TimeoutSeconds: row.TimeoutSeconds, FailurePolicy: row.FailurePolicy, OutputLimitBytes: row.OutputLimitBytes, Enabled: row.Enabled, Revision: row.Revision, UpdatedBy: row.UpdatedBy, ChangeReason: row.ChangeReason, UpdatedAt: row.UpdatedAt}
		if err := json.Unmarshal([]byte(row.CommandsJSON), &profile.Commands); err != nil {
			return nil, fmt.Errorf("decode hook profile %s: %w", row.ID, err)
		}
		profiles = append(profiles, profile)
	}
	return profiles, nil
}

func SaveHookProfile(ctx context.Context, command SaveHookProfileCommand) error {
	if err := ValidateHookProfile(command.Profile); err != nil {
		return err
	}
	commands, err := json.Marshal(command.Profile.Commands)
	if err != nil {
		return err
	}
	p := command.Profile
	return inTransaction(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE recovery_hook_profile SET profile_name = ?, commands_json = ?, timeout_seconds = ?, failure_policy = ?, output_limit_bytes = ?, enabled = ?, revision = revision + 1, updated_by = ?, change_reason = ?, updated_at = CURRENT_TIMESTAMP WHERE profile_id = ? AND revision = ?`, p.Name, string(commands), p.TimeoutSeconds, p.FailurePolicy, p.OutputLimitBytes, p.Enabled, p.UpdatedBy, p.ChangeReason, p.ID, command.ExpectedRevision)
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
		if command.ExpectedRevision != 0 {
			return ErrRevisionConflict
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO recovery_hook_profile (profile_id, profile_name, commands_json, timeout_seconds, failure_policy, output_limit_bytes, enabled, revision, updated_by, change_reason, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?, ?, CURRENT_TIMESTAMP)`, p.ID, p.Name, string(commands), p.TimeoutSeconds, p.FailurePolicy, p.OutputLimitBytes, p.Enabled, p.UpdatedBy, p.ChangeReason)
		if db.IsDuplicateKeyError(err) {
			return ErrRevisionConflict
		}
		return err
	})
}

func ListHookAssignments(ctx context.Context, scopeType, scopeKey string) ([]HookAssignment, error) {
	if err := ValidateScope(scopeType, scopeKey); err != nil {
		return nil, err
	}
	rows, err := db.QueryOrchestratorRows[hookAssignmentRow](ctx, `SELECT scope_type, scope_key, phase, mode, profile_ids_json, revision, updated_by, change_reason, updated_at FROM recovery_hook_assignment WHERE scope_type = ? AND scope_key = ? ORDER BY phase`, scopeType, scopeKey)
	if err != nil {
		return nil, err
	}
	assignments := make([]HookAssignment, 0, len(rows))
	for _, row := range rows {
		a := HookAssignment{ScopeType: row.ScopeType, ScopeKey: row.ScopeKey, Phase: row.Phase, Mode: row.Mode, Revision: row.Revision, UpdatedBy: row.UpdatedBy, ChangeReason: row.ChangeReason, UpdatedAt: row.UpdatedAt}
		if err := json.Unmarshal([]byte(row.ProfileIDsJSON), &a.ProfileIDs); err != nil {
			return nil, fmt.Errorf("decode hook assignment %s: %w", row.Phase, err)
		}
		assignments = append(assignments, a)
	}
	return assignments, nil
}

func SaveHookAssignment(ctx context.Context, command SaveHookAssignmentCommand) error {
	a := command.Assignment
	if err := ValidateHookAssignment(a); err != nil {
		return err
	}
	if a.Mode == "replace" {
		profiles, err := ListHookProfiles(ctx)
		if err != nil {
			return err
		}
		known := make(map[string]bool, len(profiles))
		for _, profile := range profiles {
			known[profile.ID] = profile.Enabled
		}
		for _, id := range a.ProfileIDs {
			if !known[id] {
				return fmt.Errorf("enabled hook profile %q does not exist", id)
			}
		}
	}
	if a.ScopeType == ScopeCluster {
		if err := RequireExplicitAlias(ctx, a.ScopeKey); err != nil {
			return err
		}
	}
	profileIDs, err := json.Marshal(a.ProfileIDs)
	if err != nil {
		return err
	}
	return inTransaction(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE recovery_hook_assignment SET mode = ?, profile_ids_json = ?, revision = revision + 1, updated_by = ?, change_reason = ?, updated_at = CURRENT_TIMESTAMP WHERE scope_type = ? AND scope_key = ? AND phase = ? AND revision = ?`, a.Mode, string(profileIDs), a.UpdatedBy, a.ChangeReason, a.ScopeType, a.ScopeKey, a.Phase, command.ExpectedRevision)
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
		if command.ExpectedRevision != 0 {
			return ErrRevisionConflict
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO recovery_hook_assignment (scope_type, scope_key, phase, mode, profile_ids_json, revision, updated_by, change_reason, updated_at) VALUES (?, ?, ?, ?, ?, 1, ?, ?, CURRENT_TIMESTAMP)`, a.ScopeType, a.ScopeKey, a.Phase, a.Mode, string(profileIDs), a.UpdatedBy, a.ChangeReason)
		if db.IsDuplicateKeyError(err) {
			return ErrRevisionConflict
		}
		return err
	})
}

func EffectiveHooks(ctx context.Context, clusterName string) (map[string][]HookProfile, map[string]string, error) {
	profiles, err := ListHookProfiles(ctx)
	if err != nil {
		return nil, nil, err
	}
	profileByID := make(map[string]HookProfile, len(profiles))
	for _, p := range profiles {
		if p.Enabled {
			profileByID[p.ID] = p
		}
	}
	global, err := ListHookAssignments(ctx, ScopeGlobal, GlobalKey)
	if err != nil {
		return nil, nil, err
	}
	resolved := map[string][]HookProfile{}
	modes := map[string]string{}
	apply := func(assignments []HookAssignment) {
		for _, a := range assignments {
			modes[a.Phase] = a.Mode
			switch a.Mode {
			case "disable":
				resolved[a.Phase] = nil
			case "replace":
				list := make([]HookProfile, 0, len(a.ProfileIDs))
				for _, id := range a.ProfileIDs {
					if p, ok := profileByID[id]; ok {
						list = append(list, p)
					}
				}
				resolved[a.Phase] = list
			}
		}
	}
	apply(global)
	type aliasRow struct {
		Alias string `gorm:"column:alias"`
	}
	rows, err := db.QueryOrchestratorRows[aliasRow](ctx, `SELECT alias FROM cluster_alias_override WHERE cluster_name = ? AND alias <> ''`, clusterName)
	if err != nil {
		return nil, nil, err
	}
	if len(rows) == 1 {
		cluster, err := ListHookAssignments(ctx, ScopeCluster, rows[0].Alias)
		if err != nil {
			return nil, nil, err
		}
		for _, a := range cluster {
			if a.Mode != "inherit" {
				apply([]HookAssignment{a})
			}
		}
	}
	return resolved, modes, nil
}

// Revisions returns deterministic fingerprints of the policy and hook inputs
// selected for a recovery, so historical records can identify what was used.
func Revisions(ctx context.Context, clusterName string) (int64, int64, error) {
	state, err := loadCache(ctx)
	if err != nil {
		return 0, 0, err
	}
	policyHash, hookHash := fnv.New64a(), fnv.New64a()
	write := func(hash interface{ Write([]byte) (int, error) }, value interface{}) {
		payload, _ := json.Marshal(value)
		_, _ = hash.Write(payload)
	}
	write(policyHash, state.global)
	alias := state.aliases[clusterName]
	if alias != "" {
		write(policyHash, state.clusters[alias])
	}
	hooks, modes, err := EffectiveHooks(ctx, clusterName)
	if err != nil {
		return 0, 0, err
	}
	write(hookHash, modes)
	write(hookHash, hooks)
	return int64(policyHash.Sum64() & 0x7fffffffffffffff), int64(hookHash.Sum64() & 0x7fffffffffffffff), nil
}

func inTransaction(ctx context.Context, fn func(*sql.Tx) error) error {
	database, err := db.OpenOrchestratorContext(ctx)
	if err != nil {
		return err
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	Invalidate()
	return nil
}
