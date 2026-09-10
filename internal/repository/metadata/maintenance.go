package metadata

import (
	"context"

	modeldo "github.com/openark/orchestrator/internal/models/do"
	modeldomain "github.com/openark/orchestrator/internal/models/domain"
	"github.com/openark/orchestrator/internal/repository/database"
)

// ReadActiveMaintenance reads active maintenance entries.
func ReadActiveMaintenance(ctx context.Context) ([]modeldomain.MaintenanceRecord, error) {
	rows, err := database.QueryOrchestratorRows[modeldo.Maintenance](ctx, `
		select id, hostname, port, begin_timestamp,
			unix_timestamp() - unix_timestamp(begin_timestamp) as seconds_elapsed,
			maintenance_active, owner, reason
		from database_instance_maintenance
		where maintenance_active = 1
		order by id
	`)
	result := make([]modeldomain.MaintenanceRecord, 0, len(rows))
	for _, row := range rows {
		result = append(result, modeldomain.MaintenanceRecord(row))
	}
	return result, err
}

// BeginMaintenance creates a maintenance entry.
func BeginMaintenance(ctx context.Context, hostname string, port int, durationSeconds uint, owner, reason, processingHostname, processingToken string, explicitlyBounded bool) (int64, bool, error) {
	result, err := database.ExecOrchestratorSQLContext(ctx, `
		insert ignore into database_instance_maintenance (
			hostname, port, maintenance_active, begin_timestamp, end_timestamp, owner, reason,
			processing_node_hostname, processing_node_token, explicitly_bounded
		) values (
			?, ?, 1, now(), now() + interval ? second, ?, ?, ?, ?, ?
		)
	`, hostname, port, durationSeconds, owner, reason, processingHostname, processingToken, explicitlyBounded)
	if err != nil {
		return 0, false, err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return 0, false, nil
	}
	id, _ := result.LastInsertId()
	return id, true, nil
}

// EndMaintenanceByInstance ends active maintenance for an instance.
func EndMaintenanceByInstance(ctx context.Context, hostname string, port int) (int64, error) {
	return execRowsAffected(ctx, `
		update database_instance_maintenance
		set maintenance_active = null, end_timestamp = now()
		where hostname = ? and port = ? and maintenance_active = 1
	`, hostname, port)
}

// InstanceInMaintenance reports whether an instance has active, unexpired maintenance.
func InstanceInMaintenance(ctx context.Context, hostname string, port int) (bool, error) {
	rows, err := database.QueryOrchestratorRows[modeldo.MaintenanceState](ctx, `
		select count(*) > 0 as in_maintenance
		from database_instance_maintenance
		where hostname = ? and port = ? and maintenance_active = 1 and end_timestamp > now()
	`, hostname, port)
	if err != nil || len(rows) == 0 {
		return false, err
	}
	return rows[0].Active, nil
}

// ReadMaintenanceInstanceKey reads the instance associated with a maintenance ID.
func ReadMaintenanceInstanceKey(ctx context.Context, maintenanceID int64) ([]modeldomain.InstanceIdentity, error) {
	rows, err := database.QueryOrchestratorRows[modeldo.InstanceKey](ctx, `
		select hostname, port
		from database_instance_maintenance
		where id = ?
	`, maintenanceID)
	result := make([]modeldomain.InstanceIdentity, 0, len(rows))
	for _, row := range rows {
		result = append(result, modeldomain.InstanceIdentity(row))
	}
	return result, err
}

// EndMaintenanceByID ends a maintenance entry by ID.
func EndMaintenanceByID(ctx context.Context, maintenanceID int64) (int64, error) {
	return execRowsAffected(ctx, `
		update database_instance_maintenance
		set maintenance_active = null, end_timestamp = now()
		where id = ?
	`, maintenanceID)
}

// PurgeHistoricalMaintenance removes old inactive maintenance rows.
func PurgeHistoricalMaintenance(ctx context.Context, purgeDays uint) (int64, error) {
	return execRowsAffected(ctx, `
		delete from database_instance_maintenance
		where maintenance_active is null and end_timestamp < now() - interval ? day
	`, purgeDays)
}

// ExpireBoundedMaintenance removes expired bounded maintenance rows.
func ExpireBoundedMaintenance(ctx context.Context) (int64, error) {
	return execRowsAffected(ctx, `
		delete from database_instance_maintenance
		where maintenance_active = 1 and end_timestamp < now()
	`)
}

// ExpireDeadNodeMaintenance removes unbounded rows owned by dead nodes.
func ExpireDeadNodeMaintenance(ctx context.Context) (int64, error) {
	return execRowsAffected(ctx, `
		delete from database_instance_maintenance
		where explicitly_bounded = 0
			and concat(processing_node_hostname, ':', processing_node_token) not in (
				select concat(hostname, ':', token) from node_health
			)
	`)
}

func execRowsAffected(ctx context.Context, query string, args ...any) (int64, error) {
	result, err := database.ExecOrchestratorContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
