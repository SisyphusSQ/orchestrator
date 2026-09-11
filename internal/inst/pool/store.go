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

package pool

import (
	"context"
	instmodel "github.com/openark/orchestrator/internal/inst/instance"
	"time"

	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/golib/log"
	modeldomain "github.com/openark/orchestrator/internal/models/domain"
	"github.com/openark/orchestrator/internal/repository/metadata"
)

// writePoolInstances will write (and override) a single cluster name mapping
func writePoolInstances(pool string, instanceKeys []*instmodel.InstanceKey) error {
	writeFunc := func() error {
		keys := make([]modeldomain.InstanceIdentity, 0, len(instanceKeys))
		for _, key := range instanceKeys {
			keys = append(keys, modeldomain.InstanceIdentity{Hostname: key.Hostname, Port: key.Port})
		}
		return log.Errore(metadata.WritePoolInstances(context.Background(), pool, keys))
	}
	return metadata.ExecuteWrite(context.Background(), writeFunc)
}

// ReadClusterPoolInstances reads cluster-pool-instance associationsfor given cluster and pool
func ReadClusterPoolInstances(clusterName string, pool string) (result []*ClusterPoolInstance, err error) {
	rows, err := metadata.ReadClusterPoolInstances(context.Background(), clusterName, pool)
	for _, row := range rows {
		clusterPoolInstance := ClusterPoolInstance{
			ClusterName:  row.ClusterName,
			ClusterAlias: row.ClusterAlias,
			Pool:         row.Pool,
			Hostname:     row.Hostname,
			Port:         row.Port,
		}
		result = append(result, &clusterPoolInstance)
	}

	if err != nil {
		return nil, err
	}

	return result, nil
}

// ReadAllClusterPoolInstances returns all clusters-pools-insatnces associations
func ReadAllClusterPoolInstances() ([]*ClusterPoolInstance, error) {
	return ReadClusterPoolInstances("", "")
}

// ReadClusterPoolInstancesMap returns association of pools-to-instances for a given cluster
// and potentially for a given pool.
func ReadClusterPoolInstancesMap(clusterName string, pool string) (*PoolInstancesMap, error) {
	var poolInstancesMap = make(PoolInstancesMap)

	clusterPoolInstances, err := ReadClusterPoolInstances(clusterName, pool)
	if err != nil {
		return nil, nil
	}
	for _, clusterPoolInstance := range clusterPoolInstances {
		if _, ok := poolInstancesMap[clusterPoolInstance.Pool]; !ok {
			poolInstancesMap[clusterPoolInstance.Pool] = []*instmodel.InstanceKey{}
		}
		poolInstancesMap[clusterPoolInstance.Pool] = append(poolInstancesMap[clusterPoolInstance.Pool], &instmodel.InstanceKey{Hostname: clusterPoolInstance.Hostname, Port: clusterPoolInstance.Port})
	}

	return &poolInstancesMap, nil
}

func ReadAllPoolInstancesSubmissions() ([]PoolInstancesSubmission, error) {
	rows, err := metadata.ReadPoolInstancesSubmissions(context.Background())
	result := make([]PoolInstancesSubmission, 0, len(rows))
	for _, row := range rows {
		submission := PoolInstancesSubmission{
			Pool: row.Pool,
		}
		submission.CreatedAt, _ = time.Parse(modeldomain.DateTimeFormat, row.RegisteredAt)
		submission.RegisteredAt = row.RegisteredAt
		submission.DelimitedInstances = row.Hosts
		result = append(result, submission)
	}

	return result, log.Errore(err)
}

// ExpirePoolInstances cleans up the database_instance_pool table from expired items
func ExpirePoolInstances() error {
	err := metadata.ExpirePoolInstances(context.Background(), config.Config.Topology.Pools.ExpiryMinutes)
	return log.Errore(err)
}
