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

// Package recovery owns failure detection, recovery orchestration and history.
package recovery

import (
	"github.com/openark/orchestrator/internal/golib/log"
	"github.com/openark/orchestrator/internal/inst/analysis"
	instaudit "github.com/openark/orchestrator/internal/inst/audit"
	instreplication "github.com/openark/orchestrator/internal/inst/change/replication"
	instdiscovery "github.com/openark/orchestrator/internal/inst/discovery"
	instmodel "github.com/openark/orchestrator/internal/inst/instance"
	instinventory "github.com/openark/orchestrator/internal/inst/inventory"
	"github.com/patrickmn/go-cache"
)

// Force a re-read of a topology instance; this is done because we need to substantiate a suspicion
// that we may have a failover scenario. we want to speed up reading the complete picture.
func emergentlyReadTopologyInstance(instanceKey *instmodel.InstanceKey, analysisCode analysis.AnalysisCode) (instance *instmodel.Instance, err error) {
	if existsInCacheError := emergencyReadTopologyInstanceMap.Add(instanceKey.StringCode(), true, cache.DefaultExpiration); existsInCacheError != nil {
		// Just recently attempted
		return nil, nil
	}
	instance, err = instdiscovery.ReadTopologyInstance(instanceKey)
	instaudit.AuditOperation("emergently-read-topology-instance", instanceKey, string(analysisCode))
	return instance, err
}

// Force reading of replicas of given instance. This is because we suspect the instance is dead, and want to speed up
// detection of replication failure from its replicas.
func emergentlyReadTopologyInstanceReplicas(instanceKey *instmodel.InstanceKey, analysisCode analysis.AnalysisCode) {
	replicas, err := instinventory.ReadReplicaInstancesIncludingBinlogServerSubReplicas(instanceKey)
	if err != nil {
		return
	}
	for _, replica := range replicas {
		go emergentlyReadTopologyInstance(&replica.Key, analysisCode)
	}
}

// emergentlyRestartReplicationOnTopologyInstance forces a RestartReplication on a given instance.
func emergentlyRestartReplicationOnTopologyInstance(instanceKey *instmodel.InstanceKey, analysisCode analysis.AnalysisCode) {
	if existsInCacheError := emergencyRestartReplicaTopologyInstanceMap.Add(instanceKey.StringCode(), true, cache.DefaultExpiration); existsInCacheError != nil {
		// Just recently attempted on this specific replica
		return
	}

	go instreplication.ExecuteOnTopology(func() {
		instance, _, err := instinventory.ReadInstance(instanceKey)
		if err != nil {
			return
		}
		if instance.UsingMariaDBGTID {
			// In MariaDB GTID, stopping and starting IO thread actually deletes relay logs.
			// This is counter productive to our objective.
			// Specifically, in a situation where the primary is unreachable and where replicas are lagging,
			// we want to restart IO thread to see if lag is actually caused by locked primary. If this results
			// with losing relay logs, then we've lost data.
			// So, unfortunately we avoid this step in MariaDB GTID.
			// See https://github.com/openark/orchestrator/issues/1260
			return
		}

		instreplication.RestartReplicationQuick(instance, instanceKey)
		instaudit.AuditOperation("emergently-restart-replication-topology-instance", instanceKey, string(analysisCode))
	})
}

func beginEmergencyOperationGracefulPeriod(instanceKey *instmodel.InstanceKey) {
	emergencyOperationGracefulPeriodMap.Set(instanceKey.StringCode(), true, cache.DefaultExpiration)
}

func isInEmergencyOperationGracefulPeriod(instanceKey *instmodel.InstanceKey) bool {
	_, found := emergencyOperationGracefulPeriodMap.Get(instanceKey.StringCode())
	return found
}

// emergentlyRestartReplicationOnTopologyInstanceReplicas forces a stop slave + start slave on
// replicas of a given instance, in an attempt to cause them to re-evaluate their replication state.
// This can be useful in scenarios where the master has Too Many Connections, but long-time connected
// replicas are not seeing this; when they stop+start replication, they need to re-authenticate and
// that's where we hope they realize the master is bad.
func emergentlyRestartReplicationOnTopologyInstanceReplicas(instanceKey *instmodel.InstanceKey, analysisCode analysis.AnalysisCode) {
	if existsInCacheError := emergencyRestartReplicaTopologyInstanceMap.Add(instanceKey.StringCode(), true, cache.DefaultExpiration); existsInCacheError != nil {
		// While each replica's RestartReplication() is throttled on its own, it's also wasteful to
		// iterate all replicas all the time. This is the reason why we do grand-throttle check.
		return
	}
	beginEmergencyOperationGracefulPeriod(instanceKey)

	replicas, err := instinventory.ReadReplicaInstancesIncludingBinlogServerSubReplicas(instanceKey)
	if err != nil {
		return
	}
	for _, replica := range replicas {
		replicaKey := &replica.Key
		go emergentlyRestartReplicationOnTopologyInstance(replicaKey, analysisCode)
	}
}

func emergentlyRecordStaleBinlogCoordinates(instanceKey *instmodel.InstanceKey, binlogCoordinates *instmodel.BinlogCoordinates) {
	err := instinventory.RecordStaleInstanceBinlogCoordinates(instanceKey, binlogCoordinates)
	log.Errore(err)
}

func runEmergentOperations(analysisEntry *analysis.ReplicationAnalysis, allowInstanceStateChanges bool) {
	switch analysisEntry.Analysis {
	case analysis.DeadMasterAndReplicas:
		go emergentlyReadTopologyInstance(&analysisEntry.AnalyzedInstanceMasterKey, analysisEntry.Analysis)
	case analysis.UnreachableMaster:
		go emergentlyReadTopologyInstance(&analysisEntry.AnalyzedInstanceKey, analysisEntry.Analysis)
		go emergentlyReadTopologyInstanceReplicas(&analysisEntry.AnalyzedInstanceKey, analysisEntry.Analysis)
	case analysis.LockedSemiSyncMasterHypothesis:
		go emergentlyReadTopologyInstance(&analysisEntry.AnalyzedInstanceKey, analysisEntry.Analysis)
		go emergentlyRecordStaleBinlogCoordinates(&analysisEntry.AnalyzedInstanceKey, &analysisEntry.AnalyzedInstanceBinlogCoordinates)
	case analysis.UnreachableMasterWithLaggingReplicas:
		fallthrough
	case analysis.UnreachableIntermediateMasterWithLaggingReplicas:
		if allowInstanceStateChanges {
			go emergentlyRestartReplicationOnTopologyInstanceReplicas(&analysisEntry.AnalyzedInstanceKey, analysisEntry.Analysis)
		}
	case analysis.AllMasterReplicasNotReplicating:
		go emergentlyReadTopologyInstance(&analysisEntry.AnalyzedInstanceKey, analysisEntry.Analysis)
	case analysis.AllMasterReplicasNotReplicatingOrDead:
		go emergentlyReadTopologyInstance(&analysisEntry.AnalyzedInstanceKey, analysisEntry.Analysis)
	case analysis.FirstTierReplicaFailingToConnectToMaster:
		go emergentlyReadTopologyInstance(&analysisEntry.AnalyzedInstanceMasterKey, analysisEntry.Analysis)
	}
}
