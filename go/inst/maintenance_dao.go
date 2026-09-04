/*
   Copyright 2014 Outbrain Inc.

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

package inst

import (
	"context"
	"fmt"

	"github.com/openark/golib/log"
	"github.com/openark/orchestrator/go/config"
	"github.com/openark/orchestrator/go/db"
	"github.com/openark/orchestrator/go/process"
	"github.com/openark/orchestrator/go/util"
)

// ReadActiveMaintenance returns the list of currently active maintenance entries
func ReadActiveMaintenance() ([]Maintenance, error) {
	res := []Maintenance{}
	query := `
		select
			database_instance_maintenance_id,
			hostname,
			port,
			begin_timestamp,
			unix_timestamp() - unix_timestamp(begin_timestamp) as seconds_elapsed,
			maintenance_active,
			owner,
			reason
		from
			database_instance_maintenance
		where
			maintenance_active = 1
		order by
			database_instance_maintenance_id
		`
	type maintenanceRow struct {
		ID             uint   `gorm:"column:database_instance_maintenance_id"`
		Hostname       string `gorm:"column:hostname"`
		Port           int    `gorm:"column:port"`
		BeginTimestamp string `gorm:"column:begin_timestamp"`
		SecondsElapsed uint   `gorm:"column:seconds_elapsed"`
		Active         bool   `gorm:"column:maintenance_active"`
		Owner          string `gorm:"column:owner"`
		Reason         string `gorm:"column:reason"`
	}
	rows, err := db.QueryOrchestratorRows[maintenanceRow](context.Background(), query)
	for _, row := range rows {
		maintenance := Maintenance{}
		maintenance.MaintenanceId = row.ID
		maintenance.Key.Hostname = row.Hostname
		maintenance.Key.Port = row.Port
		maintenance.BeginTimestamp = row.BeginTimestamp
		maintenance.SecondsElapsed = row.SecondsElapsed
		maintenance.IsActive = row.Active
		maintenance.Owner = row.Owner
		maintenance.Reason = row.Reason
		res = append(res, maintenance)
	}

	if err != nil {
		log.Errore(err)
	}
	return res, err

}

// BeginBoundedMaintenance will make new maintenance entry for given instanceKey.
func BeginBoundedMaintenance(instanceKey *InstanceKey, owner string, reason string, durationSeconds uint, explicitlyBounded bool) (int64, error) {
	var maintenanceToken int64 = 0
	if durationSeconds == 0 {
		durationSeconds = config.MaintenanceExpireMinutes * 60
	}
	res, err := db.ExecOrchestratorSQLContext(context.Background(), `
			insert ignore
				into database_instance_maintenance (
					hostname, port, maintenance_active, begin_timestamp, end_timestamp, owner, reason,
					processing_node_hostname, processing_node_token, explicitly_bounded
				) VALUES (
					?, ?, 1, NOW(), NOW() + INTERVAL ? SECOND, ?, ?,
					?, ?, ?
				)
			`,
		instanceKey.Hostname,
		instanceKey.Port,
		durationSeconds,
		owner,
		reason,
		process.ThisHostname,
		util.ProcessToken.Hash,
		explicitlyBounded,
	)
	if err != nil {
		return maintenanceToken, log.Errore(err)
	}

	if affected, _ := res.RowsAffected(); affected == 0 {
		err = fmt.Errorf("Cannot begin maintenance for instance: %+v; maintenance reason: %+v", instanceKey, reason)
	} else {
		// success
		maintenanceToken, _ = res.LastInsertId()
		AuditOperation("begin-maintenance", instanceKey, fmt.Sprintf("maintenanceToken: %d, owner: %s, reason: %s", maintenanceToken, owner, reason))
	}
	return maintenanceToken, err
}

// BeginMaintenance will make new maintenance entry for given instanceKey. Maintenance time is unbounded
func BeginMaintenance(instanceKey *InstanceKey, owner string, reason string) (int64, error) {
	return BeginBoundedMaintenance(instanceKey, owner, reason, 0, false)
}

// EndMaintenanceByInstanceKey will terminate an active maintenance using given instanceKey as hint
func EndMaintenanceByInstanceKey(instanceKey *InstanceKey) (wasMaintenance bool, err error) {
	res, err := db.ExecOrchestrator(`
			update
				database_instance_maintenance
			set
				maintenance_active = NULL,
				end_timestamp = NOW()
			where
				hostname = ?
				and port = ?
				and maintenance_active = 1
			`,
		instanceKey.Hostname,
		instanceKey.Port,
	)
	if err != nil {
		return wasMaintenance, log.Errore(err)
	}

	if affected, _ := res.RowsAffected(); affected > 0 {
		// success
		wasMaintenance = true
		AuditOperation("end-maintenance", instanceKey, "")
	}
	return wasMaintenance, err
}

// InMaintenance checks whether a given instance is under maintenacne
func InMaintenance(instanceKey *InstanceKey) (inMaintenance bool, err error) {
	query := `
		select
			count(*) > 0 as in_maintenance
		from
			database_instance_maintenance
		where
			hostname = ?
			and port = ?
			and maintenance_active = 1
			and end_timestamp > NOW()
			`
	type maintenanceStateRow struct {
		Active bool `gorm:"column:in_maintenance"`
	}
	rows, err := db.QueryOrchestratorRows[maintenanceStateRow](context.Background(), query, instanceKey.Hostname, instanceKey.Port)
	if err == nil && len(rows) > 0 {
		inMaintenance = rows[0].Active
	}

	return inMaintenance, log.Errore(err)
}

// ReadMaintenanceInstanceKey will return the instanceKey for active maintenance by maintenanceToken
func ReadMaintenanceInstanceKey(maintenanceToken int64) (*InstanceKey, error) {
	var res *InstanceKey
	query := `
		select
			hostname, port
		from
			database_instance_maintenance
		where
			database_instance_maintenance_id = ?
			`

	type maintenanceInstanceRow struct {
		Hostname string `gorm:"column:hostname"`
		Port     int    `gorm:"column:port"`
	}
	rows, err := db.QueryOrchestratorRows[maintenanceInstanceRow](context.Background(), query, maintenanceToken)
	if err == nil && len(rows) > 0 {
		instanceKey, merr := NewResolveInstanceKey(rows[0].Hostname, rows[0].Port)
		if merr != nil {
			return nil, log.Errore(merr)
		}
		res = instanceKey
	}

	return res, log.Errore(err)
}

// EndMaintenance will terminate an active maintenance via maintenanceToken
func EndMaintenance(maintenanceToken int64) (wasMaintenance bool, err error) {
	res, err := db.ExecOrchestrator(`
			update
				database_instance_maintenance
			set
				maintenance_active = NULL,
				end_timestamp = NOW()
			where
				database_instance_maintenance_id = ?
			`,
		maintenanceToken,
	)
	if err != nil {
		return wasMaintenance, log.Errore(err)
	}
	if affected, _ := res.RowsAffected(); affected > 0 {
		// success
		wasMaintenance = true
		instanceKey, _ := ReadMaintenanceInstanceKey(maintenanceToken)
		AuditOperation("end-maintenance", instanceKey, fmt.Sprintf("maintenanceToken: %d", maintenanceToken))
	}
	return wasMaintenance, err
}

// ExpireMaintenance will remove the maintenance flag on old maintenances and on bounded maintenances
func ExpireMaintenance() error {
	{
		res, err := db.ExecOrchestrator(`
			delete from
				database_instance_maintenance
			where
				maintenance_active is null
				and end_timestamp < NOW() - INTERVAL ? DAY
			`,
			config.MaintenancePurgeDays,
		)
		if err != nil {
			return log.Errore(err)
		}
		if rowsAffected, _ := res.RowsAffected(); rowsAffected > 0 {
			AuditOperation("expire-maintenance", nil, fmt.Sprintf("Purged historical entries: %d", rowsAffected))
		}
	}
	{
		res, err := db.ExecOrchestrator(`
			delete from
				database_instance_maintenance
			where
				maintenance_active = 1
				and end_timestamp < NOW()
			`,
		)
		if err != nil {
			return log.Errore(err)
		}
		if rowsAffected, _ := res.RowsAffected(); rowsAffected > 0 {
			AuditOperation("expire-maintenance", nil, fmt.Sprintf("Expired bounded: %d", rowsAffected))
		}
	}
	{
		res, err := db.ExecOrchestrator(`
			delete from
				database_instance_maintenance
			where
				explicitly_bounded = 0
				and concat(processing_node_hostname, ':', processing_node_token) not in (
					select concat(hostname, ':', token) from node_health
				)
			`,
		)
		if err != nil {
			return log.Errore(err)
		}
		if rowsAffected, _ := res.RowsAffected(); rowsAffected > 0 {
			AuditOperation("expire-maintenance", nil, fmt.Sprintf("Expired dead: %d", rowsAffected))
		}
	}

	return nil
}
