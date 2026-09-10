package metadata

import (
	"context"
	"fmt"

	modeldo "github.com/openark/orchestrator/internal/models/do"
	modeldomain "github.com/openark/orchestrator/internal/models/domain"
	"github.com/openark/orchestrator/internal/repository/database"
)

func agentFromRow(row modeldo.Agent, includeToken bool) modeldomain.Agent {
	agent := modeldomain.Agent{
		Hostname:      row.Hostname,
		Port:          row.Port,
		LastSubmitted: row.LastSubmitted,
		MySQLPort:     row.MySQLPort.Int64,
	}
	if includeToken {
		agent.Token = row.Token
	}
	return agent
}

func agentsFromRows(rows []modeldo.Agent, includeToken bool) []modeldomain.Agent {
	result := make([]modeldomain.Agent, 0, len(rows))
	for _, row := range rows {
		result = append(result, agentFromRow(row, includeToken))
	}
	return result
}

// SubmitAgent upserts an agent heartbeat.
func SubmitAgent(ctx context.Context, hostname string, port int, token string) error {
	_, err := database.ExecOrchestratorContext(ctx, `
		replace into host_agent (
			hostname, port, token, last_submitted, count_mysql_snapshots
		) values (
			?, ?, ?, now(), 0
		)
	`, hostname, port, token)
	return err
}

// ForgetLongUnseenAgents removes stale agents.
func ForgetLongUnseenAgents(ctx context.Context, unseenForgetHours uint) error {
	_, err := database.ExecOrchestratorContext(ctx, `
		delete from host_agent
		where last_submitted < now() - interval ? hour
	`, unseenForgetHours)
	return err
}

// ReadOutdatedAgentHostnames returns agents whose checks are due.
func ReadOutdatedAgentHostnames(ctx context.Context, pollMinutes uint) ([]string, error) {
	rows, err := database.QueryOrchestratorRows[modeldo.Hostname](ctx, `
		select hostname
		from host_agent
		where ifnull(last_checked < now() - interval ? minute, 1)
	`, pollMinutes)
	hostnames := make([]string, 0, len(rows))
	for _, row := range rows {
		hostnames = append(hostnames, row.Hostname)
	}
	return hostnames, err
}

// ReadAgents reads every agent without exposing stored access tokens.
func ReadAgents(ctx context.Context) ([]modeldomain.Agent, error) {
	rows, err := database.QueryOrchestratorRows[modeldo.Agent](ctx, `
		select hostname, port, token, last_submitted, mysql_port
		from host_agent
		order by hostname
	`)
	return agentsFromRows(rows, false), err
}

// ReadAgent reads one agent and includes its token for the agent client.
func ReadAgent(ctx context.Context, hostname string) ([]modeldomain.Agent, error) {
	rows, err := database.QueryOrchestratorRows[modeldo.Agent](ctx, `
		select hostname, port, token, last_submitted, mysql_port
		from host_agent
		where hostname = ?
	`, hostname)
	return agentsFromRows(rows, true), err
}

// UpdateAgentLastChecked records the latest agent check.
func UpdateAgentLastChecked(ctx context.Context, hostname string) error {
	_, err := database.ExecOrchestratorContext(ctx, `
		update host_agent
		set last_checked = now()
		where hostname = ?
	`, hostname)
	return err
}

// UpdateAgentInfo records information read from the agent service.
func UpdateAgentInfo(ctx context.Context, hostname string, mysqlPort int64, snapshotCount int) error {
	_, err := database.ExecOrchestratorContext(ctx, `
		update host_agent
		set last_seen = now(), mysql_port = ?, count_mysql_snapshots = ?
		where hostname = ?
	`, mysqlPort, snapshotCount, hostname)
	return err
}

