package metadata

import (
	"context"

	"github.com/openark/orchestrator/internal/models/domain"
	"github.com/openark/orchestrator/internal/repository/database"
)

type RecoveryInputs struct {
	Policies     []domain.RecoveryPolicyRecord
	Alias        string
	Profiles     []domain.RecoveryHookProfile
	GlobalHooks  []domain.RecoveryHookAssignment
	ClusterHooks []domain.RecoveryHookAssignment
}

// ReadRecoveryInputs atomically reads all inputs needed for one recovery.
func ReadRecoveryInputs(ctx context.Context, clusterName string) (inputs RecoveryInputs, err error) {
	err = database.WithReadSnapshot(ctx, func(ctx context.Context) error {
		var err error
		inputs.Policies, err = ReadRecoveryPolicies(ctx)
		if err != nil {
			return err
		}
		aliases, err := ReadExplicitClusterAlias(ctx, clusterName)
		if err != nil {
			return err
		}
		if len(aliases) == 1 {
			inputs.Alias = aliases[0].Alias
		}
		inputs.Profiles, err = ReadRecoveryHookProfiles(ctx)
		if err != nil {
			return err
		}
		inputs.GlobalHooks, err = ReadRecoveryHookAssignments(ctx, domain.ScopeGlobal, domain.GlobalKey)
		if err != nil {
			return err
		}
		if inputs.Alias != "" {
			inputs.ClusterHooks, err = ReadRecoveryHookAssignments(ctx, domain.ScopeCluster, inputs.Alias)
		}
		return err
	})
	return inputs, err
}
