package recoverypolicy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"sync"

	"github.com/openark/orchestrator/internal/models/domain"
	"github.com/openark/orchestrator/internal/models/dto"
	"github.com/openark/orchestrator/internal/models/vo"
	"github.com/openark/orchestrator/internal/repository/metadata"
)

// IsRevisionConflict reports whether a recovery-configuration write lost its
// optimistic concurrency race.
func IsRevisionConflict(err error) bool {
	return errors.Is(err, metadata.ErrRevisionConflict)
}

type cacheState struct {
	global   domain.RecoveryPolicyPatch
	clusters map[string]domain.RecoveryPolicyPatch
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

func readPolicyRows(ctx context.Context) ([]domain.RecoveryPolicyRecord, error) {
	return metadata.ReadRecoveryPolicies(ctx)
}

func loadCache(ctx context.Context) (cacheState, error) {
	rows, err := readPolicyRows(ctx)
	if err != nil {
		return cacheState{}, err
	}
	state := cacheState{clusters: map[string]domain.RecoveryPolicyPatch{}, aliases: map[string]string{}}
	for _, row := range rows {
		if row.ScopeType == domain.ScopeGlobal {
			state.global = row.Overrides
		} else if row.ScopeType == domain.ScopeCluster {
			state.clusters[row.ScopeKey] = row.Overrides
		}
	}
	aliases, err := metadata.ReadExplicitClusterAliases(ctx)
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
func Current(clusterName string) domain.RecoveryPolicy {
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

func GetPolicy(ctx context.Context, scopeType, scopeKey string) (vo.RecoveryPolicyDocument, error) {
	if err := ValidateScope(scopeType, scopeKey); err != nil {
		return vo.RecoveryPolicyDocument{}, err
	}
	rows, err := metadata.ReadRecoveryPolicy(ctx, scopeType, scopeKey)
	if err != nil {
		return vo.RecoveryPolicyDocument{}, err
	}
	doc := vo.RecoveryPolicyDocument{ScopeType: scopeType, ScopeKey: scopeKey}
	if len(rows) > 0 {
		row := rows[0]
		doc.Overrides = row.Overrides
		doc.Revision, doc.UpdatedBy, doc.ChangeReason, doc.UpdatedAt = row.Revision, row.UpdatedBy, row.ChangeReason, row.UpdatedAt
	}
	base := Defaults()
	if scopeType == domain.ScopeCluster {
		global, err := GetPolicy(ctx, domain.ScopeGlobal, domain.GlobalKey)
		if err != nil {
			return vo.RecoveryPolicyDocument{}, err
		}
		base = global.Effective
	}
	doc.Inherited = base
	doc.Effective = Apply(base, doc.Overrides)
	return doc, ValidatePolicy(doc.Effective)
}

func SavePolicy(ctx context.Context, command dto.SaveRecoveryPolicyCommand) error {
	if err := ValidateScope(command.ScopeType, command.ScopeKey); err != nil {
		return err
	}
	base := Defaults()
	if command.ScopeType == domain.ScopeCluster {
		if err := RequireExplicitAlias(ctx, command.ScopeKey); err != nil {
			return err
		}
		global, err := GetPolicy(ctx, domain.ScopeGlobal, domain.GlobalKey)
		if err != nil {
			return err
		}
		base = global.Effective
	}
	if err := ValidatePolicy(Apply(base, command.Overrides)); err != nil {
		return err
	}
	err := metadata.SaveRecoveryPolicy(ctx, domain.RecoveryPolicyRecord{
		ScopeType:    command.ScopeType,
		ScopeKey:     command.ScopeKey,
		Overrides:    command.Overrides,
		UpdatedBy:    command.UpdatedBy,
		ChangeReason: command.ChangeReason,
	}, command.ExpectedRevision)
	return invalidateAfter(err)
}

func RequireExplicitAlias(ctx context.Context, alias string) error {
	count, err := metadata.CountExplicitClusterAlias(ctx, alias)
	if err != nil {
		return err
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
	return invalidateAfter(metadata.SetExplicitClusterAlias(ctx, clusterName, alias, domain.ScopeCluster))
}

func ListHookProfiles(ctx context.Context) ([]domain.RecoveryHookProfile, error) {
	return metadata.ReadRecoveryHookProfiles(ctx)
}

func SaveHookProfile(ctx context.Context, command dto.SaveRecoveryHookProfileCommand) error {
	if err := ValidateHookProfile(command.Profile); err != nil {
		return err
	}
	err := metadata.SaveRecoveryHookProfile(ctx, command.Profile, command.ExpectedRevision)
	return invalidateAfter(err)
}

func ListHookAssignments(ctx context.Context, scopeType, scopeKey string) ([]domain.RecoveryHookAssignment, error) {
	if err := ValidateScope(scopeType, scopeKey); err != nil {
		return nil, err
	}
	return metadata.ReadRecoveryHookAssignments(ctx, scopeType, scopeKey)
}

func SaveHookAssignment(ctx context.Context, command dto.SaveRecoveryHookAssignmentCommand) error {
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
	if a.ScopeType == domain.ScopeCluster {
		if err := RequireExplicitAlias(ctx, a.ScopeKey); err != nil {
			return err
		}
	}
	err := metadata.SaveRecoveryHookAssignment(ctx, a, command.ExpectedRevision)
	return invalidateAfter(err)
}

func EffectiveHooks(ctx context.Context, clusterName string) (map[string][]domain.RecoveryHookProfile, map[string]string, error) {
	profiles, err := ListHookProfiles(ctx)
	if err != nil {
		return nil, nil, err
	}
	profileByID := make(map[string]domain.RecoveryHookProfile, len(profiles))
	for _, p := range profiles {
		if p.Enabled {
			profileByID[p.ID] = p
		}
	}
	global, err := ListHookAssignments(ctx, domain.ScopeGlobal, domain.GlobalKey)
	if err != nil {
		return nil, nil, err
	}
	resolved := map[string][]domain.RecoveryHookProfile{}
	modes := map[string]string{}
	apply := func(assignments []domain.RecoveryHookAssignment) {
		for _, a := range assignments {
			modes[a.Phase] = a.Mode
			switch a.Mode {
			case "disable":
				resolved[a.Phase] = nil
			case "replace":
				list := make([]domain.RecoveryHookProfile, 0, len(a.ProfileIDs))
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
	rows, err := metadata.ReadExplicitClusterAlias(ctx, clusterName)
	if err != nil {
		return nil, nil, err
	}
	if len(rows) == 1 {
		cluster, err := ListHookAssignments(ctx, domain.ScopeCluster, rows[0].Alias)
		if err != nil {
			return nil, nil, err
		}
		for _, a := range cluster {
			if a.Mode != "inherit" {
				apply([]domain.RecoveryHookAssignment{a})
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
	write := func(hash interface{ Write([]byte) (int, error) }, value any) {
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

func invalidateAfter(err error) error {
	if err == nil {
		Invalidate()
	}
	return err
}