// SubmitSeed inserts a seed operation and returns its generated ID.
func SubmitSeed(ctx context.Context, targetHostname, sourceHostname string) (int64, error) {
	result, err := database.ExecOrchestratorSQLContext(ctx, `
		insert into agent_seed (
			target_hostname, source_hostname, start_timestamp
		) values (
			?, ?, now()
		)
	`, targetHostname, sourceHostname)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// CompleteSeed records seed completion and success.
func CompleteSeed(ctx context.Context, seedID int64, successful bool) error {
	_, err := database.ExecOrchestratorContext(ctx, `
		update agent_seed
		set end_timestamp = now(), is_complete = 1, is_successful = ?
		where id = ?
	`, successful, seedID)
	return err
}

// SubmitSeedState inserts one seed state and returns its generated ID.
func SubmitSeedState(ctx context.Context, seedID int64, action, errorMessage string) (int64, error) {
	result, err := database.ExecOrchestratorSQLContext(ctx, `
		insert into agent_seed_state (
			agent_seed_id, state_timestamp, state_action, error_message
		) values (
			?, now(), ?, ?
		)
	`, seedID, action, errorMessage)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// UpdateSeedStateError records the failure of one seed state.
func UpdateSeedStateError(ctx context.Context, seedStateID int64, message string) error {
	_, err := database.ExecOrchestratorContext(ctx, `
		update agent_seed_state
		set error_message = ?
		where id = ?
	`, message, seedStateID)
	return err
}

// FailStaleSeeds marks stalled seed operations as failed.
func FailStaleSeeds(ctx context.Context, staleMinutes uint) error {
	_, err := database.ExecOrchestratorContext(ctx, `
		update agent_seed
		set is_complete = 1, is_successful = 0
		where is_complete = 0
			and (
				select max(state_timestamp)
				from agent_seed_state
				where agent_seed.id = agent_seed_state.agent_seed_id
			) < now() - interval ? minute
	`, staleMinutes)
	return err
}

// ReadActiveSeedsForHost reads active seed operations involving a host.
func ReadActiveSeedsForHost(ctx context.Context, hostname string) ([]modeldomain.SeedOperation, error) {
	return readSeeds(ctx, `
		where is_complete = 0
			and (target_hostname = ? or source_hostname = ?)
	`, "", hostname, hostname)
}

// ReadRecentCompletedSeedsForHost reads the latest completed seed operations involving a host.
func ReadRecentCompletedSeedsForHost(ctx context.Context, hostname string) ([]modeldomain.SeedOperation, error) {
	return readSeeds(ctx, `
		where is_complete = 1
			and (target_hostname = ? or source_hostname = ?)
	`, "limit 10", hostname, hostname)
}

// ReadSeed reads one seed operation.
func ReadSeed(ctx context.Context, seedID int64) ([]modeldomain.SeedOperation, error) {
	return readSeeds(ctx, `where id = ?`, "", seedID)
}

// ReadRecentSeeds reads the latest seed operations.
func ReadRecentSeeds(ctx context.Context) ([]modeldomain.SeedOperation, error) {
	return readSeeds(ctx, "", "limit 100")
}

func readSeeds(ctx context.Context, where, limit string, args ...any) ([]modeldomain.SeedOperation, error) {
	query := fmt.Sprintf(`
		select id, target_hostname, source_hostname, start_timestamp, end_timestamp, is_complete, is_successful
		from agent_seed
		%s
		order by id desc
		%s
	`, where, limit)
	rows, err := database.QueryOrchestratorRows[modeldo.SeedOperation](ctx, query, args...)
	result := make([]modeldomain.SeedOperation, 0, len(rows))
	for _, row := range rows {
		result = append(result, modeldomain.SeedOperation{
			SeedId:         row.SeedID,
			TargetHostname: row.TargetHostname,
			SourceHostname: row.SourceHostname,
			StartTimestamp: row.StartTimestamp,
			EndTimestamp:   row.EndTimestamp,
			IsComplete:     row.IsComplete,
			IsSuccessful:   row.IsSuccessful,
		})
	}
	return result, err
}

// ReadSeedStates reads states for a seed operation.
func ReadSeedStates(ctx context.Context, seedID int64) ([]modeldomain.SeedOperationState, error) {
	rows, err := database.QueryOrchestratorRows[modeldo.SeedOperationState](ctx, `
		select id, agent_seed_id, state_timestamp, state_action, error_message
		from agent_seed_state
		where agent_seed_id = ?
		order by id desc
	`, seedID)
	result := make([]modeldomain.SeedOperationState, 0, len(rows))
	for _, row := range rows {
		result = append(result, modeldomain.SeedOperationState{
			SeedStateId:    row.StateID,
			SeedId:         row.SeedID,
			StateTimestamp: row.StateTimestamp,
			Action:         row.Action,
			ErrorMessage:   row.ErrorMessage,
		})
	}
	return result, err
}
