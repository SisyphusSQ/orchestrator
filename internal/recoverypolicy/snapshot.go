package recoverypolicy

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"

	"github.com/openark/orchestrator/internal/models/domain"
	"github.com/openark/orchestrator/internal/repository/metadata"
)

// Snapshot holds exactly the policy and hooks used by one recovery. It is
// process-local execution data; commands and secrets must not enter API output.
type Snapshot struct {
	ClusterName    string
	Policy         domain.RecoveryPolicy
	Hooks          map[string][]domain.RecoveryHookProfile
	PolicyRevision int64
	HookRevision   int64
}

type executionKey struct{}

func Capture(ctx context.Context, clusterName string) (*Snapshot, error) {
	inputs, err := metadata.ReadRecoveryInputs(ctx, clusterName)
	if err != nil {
		return nil, fmt.Errorf("read recovery execution snapshot: %w", err)
	}
	return buildSnapshot(clusterName, inputs)
}

func buildSnapshot(clusterName string, inputs metadata.RecoveryInputs) (*Snapshot, error) {
	policy := Defaults()
	for _, row := range inputs.Policies {
		if row.ScopeType == domain.ScopeGlobal {
			policy = Apply(policy, row.Overrides)
		}
	}
	for _, row := range inputs.Policies {
		if inputs.Alias != "" && row.ScopeType == domain.ScopeCluster && row.ScopeKey == inputs.Alias {
			policy = Apply(policy, row.Overrides)
		}
	}
	if err := ValidatePolicy(policy); err != nil {
		return nil, err
	}
	profiles := map[string]domain.RecoveryHookProfile{}
	for _, profile := range inputs.Profiles {
		profiles[profile.ID] = profile
	}
	hooks := map[string][]domain.RecoveryHookProfile{}
	apply := func(assignments []domain.RecoveryHookAssignment) error {
		for _, assignment := range assignments {
			switch assignment.Mode {
			case "inherit":
				continue
			case "disable":
				hooks[assignment.Phase] = nil
			case "replace":
				list := []domain.RecoveryHookProfile{}
				for _, id := range assignment.ProfileIDs {
					profile, ok := profiles[id]
					if !ok {
						return fmt.Errorf("recovery hook profile %q is missing", id)
					}
					if profile.Enabled {
						list = append(list, profile)
					}
				}
				hooks[assignment.Phase] = list
			default:
				return fmt.Errorf("invalid hook assignment mode %q", assignment.Mode)
			}
		}
		return nil
	}
	if err := apply(inputs.GlobalHooks); err != nil {
		return nil, err
	}
	if err := apply(inputs.ClusterHooks); err != nil {
		return nil, err
	}
	// Own all nested data so later cache/fixture changes cannot alter execution.
	data, err := json.Marshal(&Snapshot{ClusterName: clusterName, Policy: policy, Hooks: hooks})
	if err != nil {
		return nil, err
	}
	var snapshot Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, err
	}
	fingerprint := func(value any) int64 {
		data, _ := json.Marshal(value)
		h := fnv.New64a()
		_, _ = h.Write(data)
		return int64(h.Sum64() & 0x7fffffffffffffff)
	}
	snapshot.PolicyRevision = fingerprint(snapshot.Policy)
	snapshot.HookRevision = fingerprint(snapshot.Hooks)
	return &snapshot, nil
}

func WithSnapshot(ctx context.Context, snapshot *Snapshot) context.Context {
	return context.WithValue(ctx, executionKey{}, snapshot)
}
func SnapshotFromContext(ctx context.Context) *Snapshot {
	snapshot, _ := ctx.Value(executionKey{}).(*Snapshot)
	return snapshot
}

func FromContext(ctx context.Context, clusterName string) domain.RecoveryPolicy {
	if snapshot := SnapshotFromContext(ctx); snapshot != nil {
		return snapshot.Policy
	}
	return Current(clusterName)
}
