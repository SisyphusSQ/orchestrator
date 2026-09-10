package metadata

import (
	"context"

	modeldo "github.com/openark/orchestrator/internal/models/do"
	modeldomain "github.com/openark/orchestrator/internal/models/domain"
	"github.com/openark/orchestrator/internal/repository/database"
	"gorm.io/gorm"
)

// WritePoolInstances replaces one pool's membership in a transaction.
func WritePoolInstances(ctx context.Context, pool string, keys []modeldomain.InstanceIdentity) error {
	handle, err := database.OpenOrchestratorGORMContext(ctx)
	if err != nil {
		return err
	}
	return handle.Transaction(func(tx *gorm.DB) error {
		if _, err := database.ExecOrchestratorGORM(tx, `delete from database_instance_pool where pool = ?`, pool); err != nil {
			return err
		}
		for _, key := range keys {
			if _, err := database.ExecOrchestratorGORM(tx, `
				insert into database_instance_pool (hostname, port, pool, registered_at)
				values (?, ?, ?, now())
			`, key.Hostname, key.Port, pool); err != nil {
				return err
			}
		}
		return nil
	})
}

// ReadClusterPoolInstances reads pool memberships, optionally filtered by cluster and pool.
func ReadClusterPoolInstances(ctx context.Context, clusterName, pool string) ([]modeldomain.ClusterPoolInstance, error) {
	query := `
		select cluster_name, ifnull(alias, cluster_name) as alias, database_instance_pool.*
		from database_instance
		join database_instance_pool using (hostname, port)
		left join cluster_alias using (cluster_name)
	`
	args := make([]any, 0, 2)
	if clusterName != "" {
		query += ` where database_instance.cluster_name = ? and ? in ('', pool)`
		args = append(args, clusterName, pool)
	}
	rows, err := database.QueryOrchestratorRows[modeldo.ClusterPoolInstance](ctx, query, args...)
	result := make([]modeldomain.ClusterPoolInstance, 0, len(rows))
	for _, row := range rows {
		result = append(result, modeldomain.ClusterPoolInstance{
			ClusterName: row.ClusterName, ClusterAlias: row.ClusterAlias, Pool: row.Pool,
			Hostname: row.Hostname, Port: row.Port,
		})
	}
	return result, err
}

// ReadPoolInstancesSubmissions reads aggregate pool submission records.
func ReadPoolInstancesSubmissions(ctx context.Context) ([]modeldomain.PoolInstancesSubmission, error) {
	rows, err := database.QueryOrchestratorRows[modeldo.PoolInstancesSubmission](ctx, `
		select pool, min(registered_at) as registered_at,
			group_concat(concat(hostname, ':', port)) as hosts
		from database_instance_pool
		group by pool
	`)
	result := make([]modeldomain.PoolInstancesSubmission, 0, len(rows))
	for _, row := range rows {
		result = append(result, modeldomain.PoolInstancesSubmission{
			Pool: row.Pool, RegisteredAt: row.RegisteredAt, Hosts: row.Hosts,
		})
	}
	return result, err
}

// ExpirePoolInstances removes stale pool membership submissions.
func ExpirePoolInstances(ctx context.Context, expiryMinutes uint) error {
	_, err := database.ExecOrchestratorContext(ctx, `
		delete from database_instance_pool
		where registered_at < now() - interval ? minute
	`, expiryMinutes)
	return err
}
