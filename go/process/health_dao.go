/*
   Copyright 2015 Shlomi Noach, courtesy Booking.com

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package process

import (
	"context"
	"fmt"
	"time"

	"github.com/openark/golib/log"
	"github.com/openark/orchestrator/go/config"
	"github.com/openark/orchestrator/go/db"
)

const healthyHTTPTokenQuery = `
	select
		token
	from
		node_health
	where
		token = ?
		and extra_info = ?
	`

// RegisterNode writes down this node in the node_health table
func WriteRegisterNode(nodeHealth *NodeHealth) (healthy bool, err error) {
	timeNow := time.Now()
	reportedAgo := timeNow.Sub(nodeHealth.LastReported)
	reportedSecondsAgo := int64(reportedAgo.Seconds())
	if reportedSecondsAgo > config.HealthPollSeconds*2 {
		// This entry is too old. No reason to persist it; already expired.
		return false, nil
	}

	nodeHealth.onceHistory.Do(func() {
		db.ExecOrchestrator(`
			insert ignore into node_health_history
				(hostname, token, first_seen_active, extra_info, command, app_version)
			values
				(?, ?, NOW(), ?, ?, ?)
			`,
			nodeHealth.Hostname, nodeHealth.Token, nodeHealth.ExtraInfo, nodeHealth.Command,
			nodeHealth.AppVersion,
		)
	})
	{
		sqlResult, err := db.ExecOrchestrator(`
			update node_health set
				last_seen_active = now() - interval ? second,
				extra_info = case when ? != '' then ? else extra_info end,
				app_version = ?,
				incrementing_indicator = incrementing_indicator + 1
			where
				hostname = ?
				and token = ?
			`,
			reportedSecondsAgo,
			nodeHealth.ExtraInfo, nodeHealth.ExtraInfo,
			nodeHealth.AppVersion,
			nodeHealth.Hostname, nodeHealth.Token,
		)
		if err != nil {
			return false, log.Errore(err)
		}
		rows, err := sqlResult.RowsAffected()
		if err != nil {
			return false, log.Errore(err)
		}
		if rows > 0 {
			return true, nil
		}
	}
	// Got here? The UPDATE didn't work. Row isn't there.
	{
		dbBackend := ""
		if config.Config.IsSQLite() {
			dbBackend = config.Config.SQLite3DataFile
		} else {
			dbBackend = fmt.Sprintf("%s:%d", config.Config.MySQLOrchestratorHost,
				config.Config.MySQLOrchestratorPort)
		}
		sqlResult, err := db.ExecOrchestrator(`
			insert ignore into node_health
				(hostname, token, first_seen_active, last_seen_active, extra_info, command, app_version, db_backend)
			values (
				?, ?,
				now() - interval ? second, now() - interval ? second,
				?, ?, ?, ?)
			`,
			nodeHealth.Hostname, nodeHealth.Token,
			reportedSecondsAgo, reportedSecondsAgo,
			nodeHealth.ExtraInfo, nodeHealth.Command,
			nodeHealth.AppVersion, dbBackend,
		)
		if err != nil {
			return false, log.Errore(err)
		}
		rows, err := sqlResult.RowsAffected()
		if err != nil {
			return false, log.Errore(err)
		}
		if rows > 0 {
			return true, nil
		}
	}
	return false, nil
}

// ExpireAvailableNodes is an aggressive purging method to remove
// node entries who have skipped their keepalive for two times.
func ExpireAvailableNodes() {
	_, err := db.ExecOrchestrator(`
			delete
				from node_health
			where
				last_seen_active < now() - interval ? second
			`,
		config.HealthPollSeconds*5,
	)
	if err != nil {
		log.Errorf("ExpireAvailableNodes: failed to remove old entries: %+v", err)
	}
}

// ExpireNodesHistory cleans up the nodes history and is run by
// the orchestrator active node.
func ExpireNodesHistory() error {
	_, err := db.ExecOrchestrator(`
			delete
				from node_health_history
			where
				first_seen_active < now() - interval ? hour
			`,
		config.Config.UnseenInstanceForgetHours,
	)
	return log.Errore(err)
}

func ReadAvailableNodes(onlyHttpNodes bool) (nodes [](*NodeHealth), err error) {
	extraInfo := ""
	if onlyHttpNodes {
		extraInfo = string(OrchestratorExecutionHttpMode)
	}
	query := `
		select
			hostname, token, app_version, first_seen_active, last_seen_active, db_backend
		from
			node_health
		where
			last_seen_active > now() - interval ? second
			and ? in (extra_info, '')
		order by
			hostname
		`

	type nodeHealthRow struct {
		Hostname        string `gorm:"column:hostname"`
		Token           string `gorm:"column:token"`
		AppVersion      string `gorm:"column:app_version"`
		FirstSeenActive string `gorm:"column:first_seen_active"`
		LastSeenActive  string `gorm:"column:last_seen_active"`
		DBBackend       string `gorm:"column:db_backend"`
	}
	rows, err := db.QueryOrchestratorRows[nodeHealthRow](context.Background(), query, config.HealthPollSeconds*2, extraInfo)
	for _, row := range rows {
		nodes = append(nodes, &NodeHealth{
			Hostname:        row.Hostname,
			Token:           row.Token,
			AppVersion:      row.AppVersion,
			FirstSeenActive: row.FirstSeenActive,
			LastSeenActive:  row.LastSeenActive,
			DBBackend:       row.DBBackend,
		})
	}
	return nodes, log.Errore(err)
}

func TokenBelongsToHealthyHttpService(token string) (result bool, err error) {
	extraInfo := string(OrchestratorExecutionHttpMode)

	type healthyTokenRow struct {
		Token string `gorm:"column:token"`
	}
	rows, err := db.QueryOrchestratorRows[healthyTokenRow](context.Background(), healthyHTTPTokenQuery, token, extraInfo)
	result = err == nil && len(rows) > 0
	return result, log.Errore(err)
}
