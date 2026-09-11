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

package downtime

import (
	"context"
	"fmt"
	"time"

	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/golib/log"
	instaudit "github.com/openark/orchestrator/internal/inst/audit"
	instcluster "github.com/openark/orchestrator/internal/inst/cluster"
	instmodel "github.com/openark/orchestrator/internal/inst/instance"
	modeldomain "github.com/openark/orchestrator/internal/models/domain"
	"github.com/openark/orchestrator/internal/repository/metadata"
)

// BeginDowntime will make mark an instance as downtimed (or override existing downtime period)
func BeginDowntime(downtime *Downtime) (err error) {
	if downtime.Duration == 0 {
		downtime.Duration = config.MaintenanceExpireMinutes * time.Minute
	}
	if downtime.EndsAtString != "" {
		err = metadata.BeginDowntimeAt(context.Background(), modeldomain.DowntimeRecord{
			Hostname:       downtime.Key.Hostname,
			Port:           downtime.Key.Port,
			BeginTimestamp: downtime.BeginsAtString,
			EndTimestamp:   downtime.EndsAtString,
			Owner:          downtime.Owner,
			Reason:         downtime.Reason,
		})
	} else {
		if downtime.Ended() {
			// No point in writing it down; it's expired
			return nil
		}

		err = metadata.BeginDowntimeFor(
			context.Background(), downtime.Key.Hostname, downtime.Key.Port,
			int(downtime.EndsIn().Seconds()), downtime.Owner, downtime.Reason,
		)
	}
	if err != nil {
		return log.Errore(err)
	}
	instaudit.AuditOperation("begin-downtime", downtime.Key, fmt.Sprintf("owner: %s, reason: %s", downtime.Owner, downtime.Reason))

	return nil
}

// EndDowntime will remove downtime flag from an instance
func EndDowntime(instanceKey *instmodel.InstanceKey) (wasDowntimed bool, err error) {
	affected, err := metadata.EndDowntime(context.Background(), instanceKey.Hostname, instanceKey.Port)
	if err != nil {
		return wasDowntimed, log.Errore(err)
	}

	if affected > 0 {
		wasDowntimed = true
		instaudit.AuditOperation("end-downtime", instanceKey, "")
	}
	return wasDowntimed, err
}

// renewLostInRecoveryDowntime renews hosts who are downtimed due to being lost in recovery, such that
// their downtime never expires.
func renewLostInRecoveryDowntime() error {
	return metadata.RenewDowntimeByReason(
		context.Background(), config.LostInRecoveryDowntimeSeconds, instmodel.DowntimeLostInRecoveryMessage,
	)
}

// expireLostInRecoveryDowntime expires downtime for servers who have been lost in recovery in the last,
// but are now replicating.
func expireLostInRecoveryDowntime() error {
	instances, err := readLostInRecoveryInstances()
	if err != nil {
		return err
	}
	if len(instances) == 0 {
		return nil
	}
	unambiguousAliases, err := instcluster.ReadUnambiguousSuggestedClusterAliases()
	if err != nil {
		return err
	}
	for _, instance := range instances {
		// We _may_ expire this downtime, but only after a minute
		// This is a graceful period, during which other servers can claim ownership of the alias,
		// or can update their own cluster name to match a new master's name
		if instance.ElapsedDowntime < time.Minute {
			continue
		}
		if !instance.IsLastCheckValid {
			continue
		}
		endDowntime := false
		if instance.ReplicaRunning() {
			// back, alive, replicating in some topology
			endDowntime = true
		} else if instance.ReplicationDepth == 0 {
			// instance makes the appearance of a master
			if unambiguousKey, ok := unambiguousAliases[instance.SuggestedClusterAlias]; ok {
				if unambiguousKey.Equals(&instance.Key) {
					// This instance seems to be a master, which is valid, and has a suggested alias,
					// and is the _only_ one to have this suggested alias (i.e. no one took its place)
					endDowntime = true
				}
			}
		}
		if endDowntime {
			if _, err := EndDowntime(&instance.Key); err != nil {
				return err
			}
		}
	}
	return nil
}

func readLostInRecoveryInstances() ([]*instmodel.Instance, error) {
	rows, err := metadata.ReadLostInRecoveryInstanceRows(
		context.Background(), instmodel.DowntimeLostInRecoveryMessage, "",
	)
	instances := make([]*instmodel.Instance, 0, len(rows))
	for _, row := range rows {
		instance := instmodel.NewInstance()
		instance.Key = instmodel.InstanceKey{Hostname: row.Hostname, Port: row.Port}
		instance.MasterKey = instmodel.InstanceKey{Hostname: row.MasterHost, Port: row.MasterPort}
		instance.ReadBinlogCoordinates.LogFile = row.MasterLogFile
		instance.UsingOracleGTID = row.OracleGTID
		instance.UsingMariaDBGTID = row.MariaDBGTID
		instance.ReplicationSQLThreadState = instmodel.ReplicationThreadState(row.ReplicationSQLThreadState)
		instance.ReplicationIOThreadState = instmodel.ReplicationThreadState(row.ReplicationIOThreadState)
		instance.ElapsedDowntime = time.Duration(row.ElapsedDowntimeSeconds) * time.Second
		instance.IsLastCheckValid = row.LastCheckValid
		instance.ReplicationDepth = row.ReplicationDepth
		instance.SuggestedClusterAlias = row.SuggestedClusterAlias
		instances = append(instances, instance)
	}
	return instances, err
}

// ExpireDowntime will remove the maintenance flag on old downtimes
func ExpireDowntime() error {
	if err := renewLostInRecoveryDowntime(); err != nil {
		return log.Errore(err)
	}
	if err := expireLostInRecoveryDowntime(); err != nil {
		return log.Errore(err)
	}
	{
		rowsAffected, err := metadata.ExpireDowntime(context.Background())
		if err != nil {
			return log.Errore(err)
		}
		if rowsAffected > 0 {
			instaudit.AuditOperation("expire-downtime", nil, fmt.Sprintf("Expired %d entries", rowsAffected))
		}
	}

	return nil
}

func ReadDowntime() (result []Downtime, err error) {
	rows, err := metadata.ReadDowntime(context.Background())
	for _, row := range rows {
		downtime := Downtime{
			Key:            &instmodel.InstanceKey{Hostname: row.Hostname, Port: row.Port},
			BeginsAtString: row.BeginTimestamp,
			EndsAtString:   row.EndTimestamp,
			Owner:          row.Owner,
			Reason:         row.Reason,
		}
		downtime.BeginsAt, _ = time.Parse(modeldomain.DateTimeFormat, row.BeginTimestamp)
		downtime.EndsAt, _ = time.Parse(modeldomain.DateTimeFormat, row.EndTimestamp)
		downtime.Duration = downtime.EndsAt.Sub(downtime.BeginsAt)
		result = append(result, downtime)
	}
	return result, log.Errore(err)
}
