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

package inst

import (
	"context"
	"fmt"
	"time"

	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/golib/log"
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
	AuditOperation("begin-downtime", downtime.Key, fmt.Sprintf("owner: %s, reason: %s", downtime.Owner, downtime.Reason))

	return nil
}

// EndDowntime will remove downtime flag from an instance
func EndDowntime(instanceKey *InstanceKey) (wasDowntimed bool, err error) {
	affected, err := metadata.EndDowntime(context.Background(), instanceKey.Hostname, instanceKey.Port)
	if err != nil {
		return wasDowntimed, log.Errore(err)
	}

	if affected > 0 {
		wasDowntimed = true
		AuditOperation("end-downtime", instanceKey, "")
	}
	return wasDowntimed, err
}

// renewLostInRecoveryDowntime renews hosts who are downtimed due to being lost in recovery, such that
// their downtime never expires.
func renewLostInRecoveryDowntime() error {
	return metadata.RenewDowntimeByReason(
		context.Background(), config.LostInRecoveryDowntimeSeconds, DowntimeLostInRecoveryMessage,
	)
}

// expireLostInRecoveryDowntime expires downtime for servers who have been lost in recovery in the last,
// but are now replicating.
func expireLostInRecoveryDowntime() error {
	instances, err := ReadLostInRecoveryInstances("")
	if err != nil {
		return err
	}
	if len(instances) == 0 {
		return nil
	}
	unambiguousAliases, err := ReadUnambiguousSuggestedClusterAliases()
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
			AuditOperation("expire-downtime", nil, fmt.Sprintf("Expired %d entries", rowsAffected))
		}
	}

	return nil
}

func ReadDowntime() (result []Downtime, err error) {
	rows, err := metadata.ReadDowntime(context.Background())
	for _, row := range rows {
		downtime := Downtime{
			Key:            &InstanceKey{Hostname: row.Hostname, Port: row.Port},
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
