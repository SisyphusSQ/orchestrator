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

// Package pool owns cluster pool submissions and memberships.
package pool

import (
	"context"
	"strings"
	"time"

	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/golib/log"
	instmodel "github.com/openark/orchestrator/internal/inst/instance"
	instresolve "github.com/openark/orchestrator/internal/inst/resolve"
	"github.com/openark/orchestrator/internal/repository/metadata"
)

// PoolInstancesMap lists instance keys per pool name
type PoolInstancesMap map[string]([]*instmodel.InstanceKey)

type PoolInstancesSubmission struct {
	CreatedAt          time.Time
	Pool               string
	DelimitedInstances string
	RegisteredAt       string
}

func NewPoolInstancesSubmission(pool string, instances string) *PoolInstancesSubmission {
	return &PoolInstancesSubmission{
		CreatedAt:          time.Now(),
		Pool:               pool,
		DelimitedInstances: instances,
	}
}

// ClusterPoolInstance is an instance mapping a cluster, pool & instance
type ClusterPoolInstance struct {
	ClusterName  string
	ClusterAlias string
	Pool         string
	Hostname     string
	Port         int
}

func ApplyPoolInstances(submission *PoolInstancesSubmission) error {
	if submission.CreatedAt.Add(time.Duration(config.Config.Topology.Pools.ExpiryMinutes) * time.Minute).Before(time.Now()) {
		// already expired; no need to persist
		return nil
	}
	var instanceKeys []*instmodel.InstanceKey
	if submission.DelimitedInstances != "" {
		instancesStrings := strings.SplitSeq(submission.DelimitedInstances, ",")
		for instanceString := range instancesStrings {
			instanceString = strings.TrimSpace(instanceString)
			instanceKey, err := instresolve.ParseInstanceKey(instanceString)
			if config.Config.Topology.Pools.SupportFuzzyHostnames {
				instanceKey = readFuzzyInstanceKeyIfPossible(instanceKey)
			}
			if err != nil {
				return log.Errore(err)
			}

			instanceKeys = append(instanceKeys, instanceKey)
		}
	}
	log.Debugf("submitting %d instances in %+v pool", len(instanceKeys), submission.Pool)
	writePoolInstances(submission.Pool, instanceKeys)
	return nil
}

func readFuzzyInstanceKeyIfPossible(key *instmodel.InstanceKey) *instmodel.InstanceKey {
	if key == nil || key.IsIPv4() || key.Hostname == "" {
		return key
	}
	rows, _ := metadata.ReadFuzzyInstanceRows(context.Background(), key.Hostname, key.Port)
	if len(rows) != 1 {
		return key
	}
	return &instmodel.InstanceKey{Hostname: rows[0].Hostname, Port: rows[0].Port}
}
