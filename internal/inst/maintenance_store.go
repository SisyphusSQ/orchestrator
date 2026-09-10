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

	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/golib/log"
	"github.com/openark/orchestrator/internal/process"
	"github.com/openark/orchestrator/internal/repository/metadata"
	"github.com/openark/orchestrator/internal/util"
)

// ReadActiveMaintenance returns the list of currently active maintenance entries
func ReadActiveMaintenance() ([]Maintenance, error) {
	rows, err := metadata.ReadActiveMaintenance(context.Background())
	res := make([]Maintenance, 0, len(rows))
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
	maintenanceToken, inserted, err := metadata.BeginMaintenance(
		context.Background(), instanceKey.Hostname, instanceKey.Port, durationSeconds,
		owner, reason, process.ThisHostname, util.ProcessToken.Hash, explicitlyBounded,
	)
	if err != nil {
		return 0, log.Errore(err)
	}

	if !inserted {
		err = fmt.Errorf("Cannot begin maintenance for instance: %+v; maintenance reason: %+v", instanceKey, reason)
	} else {
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
	affected, err := metadata.EndMaintenanceByInstance(context.Background(), instanceKey.Hostname, instanceKey.Port)
	if err != nil {
		return wasMaintenance, log.Errore(err)
	}

	if affected > 0 {
		// success
		wasMaintenance = true
		AuditOperation("end-maintenance", instanceKey, "")
	}
	return wasMaintenance, err
}

// InMaintenance checks whether a given instance is under maintenacne
func InMaintenance(instanceKey *InstanceKey) (inMaintenance bool, err error) {
	inMaintenance, err = metadata.InstanceInMaintenance(context.Background(), instanceKey.Hostname, instanceKey.Port)
	return inMaintenance, log.Errore(err)
}

// ReadMaintenanceInstanceKey will return the instanceKey for active maintenance by maintenanceToken
func ReadMaintenanceInstanceKey(maintenanceToken int64) (*InstanceKey, error) {
	var res *InstanceKey
	rows, err := metadata.ReadMaintenanceInstanceKey(context.Background(), maintenanceToken)
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
	affected, err := metadata.EndMaintenanceByID(context.Background(), maintenanceToken)
	if err != nil {
		return wasMaintenance, log.Errore(err)
	}
	if affected > 0 {
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
		rowsAffected, err := metadata.PurgeHistoricalMaintenance(context.Background(), config.MaintenancePurgeDays)
		if err != nil {
			return log.Errore(err)
		}
		if rowsAffected > 0 {
			AuditOperation("expire-maintenance", nil, fmt.Sprintf("Purged historical entries: %d", rowsAffected))
		}
	}
	{
		rowsAffected, err := metadata.ExpireBoundedMaintenance(context.Background())
		if err != nil {
			return log.Errore(err)
		}
		if rowsAffected > 0 {
			AuditOperation("expire-maintenance", nil, fmt.Sprintf("Expired bounded: %d", rowsAffected))
		}
	}
	{
		rowsAffected, err := metadata.ExpireDeadNodeMaintenance(context.Background())
		if err != nil {
			return log.Errore(err)
		}
		if rowsAffected > 0 {
			AuditOperation("expire-maintenance", nil, fmt.Sprintf("Expired dead: %d", rowsAffected))
		}
	}

	return nil
}
