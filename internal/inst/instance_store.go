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
	"regexp"
	"runtime"
	"time"

	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/observability"
	"github.com/patrickmn/go-cache"
)

const (
	backendDBConcurrency       = 20
	retryInstanceFunctionCount = 5
	retryInterval              = 500 * time.Millisecond
	error1045AccessDenied      = "Error 1045: Access denied for user"
	errorConnectionRefused     = "getsockopt: connection refused"
	errorNoSuchHost            = "no such host"
	errorIOTimeout             = "i/o timeout"
)

var instanceReadChan = make(chan bool, backendDBConcurrency)
var instanceWriteChan = make(chan bool, backendDBConcurrency)

// InstancesByCountReplicas is a sortable type for Instance
type InstancesByCountReplicas [](*Instance)

func (this InstancesByCountReplicas) Len() int      { return len(this) }
func (this InstancesByCountReplicas) Swap(i, j int) { this[i], this[j] = this[j], this[i] }
func (this InstancesByCountReplicas) Less(i, j int) bool {
	return len(this[i].Replicas) < len(this[j].Replicas)
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
type InstancesByDc [](*Instance)

func (this InstancesByDc) Len() int      { return len(this) }
func (this InstancesByDc) Swap(i, j int) { this[i], this[j] = this[j], this[i] }
func (this InstancesByDc) Less(i, j int) bool {
	if this[i].DataCenter == this[j].DataCenter {
		if len(this[i].Replicas) == 0 && len(this[j].Replicas) == 0 {
			return this[i].ReplicationLagSeconds.Int64 < this[j].ReplicationLagSeconds.Int64
		}
		return len(this[i].Replicas) < len(this[j].Replicas)
	}
	return (this[i].DataCenter < this[j].DataCenter)
}

// Constant strings for Group Replication information
// See https://dev.mysql.com/doc/refman/8.0/en/replication-group-members-table.html for additional information.
const (
	// Group member roles
	GroupReplicationMemberRolePrimary   = "PRIMARY"
	GroupReplicationMemberRoleSecondary = "SECONDARY"
	// Group member states
	GroupReplicationMemberStateOnline     = "ONLINE"
	GroupReplicationMemberStateRecovering = "RECOVERING"
	GroupReplicationMemberStateOffline    = "OFFLINE"
	GroupReplicationMemberStateError      = "ERROR"
)

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

var accessDeniedCounter = observability.NewCounter("orchestrator_instance_access_denied_total", "instance.access_denied events")
var readTopologyInstanceCounter = observability.NewCounter("orchestrator_instance_read_topology_total", "instance.read_topology events")
var readInstanceCounter = observability.NewCounter("orchestrator_instance_read_total", "instance.read events")
var writeInstanceCounter = observability.NewCounter("orchestrator_instance_write_total", "instance.write events")

var emptyQuotesRegexp = regexp.MustCompile(`^""$`)

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

// ExecDBWriteFunc chooses how to execute a write onto the database: whether synchronuously or not
func ExecDBWriteFunc(f func() error) error {
	return ExecDBWriteFuncContext(context.Background(), f)
}

// ExecDBWriteFuncContext records task wait separately from execution and preserves panic semantics.
func ExecDBWriteFuncContext(ctx context.Context, f func() error) (resultErr error) {
	ctx, span := observability.StartSpan(ctx, "backend.write_task")
	started := time.Now()
	select {
	case instanceWriteChan <- true:
	case <-ctx.Done():
		observability.RecordBackendWrite(ctx, "failure", time.Since(started), 0)
		observability.EndSpan(span, ctx.Err())
		return ctx.Err()
	}
	wait := time.Since(started)
	execution := time.Now()
	result := "failure"
	defer func() {
		r := recover()
		observability.RecordBackendWrite(ctx, result, wait, time.Since(execution))
		<-instanceWriteChan
		if r != nil {
			observability.EndSpan(span, fmt.Errorf("write task panicked"))
			if _, ok := r.(runtime.Error); ok {
				panic(r)
			}
			// Preserve the existing recovery contract; unsupported panic payloads still panic.
			if _, ok := r.(string); !ok {
				_ = r.(error)
			}
			return
		}
		observability.EndSpan(span, resultErr)
	}()
	resultErr = f()
	result = observability.Result(resultErr)
	return resultErr
}
