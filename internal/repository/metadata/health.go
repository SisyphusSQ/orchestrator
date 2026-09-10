package metadata

import (
	"context"

	modeldo "github.com/openark/orchestrator/internal/models/do"
	modeldomain "github.com/openark/orchestrator/internal/models/domain"
	"github.com/openark/orchestrator/internal/repository/database"
)

const healthyHTTPTokenQuery = `
	select token
	from node_health
	where token = ?
		and extra_info = ?
`

// InsertNodeHealthHistory records the first sighting of a node token.
func InsertNodeHealthHistory(ctx context.Context, row modeldomain.NodeHealth, extraInfo, command string) error {
	_, err := database.ExecOrchestratorContext(ctx, `
		insert ignore into node_health_history
			(hostname, token, first_seen_active, extra_info, command, app_version)
		values
			(?, ?, now(), ?, ?, ?)
	`, row.Hostname, row.Token, extraInfo, command, row.AppVersion)
	return err
}

// UpdateNodeHealth refreshes an existing node heartbeat.
func UpdateNodeHealth(ctx context.Context, row modeldomain.NodeHealth, reportedSecondsAgo int64, extraInfo string) (bool, error) {
	result, err := database.ExecOrchestratorContext(ctx, `
		update node_health set
			last_seen_active = now() - interval ? second,
			extra_info = case when ? != '' then ? else extra_info end,
			app_version = ?,
			incrementing_indicator = incrementing_indicator + 1
		where hostname = ? and token = ?
	`, reportedSecondsAgo, extraInfo, extraInfo, row.AppVersion, row.Hostname, row.Token)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count > 0, err
}

// InsertNodeHealth creates a node heartbeat when no existing row was updated.
func InsertNodeHealth(ctx context.Context, row modeldomain.NodeHealth, reportedSecondsAgo int64, extraInfo, command string) (bool, error) {
	result, err := database.ExecOrchestratorContext(ctx, `
		insert ignore into node_health
			(hostname, token, first_seen_active, last_seen_active, extra_info, command, app_version, db_backend)
		values (
			?, ?,
			now() - interval ? second, now() - interval ? second,
			?, ?, ?, ?
		)
	`, row.Hostname, row.Token, reportedSecondsAgo, reportedSecondsAgo, extraInfo, command, row.AppVersion, row.DBBackend)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count > 0, err
}

// ExpireAvailableNodes removes nodes that have missed their heartbeat window.
func ExpireAvailableNodes(ctx context.Context, expirySeconds int64) error {
	_, err := database.ExecOrchestratorContext(ctx, `
		delete from node_health
		where last_seen_active < now() - interval ? second
	`, expirySeconds)
	return err
}

// ExpireNodesHistory removes old node history rows.
func ExpireNodesHistory(ctx context.Context, expiryHours uint) error {
	_, err := database.ExecOrchestratorContext(ctx, `
		delete from node_health_history
		where first_seen_active < now() - interval ? hour
	`, expiryHours)
	return err
}

// ReadAvailableNodes returns live node rows, optionally constrained to HTTP nodes.
func ReadAvailableNodes(ctx context.Context, healthPollSeconds int64, extraInfo string) ([]modeldomain.NodeHealth, error) {
	rows, err := database.QueryOrchestratorRows[modeldo.NodeHealth](ctx, `
		select hostname, token, app_version, first_seen_active, last_seen_active, db_backend
		from node_health
		where last_seen_active > now() - interval ? second
			and ? in (extra_info, '')
		order by hostname
	`, healthPollSeconds*2, extraInfo)
	result := make([]modeldomain.NodeHealth, 0, len(rows))
	for _, row := range rows {
		result = append(result, modeldomain.NodeHealth{
			Hostname:        row.Hostname,
			Token:           row.Token,
			AppVersion:      row.AppVersion,
			FirstSeenActive: row.FirstSeenActive,
			LastSeenActive:  row.LastSeenActive,
			DBBackend:       row.DBBackend,
		})
	}
	return result, err
}

// TokenBelongsToHealthyHTTPService reports whether a token belongs to a live HTTP node.
func TokenBelongsToHealthyHTTPService(ctx context.Context, token, extraInfo string) (bool, error) {
	rows, err := database.QueryOrchestratorRows[modeldo.HealthyToken](ctx, healthyHTTPTokenQuery, token, extraInfo)
	return err == nil && len(rows) > 0, err
}
