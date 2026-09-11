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

package inventory

import (
	"time"

	"github.com/openark/orchestrator/internal/config"
	instmodel "github.com/openark/orchestrator/internal/inst/instance"
	"github.com/openark/orchestrator/internal/observability"
	"github.com/patrickmn/go-cache"
)

const (
	backendDBConcurrency = 20
)

var instanceReadChan = make(chan bool, backendDBConcurrency)

// InstancesByCountReplicas is a sortable type for Instance
type InstancesByCountReplicas []*instmodel.Instance

func (instances InstancesByCountReplicas) Len() int { return len(instances) }
func (instances InstancesByCountReplicas) Swap(i, j int) {
	instances[i], instances[j] = instances[j], instances[i]
}
func (instances InstancesByCountReplicas) Less(i, j int) bool {
	return len(instances[i].Replicas) < len(instances[j].Replicas)
}

// InstancesByDc is a sortable type for Instance
//  1. Instances are sorted by DC
//  2. Within DC group instances are sorted by replicas count
//  3. Within ReplicasCount group insances are:
//     a) not sorted if ReplicasCount > 0
//     b) sorted by replication lag if ReplicasCount == 0
//
// DC1 < DC2
// if DC1 == DC2 => len(Replicas1) < len (Replicas2)
// if Replicas.cnt == 0 => replicationLag1 < replicatonLag2
type InstancesByDc []*instmodel.Instance

func (instances InstancesByDc) Len() int { return len(instances) }
func (instances InstancesByDc) Swap(i, j int) {
	instances[i], instances[j] = instances[j], instances[i]
}
func (instances InstancesByDc) Less(i, j int) bool {
	if instances[i].DataCenter == instances[j].DataCenter {
		if len(instances[i].Replicas) == 0 && len(instances[j].Replicas) == 0 {
			return instances[i].ReplicationLagSeconds.Int64 < instances[j].ReplicationLagSeconds.Int64
		}
		return len(instances[i].Replicas) < len(instances[j].Replicas)
	}
	return (instances[i].DataCenter < instances[j].DataCenter)
}

// We use this map to identify whether the query failed because the server does not support group replication or due
// to a different reason.
var GroupReplicationNotSupportedErrors = map[uint16]bool{
	// If either the group replication global variables are not known or the
	// performance_schema.replication_group_members table does not exist, the host does not support group
	// replication, at least in the form supported here.
	1193: true, // ERROR: 1193 (HY000): Unknown system variable 'group_replication_group_name'
	1146: true, // ERROR: 1146 (42S02): Table 'performance_schema.replication_group_members' doesn't exist
}

// instanceKeyInformativeClusterName is a non-authoritative cache; used for auditing or general purpose.
var instanceKeyInformativeClusterName *cache.Cache
var forgetInstanceKeys *cache.Cache
var clusterInjectedPseudoGTIDCache *cache.Cache

var readInstanceCounter = observability.NewCounter("orchestrator_instance_read_total", "instance.read events")
var writeInstanceCounter = observability.NewCounter("orchestrator_instance_write_total", "instance.write events")

func init() {

	go initializeInstanceStore()
}

func initializeInstanceStore() {
	config.WaitForConfigurationToBeLoaded()
	instanceWriteBuffer = make(chan instanceUpdateObject, config.Config.Topology.WriteBuffer.Size)
	observability.Gauge("orchestrator_write_buffer_items", "Current pending instance writes", func() int64 { return int64(len(instanceWriteBuffer)) })
	instanceKeyInformativeClusterName = cache.New(time.Duration(config.Config.Topology.Discovery.PollSeconds/2)*time.Second, time.Second)
	forgetInstanceKeys = cache.New(time.Duration(config.Config.Topology.Discovery.PollSeconds*3)*time.Second, time.Second)
	clusterInjectedPseudoGTIDCache = cache.New(time.Minute, time.Second)
	// spin off instance write buffer flushing
	go func() {
		flushTick := time.Tick(time.Duration(config.Config.Topology.WriteBuffer.FlushIntervalMilliseconds) * time.Millisecond)
		for {
			// it is time to flush
			select {
			case <-flushTick:
				flushInstanceWriteBuffer()
			case <-forceFlushInstanceWriteBuffer:
				flushInstanceWriteBuffer()
			}
		}
	}()
}
