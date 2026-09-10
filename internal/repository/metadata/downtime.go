package metadata

import (
	"context"

	modeldo "github.com/openark/orchestrator/internal/models/do"
	modeldomain "github.com/openark/orchestrator/internal/models/domain"
	"github.com/openark/orchestrator/internal/repository/database"
)

// BeginDowntimeAt upserts downtime using explicit begin/end timestamps.
func BeginDowntimeAt(ctx context.Context, row modeldomain.DowntimeRecord) error {
	_, err := database.ExecOrchestratorContext(ctx, `
		insert into database_instance_downtime (
			hostname, port, downtime_active, begin_timestamp, end_timestamp, owner, reason
		) values (?, ?, 1, ?, ?, ?, ?)
		on duplicate key update
			downtime_active = values(downtime_active),
			begin_timestamp = values(begin_timestamp),
			end_timestamp = values(end_timestamp),
			owner = values(owner),
			reason = values(reason)
	`, row.Hostname, row.Port, row.BeginTimestamp, row.EndTimestamp, row.Owner, row.Reason)
	return err
}

// BeginDowntimeFor upserts downtime for a relative duration.
func BeginDowntimeFor(ctx context.Context, hostname string, port, durationSeconds int, owner, reason string) error {
	_, err := database.ExecOrchestratorContext(ctx, `
		insert into database_instance_downtime (
			hostname, port, downtime_active, begin_timestamp, end_timestamp, owner, reason
		) values (?, ?, 1, now(), now() + interval ? second, ?, ?)
		on duplicate key update
			downtime_active = values(downtime_active),
			begin_timestamp = values(begin_timestamp),
			end_timestamp = values(end_timestamp),
			owner = values(owner),
			reason = values(reason)
	`, hostname, port, durationSeconds, owner, reason)
	return err
}

// EndDowntime deletes an instance downtime row.
func EndDowntime(ctx context.Context, hostname string, port int) (int64, error) {
	return execRowsAffected(ctx, `
		delete from database_instance_downtime where hostname = ? and port = ?
	`, hostname, port)
}

// RenewDowntimeByReason extends active downtime rows with a matching reason.
func RenewDowntimeByReason(ctx context.Context, durationSeconds int, reason string) error {
	_, err := database.ExecOrchestratorContext(ctx, `
		update database_instance_downtime
		set end_timestamp = now() + interval ? second
		where end_timestamp > now() and reason = ?
	`, durationSeconds, reason)
	return err
}

// ExpireDowntime deletes elapsed downtime rows.
func ExpireDowntime(ctx context.Context) (int64, error) {
	return execRowsAffected(ctx, `delete from database_instance_downtime where end_timestamp < now()`)
}

// ReadDowntime reads active downtime rows.
func ReadDowntime(ctx context.Context) ([]modeldomain.DowntimeRecord, error) {
	rows, err := database.QueryOrchestratorRows[modeldo.Downtime](ctx, `
		select hostname, port, begin_timestamp, end_timestamp, owner, reason
		from database_instance_downtime
		where end_timestamp > now()
	`)
	result := make([]modeldomain.DowntimeRecord, 0, len(rows))
	for _, row := range rows {
		result = append(result, modeldomain.DowntimeRecord{
			Hostname: row.Hostname, Port: row.Port, BeginTimestamp: row.BeginTimestamp,
			EndTimestamp: row.EndTimestamp, Owner: row.Owner, Reason: row.Reason,
		})
	}
	return result, err
}
