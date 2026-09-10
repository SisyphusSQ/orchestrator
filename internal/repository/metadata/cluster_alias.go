package metadata

import (
	"context"

	modeldo "github.com/openark/orchestrator/internal/models/do"
	modeldomain "github.com/openark/orchestrator/internal/models/domain"
	"github.com/openark/orchestrator/internal/repository/database"
)

func ReadClusterNameByAlias(ctx context.Context, alias string) ([]string, error) {
	rows, err := database.QueryOrchestratorRows[modeldo.ClusterName](ctx, `
		select cluster_name from cluster_alias where alias = ? or cluster_name = ?
	`, alias, alias)
	result := make([]string, 0, len(rows))
	for _, row := range rows {
		result = append(result, row.ClusterName)
	}
	return result, err
}

func ReadClusterAlias(ctx context.Context, clusterName string) ([]modeldomain.ClusterAlias, error) {
	rows, err := database.QueryOrchestratorRows[modeldo.ClusterAlias](ctx, `
		select cluster_name, alias from cluster_alias where cluster_name = ?
	`, clusterName)
	result := make([]modeldomain.ClusterAlias, 0, len(rows))
	for _, row := range rows {
		result = append(result, modeldomain.ClusterAlias{ClusterName: row.ClusterName, Alias: row.Alias})
	}
	return result, err
}

func WriteClusterAlias(ctx context.Context, clusterName, alias string) error {
	_, err := database.ExecOrchestratorContext(ctx, `
		replace into cluster_alias (cluster_name, alias, last_registered) values (?, ?, now())
	`, clusterName, alias)
	return err
}

func WriteClusterAliasOverride(ctx context.Context, clusterName, alias string) error {
	_, err := database.ExecOrchestratorContext(ctx, `
		replace into cluster_alias_override (cluster_name, alias) values (?, ?)
	`, clusterName, alias)
	return err
}

func UpdateClusterAliasesUsingReplace(ctx context.Context, lostInRecoveryReason string) error {
	_, err := database.ExecOrchestratorContext(ctx, `
		replace into cluster_alias (alias, cluster_name, last_registered)
		select suggested_cluster_alias, cluster_name, now()
		from database_instance
		left join database_instance_downtime using (hostname, port)
		where suggested_cluster_alias != ''
			and ifnull(
				database_instance_downtime.downtime_active = 1
				and database_instance_downtime.end_timestamp > now()
				and database_instance_downtime.reason = ?, 0
			) = 0
		order by ifnull(last_checked <= last_seen, 0) asc, read_only desc, num_slave_hosts asc
	`, lostInRecoveryReason)
	return err
}

func UpdateClusterAliasesUsingInsert(ctx context.Context, lostInRecoveryReason string) error {
	_, err := database.ExecOrchestratorContext(ctx, `
		insert into cluster_alias (alias, cluster_name, last_registered)
		select di.suggested_cluster_alias, di.cluster_name, now()
		from database_instance di
		left join database_instance_downtime did using (hostname, port)
		where di.suggested_cluster_alias != ''
			and ifnull(
				did.downtime_active = 1 and did.end_timestamp > now() and did.reason = ?, 0
			) = 0
		order by ifnull(di.last_checked <= di.last_seen, 0) asc, di.read_only desc, di.num_slave_hosts asc
		on duplicate key update
			alias = di.suggested_cluster_alias,
			cluster_name = di.cluster_name,
			last_registered = now()
	`, lostInRecoveryReason)
	return err
}

func WriteMissingClusterAliases(ctx context.Context) error {
	_, err := database.ExecOrchestratorContext(ctx, `
		replace into cluster_alias (alias, cluster_name, last_registered)
		select cluster_name as alias, cluster_name, now()
		from database_instance
		group by cluster_name
		having sum(suggested_cluster_alias = '') = count(*)
	`)
	return err
}

func ForgetLongUnseenClusterAliases(ctx context.Context, unseenForgetHours uint) (int64, error) {
	return execRowsAffected(ctx, `
		delete from cluster_alias where last_registered < now() - interval ? hour
	`, unseenForgetHours)
}

func ReplaceClusterAliasName(ctx context.Context, oldClusterName, newClusterName string) error {
	_, err := database.ExecOrchestratorContext(ctx, `
		update cluster_alias set cluster_name = ? where cluster_name = ?
	`, newClusterName, oldClusterName)
	return err
}

func ReplaceClusterAliasOverrideName(ctx context.Context, oldClusterName, newClusterName string) error {
	_, err := database.ExecOrchestratorContext(ctx, `
		update cluster_alias_override set cluster_name = ? where cluster_name = ?
	`, newClusterName, oldClusterName)
	return err
}

func ReadUnambiguousSuggestedClusterAliases(ctx context.Context) ([]modeldomain.SuggestedClusterAlias, error) {
	rows, err := database.QueryOrchestratorRows[modeldo.SuggestedClusterAlias](ctx, `
		select suggested_cluster_alias, min(hostname) as hostname, min(port) as port
		from database_instance
		where suggested_cluster_alias != '' and replication_depth = 0
		group by suggested_cluster_alias
		having count(*) = 1
	`)
	result := make([]modeldomain.SuggestedClusterAlias, 0, len(rows))
	for _, row := range rows {
		result = append(result, modeldomain.SuggestedClusterAlias(row))
	}
	return result, err
}
