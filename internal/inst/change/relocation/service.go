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

package relocation

import (
	"context"
	"fmt"
	instaudit "github.com/openark/orchestrator/internal/inst/audit"
	instbinlog "github.com/openark/orchestrator/internal/inst/binlog"
	instreplication "github.com/openark/orchestrator/internal/inst/change/replication"
	instcluster "github.com/openark/orchestrator/internal/inst/cluster"
	instdiscovery "github.com/openark/orchestrator/internal/inst/discovery"
	instequivalence "github.com/openark/orchestrator/internal/inst/equivalence"
	instmodel "github.com/openark/orchestrator/internal/inst/instance"
	instinventory "github.com/openark/orchestrator/internal/inst/inventory"
	instmaintenance "github.com/openark/orchestrator/internal/inst/maintenance"
	insttopology "github.com/openark/orchestrator/internal/inst/topology"
	goos "os"
	"strings"
	"sync"
	"time"

	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/golib/log"
	"github.com/openark/orchestrator/internal/inst/gtid"
	modeldomain "github.com/openark/orchestrator/internal/models/domain"
	"github.com/openark/orchestrator/internal/os"
	"github.com/openark/orchestrator/internal/recoverypolicy"
)

var countRetries = 5

// restartReplicationAfterTopologyOperation restores replication without hiding
// either the topology operation's result or a cleanup failure. The primary
// operation error remains externally visible when both operations fail.
func restartReplicationAfterTopologyOperation(instanceKey *instmodel.InstanceKey, instance *instmodel.Instance, operationErr error) (*instmodel.Instance, error) {
	restartedInstance, restartErr := instreplication.StartReplication(instanceKey)
	if restartedInstance != nil {
		instance = restartedInstance
	}
	if restartErr == nil {
		return instance, operationErr
	}
	if operationErr != nil {
		log.Errorf("failed to restart replication on %+v while preserving topology operation error %v: %v", *instanceKey, operationErr, restartErr)
		return instance, operationErr
	}
	return instance, restartErr
}

func shouldPostponeRelocatingReplica(replica *instmodel.Instance, postponedFunctionsContainer *PostponedFunctionsContainer) bool {
	if postponedFunctionsContainer == nil {
		return false
	}
	postponeMinutes := recoverypolicy.Current(replica.ClusterName).PostponeReplicaRecoveryOnLagMinutes
	if postponeMinutes > 0 && replica.SQLDelay > uint(postponeMinutes*60) {
		// This replica is lagging very much, AND
		// we're configured to postpone operation on this replica so as not to delay everyone else.
		return true
	}
	if replica.LastDiscoveryLatency > instmodel.ReasonableDiscoveryLatency {
		return true
	}
	return false
}

// MoveEquivalent will attempt moving instance indicated by instanceKey below another instance,
// based on known master coordinates equivalence
func MoveEquivalent(instanceKey, otherKey *instmodel.InstanceKey) (*instmodel.Instance, error) {
	instance, found, err := instinventory.ReadInstance(instanceKey)
	if err != nil || !found {
		return instance, err
	}
	if instance.Key.Equals(otherKey) {
		return instance, fmt.Errorf("MoveEquivalent: attempt to move an instance below itself %+v", instance.Key)
	}

	// Are there equivalent coordinates to this instance?
	instanceCoordinates := &instequivalence.InstanceBinlogCoordinates{Key: instance.MasterKey, Coordinates: instance.ExecBinlogCoordinates}
	binlogCoordinates, err := instequivalence.GetEquivalentBinlogCoordinatesFor(instanceCoordinates, otherKey)
	if err != nil {
		return instance, err
	}
	if binlogCoordinates == nil {
		return instance, fmt.Errorf("no equivalent coordinates found for %+v replicating from %+v at %+v", instance.Key, instance.MasterKey, instance.ExecBinlogCoordinates)
	}
	// For performance reasons, we did all the above before even checking the replica is stopped or stopping it at all.
	// This allows us to quickly skip the entire operation should there NOT be coordinates.
	// To elaborate: if the replica is actually running AND making progress, it is unlikely/impossible for it to have
	// equivalent coordinates, as the current coordinates are like to have never been seen.
	// This excludes the case, for example, that the master is itself not replicating.
	// Now if we DO get to happen on equivalent coordinates, we need to double check. For CHANGE MASTER to happen we must
	// stop the replica anyhow. But then let's verify the position hasn't changed.
	knownExecBinlogCoordinates := instance.ExecBinlogCoordinates
	instance, err = instreplication.StopReplication(instanceKey)
	if err != nil {
		goto Cleanup
	}
	if !instance.ExecBinlogCoordinates.Equals(&knownExecBinlogCoordinates) {
		// Seems like things were still running... We don't have an equivalence point
		err = fmt.Errorf("MoveEquivalent(): ExecBinlogCoordinates changed after stopping replication on %+v; aborting", instance.Key)
		goto Cleanup
	}
	instance, err = instreplication.ChangeMasterTo(instanceKey, otherKey, binlogCoordinates, false, modeldomain.GTIDHintNeutral)

Cleanup:
	instance, err = restartReplicationAfterTopologyOperation(instanceKey, instance, err)

	if err == nil {
		message := fmt.Sprintf("moved %+v via equivalence coordinates below %+v", *instanceKey, *otherKey)
		log.Debugf("%s", message)
		instaudit.AuditOperation("move-equivalent", instanceKey, message)
	}
	return instance, err
}

// MoveUp will attempt moving instance indicated by instanceKey up the topology hierarchy.
// It will perform all safety and sanity checks and will tamper with this instance's replication
// as well as its master.
func MoveUp(instanceKey *instmodel.InstanceKey) (*instmodel.Instance, error) {
	instance, err := instdiscovery.ReadTopologyInstance(instanceKey)
	if err != nil {
		return instance, err
	}
	if !instance.IsReplica() {
		return instance, fmt.Errorf("instance is not a replica: %+v", instanceKey)
	}
	rinstance, _, _ := instinventory.ReadInstance(&instance.Key)
	if canMove, merr := rinstance.CanMove(); !canMove {
		return instance, merr
	}
	master, err := insttopology.GetInstanceMaster(instance)
	if err != nil {
		return instance, log.Errorf("Cannot insttopology.GetInstanceMaster() for %+v. error=%+v", instance.Key, err)
	}

	if !master.IsReplica() {
		return instance, fmt.Errorf("master is not a replica itself: %+v", master.Key)
	}

	if canReplicate, err := instance.CanReplicateFromEx(master, "MoveUp()"); !canReplicate {
		return instance, err
	}
	if master.IsBinlogServer() {
		// Quick solution via binlog servers
		return Repoint(instanceKey, &master.MasterKey, modeldomain.GTIDHintDeny)
	}

	log.Infof("Will move %+v up the topology", *instanceKey)

	if maintenanceToken, merr := instmaintenance.BeginMaintenance(instanceKey, instmaintenance.GetMaintenanceOwner(), "move up"); merr != nil {
		err = fmt.Errorf("cannot begin maintenance on %+v: %v", *instanceKey, merr)
		goto Cleanup
	} else {
		defer instmaintenance.EndMaintenance(maintenanceToken)
	}
	if maintenanceToken, merr := instmaintenance.BeginMaintenance(&master.Key, instmaintenance.GetMaintenanceOwner(), fmt.Sprintf("child %+v moves up", *instanceKey)); merr != nil {
		err = fmt.Errorf("cannot begin maintenance on %+v: %v", master.Key, merr)
		goto Cleanup
	} else {
		defer instmaintenance.EndMaintenance(maintenanceToken)
	}

	if !instance.UsingMariaDBGTID {
		master, err = instreplication.StopReplication(&master.Key)
		if err != nil {
			goto Cleanup
		}
	}

	instance, err = instreplication.StopReplication(instanceKey)
	if err != nil {
		goto Cleanup
	}

	if !instance.UsingMariaDBGTID {
		_, err = instreplication.StartReplicationUntilMasterCoordinates(instanceKey, &master.SelfBinlogCoordinates)
		if err != nil {
			goto Cleanup
		}
	}

	// We can skip hostname unresolve; we just copy+paste whatever our master thinks of its master.
	instance, err = instreplication.ChangeMasterTo(instanceKey, &master.MasterKey, &master.ExecBinlogCoordinates, true, modeldomain.GTIDHintDeny)
	if err != nil {
		goto Cleanup
	}

Cleanup:
	instance, err = restartReplicationAfterTopologyOperation(instanceKey, instance, err)
	if !instance.UsingMariaDBGTID {
		master, err = restartReplicationAfterTopologyOperation(&master.Key, master, err)
	}
	if err != nil {
		return instance, log.Errore(err)
	}
	// and we're done (pending deferred functions)
	instaudit.AuditOperation("move-up", instanceKey, fmt.Sprintf("moved up %+v. Previous master: %+v", *instanceKey, master.Key))

	return instance, err
}

// MoveUpReplicas will attempt moving up all replicas of a given instance, at the same time.
// Clock-time, this is fater than moving one at a time. However this means all replicas of the given instance, and the instance itself,
// will all stop replicating together.
func MoveUpReplicas(instanceKey *instmodel.InstanceKey, pattern string) ([]*instmodel.Instance, *instmodel.Instance, []error, error) {
	res := []*instmodel.Instance{}
	errs := []error{}
	replicaMutex := make(chan bool, 1)
	var barrier chan *instmodel.InstanceKey

	instance, err := instdiscovery.ReadTopologyInstance(instanceKey)
	if err != nil {
		return res, nil, errs, err
	}
	if !instance.IsReplica() {
		return res, instance, errs, fmt.Errorf("instance is not a replica: %+v", instanceKey)
	}
	_, err = insttopology.GetInstanceMaster(instance)
	if err != nil {
		return res, instance, errs, log.Errorf("Cannot insttopology.GetInstanceMaster() for %+v. error=%+v", instance.Key, err)
	}

	if instance.IsBinlogServer() {
		replicas, operationErrors, err := RepointReplicasTo(instanceKey, pattern, &instance.MasterKey)
		// Bail out!
		return replicas, instance, operationErrors, err
	}

	replicas, err := instinventory.ReadReplicaInstances(instanceKey)
	if err != nil {
		return res, instance, errs, err
	}
	replicas = instmodel.FilterInstancesByPattern(replicas, pattern)
	if len(replicas) == 0 {
		return res, instance, errs, nil
	}
	log.Infof("Will move replicas of %+v up the topology", *instanceKey)

	if maintenanceToken, merr := instmaintenance.BeginMaintenance(instanceKey, instmaintenance.GetMaintenanceOwner(), "move up replicas"); merr != nil {
		err = fmt.Errorf("cannot begin maintenance on %+v: %v", *instanceKey, merr)
		goto Cleanup
	} else {
		defer instmaintenance.EndMaintenance(maintenanceToken)
	}
	for _, replica := range replicas {
		if maintenanceToken, merr := instmaintenance.BeginMaintenance(&replica.Key, instmaintenance.GetMaintenanceOwner(), fmt.Sprintf("%+v moves up", replica.Key)); merr != nil {
			err = fmt.Errorf("cannot begin maintenance on %+v: %v", replica.Key, merr)
			goto Cleanup
		} else {
			defer instmaintenance.EndMaintenance(maintenanceToken)
		}
	}

	instance, err = instreplication.StopReplication(instanceKey)
	if err != nil {
		goto Cleanup
	}

	barrier = make(chan *instmodel.InstanceKey)
	for _, replica := range replicas {
		go func() {
			var replicaErr error
			defer func() {
				defer func() { barrier <- &replica.Key }()
				replica, replicaErr = restartReplicationAfterTopologyOperation(&replica.Key, replica, replicaErr)

				replicaMutex <- true
				defer func() { <-replicaMutex }()
				if replicaErr == nil {
					res = append(res, replica)
				} else {
					errs = append(errs, replicaErr)
				}
			}()

			instreplication.ExecuteOnTopology(func() {
				if canReplicate, err := replica.CanReplicateFromEx(instance, "MoveUpReplicas()"); !canReplicate || err != nil {
					replicaErr = err
					return
				}
				if instance.IsBinlogServer() {
					// Special case. Just repoint
					replica, err = Repoint(&replica.Key, instanceKey, modeldomain.GTIDHintDeny)
					if err != nil {
						replicaErr = err
						return
					}
				} else {
					// Normal case. Do the math.
					replica, err = instreplication.StopReplication(&replica.Key)
					if err != nil {
						replicaErr = err
						return
					}
					replica, err = instreplication.StartReplicationUntilMasterCoordinates(&replica.Key, &instance.SelfBinlogCoordinates)
					if err != nil {
						replicaErr = err
						return
					}

					replica, err = instreplication.ChangeMasterTo(&replica.Key, &instance.MasterKey, &instance.ExecBinlogCoordinates, false, modeldomain.GTIDHintDeny)
					if err != nil {
						replicaErr = err
						return
					}
				}
			})
		}()
	}
	for range replicas {
		<-barrier
	}

Cleanup:
	instance, err = restartReplicationAfterTopologyOperation(instanceKey, instance, err)
	if err != nil {
		return res, instance, errs, log.Errore(err)
	}
	if len(errs) == len(replicas) {
		// All returned with error
		return res, instance, errs, log.Error("Error on all operations")
	}
	instaudit.AuditOperation("move-up-replicas", instanceKey, fmt.Sprintf("moved up %d/%d replicas of %+v. New master: %+v", len(res), len(replicas), *instanceKey, instance.MasterKey))

	return res, instance, errs, err
}

// MoveBelow will attempt moving instance indicated by instanceKey below its supposed sibling indicated by sinblingKey.
// It will perform all safety and sanity checks and will tamper with this instance's replication
// as well as its sibling.
func MoveBelow(instanceKey, siblingKey *instmodel.InstanceKey) (*instmodel.Instance, error) {
	instance, err := instdiscovery.ReadTopologyInstance(instanceKey)
	if err != nil {
		return instance, err
	}
	sibling, err := instdiscovery.ReadTopologyInstance(siblingKey)
	if err != nil {
		return instance, err
	}
	// Relocation of group secondaries makes no sense, group secondaries, by definition, always replicate from the group
	// primary
	if instance.IsReplicationGroupSecondary() {
		return instance, log.Errorf("MoveBelow: %+v is a secondary replication group member, hence, it cannot be relocated", instance.Key)
	}

	if sibling.IsBinlogServer() {
		// Binlog server has same coordinates as master
		// Easy solution!
		return Repoint(instanceKey, &sibling.Key, modeldomain.GTIDHintDeny)
	}

	rinstance, _, _ := instinventory.ReadInstance(&instance.Key)
	if canMove, merr := rinstance.CanMove(); !canMove {
		return instance, merr
	}

	rinstance, _, _ = instinventory.ReadInstance(&sibling.Key)
	if canMove, merr := rinstance.CanMove(); !canMove {
		return instance, merr
	}
	if !insttopology.InstancesAreSiblings(instance, sibling) {
		return instance, fmt.Errorf("instances are not siblings: %+v, %+v", *instanceKey, *siblingKey)
	}

	if canReplicate, err := instance.CanReplicateFromEx(sibling, "MoveBelow()"); !canReplicate {
		return instance, err
	}
	log.Infof("Will move %+v below %+v", instanceKey, siblingKey)

	if maintenanceToken, merr := instmaintenance.BeginMaintenance(instanceKey, instmaintenance.GetMaintenanceOwner(), fmt.Sprintf("move below %+v", *siblingKey)); merr != nil {
		err = fmt.Errorf("cannot begin maintenance on %+v: %v", *instanceKey, merr)
		goto Cleanup
	} else {
		defer instmaintenance.EndMaintenance(maintenanceToken)
	}
	if maintenanceToken, merr := instmaintenance.BeginMaintenance(siblingKey, instmaintenance.GetMaintenanceOwner(), fmt.Sprintf("%+v moves below this", *instanceKey)); merr != nil {
		err = fmt.Errorf("cannot begin maintenance on %+v: %v", *siblingKey, merr)
		goto Cleanup
	} else {
		defer instmaintenance.EndMaintenance(maintenanceToken)
	}

	instance, err = instreplication.StopReplication(instanceKey)
	if err != nil {
		goto Cleanup
	}

	sibling, err = instreplication.StopReplication(siblingKey)
	if err != nil {
		goto Cleanup
	}
	if instance.ExecBinlogCoordinates.SmallerThan(&sibling.ExecBinlogCoordinates) {
		_, err = instreplication.StartReplicationUntilMasterCoordinates(instanceKey, &sibling.ExecBinlogCoordinates)
		if err != nil {
			goto Cleanup
		}
	} else if sibling.ExecBinlogCoordinates.SmallerThan(&instance.ExecBinlogCoordinates) {
		sibling, err = instreplication.StartReplicationUntilMasterCoordinates(siblingKey, &instance.ExecBinlogCoordinates)
		if err != nil {
			goto Cleanup
		}
	}
	// At this point both siblings have executed exact same statements and are identical

	instance, err = instreplication.ChangeMasterTo(instanceKey, &sibling.Key, &sibling.SelfBinlogCoordinates, false, modeldomain.GTIDHintDeny)
	if err != nil {
		goto Cleanup
	}

Cleanup:
	instance, err = restartReplicationAfterTopologyOperation(instanceKey, instance, err)
	_, err = restartReplicationAfterTopologyOperation(siblingKey, sibling, err)

	if err != nil {
		return instance, log.Errore(err)
	}
	// and we're done (pending deferred functions)
	instaudit.AuditOperation("move-below", instanceKey, fmt.Sprintf("moved %+v below %+v", *instanceKey, *siblingKey))

	return instance, err
}

func canReplicateAssumingOracleGTID(instance, masterInstance *instmodel.Instance) (canReplicate bool, err error) {
	subtract, err := instreplication.GTIDSubtract(&instance.Key, masterInstance.GtidPurged, instance.ExecutedGtidSet)
	if err != nil {
		return false, err
	}
	subtractGtidSet, err := gtid.NewOracleGtidSet(subtract)
	if err != nil {
		return false, err
	}
	return subtractGtidSet.IsEmpty(), nil
}

func instancesAreGTIDAndCompatible(instance, otherInstance *instmodel.Instance) (isOracleGTID bool, isMariaDBGTID, compatible bool) {
	isOracleGTID = (instance.UsingOracleGTID && otherInstance.SupportsOracleGTID)
	isMariaDBGTID = (instance.UsingMariaDBGTID && otherInstance.IsMariaDB())
	compatible = isOracleGTID || isMariaDBGTID
	return isOracleGTID, isMariaDBGTID, compatible
}

func CheckMoveViaGTID(instance, otherInstance *instmodel.Instance) (err error) {
	isOracleGTID, _, moveCompatible := instancesAreGTIDAndCompatible(instance, otherInstance)
	if !moveCompatible {
		return fmt.Errorf("instances %+v, %+v not GTID compatible or not using GTID", instance.Key, otherInstance.Key)
	}
	if isOracleGTID {
		canReplicate, err := canReplicateAssumingOracleGTID(instance, otherInstance)
		if err != nil {
			return err
		}
		if !canReplicate {
			return fmt.Errorf("Instance %+v has purged GTID entries not found on %+v", otherInstance.Key, instance.Key)
		}
	}

	return nil
}

// moveInstanceBelowViaGTID will attempt moving given instance below another instance using either Oracle GTID or MariaDB GTID.
func moveInstanceBelowViaGTID(instance, otherInstance *instmodel.Instance) (*instmodel.Instance, error) {
	rinstance, _, _ := instinventory.ReadInstance(&instance.Key)
	if canMove, merr := rinstance.CanMoveViaMatch(); !canMove {
		return instance, merr
	}

	if canReplicate, err := instance.CanReplicateFromEx(otherInstance, "moveInstanceBelowViaGTID()"); !canReplicate {
		return instance, err
	}
	if err := CheckMoveViaGTID(instance, otherInstance); err != nil {
		return instance, err
	}
	log.Infof("Will move %+v below %+v via GTID", instance.Key, otherInstance.Key)

	instanceKey := &instance.Key
	otherInstanceKey := &otherInstance.Key

	var err error
	if maintenanceToken, merr := instmaintenance.BeginMaintenance(instanceKey, instmaintenance.GetMaintenanceOwner(), fmt.Sprintf("move below %+v", *otherInstanceKey)); merr != nil {
		err = fmt.Errorf("cannot begin maintenance on %+v: %v", *instanceKey, merr)
		goto Cleanup
	} else {
		defer instmaintenance.EndMaintenance(maintenanceToken)
	}

	_, err = instreplication.StopReplication(instanceKey)
	if err != nil {
		goto Cleanup
	}

	instance, err = instreplication.ChangeMasterTo(instanceKey, &otherInstance.Key, &otherInstance.SelfBinlogCoordinates, false, modeldomain.GTIDHintForce)
	if err != nil {
		goto Cleanup
	}
Cleanup:
	instance, err = restartReplicationAfterTopologyOperation(instanceKey, instance, err)
	if err != nil {
		return instance, log.Errore(err)
	}
	// and we're done (pending deferred functions)
	instaudit.AuditOperation("move-below-gtid", instanceKey, fmt.Sprintf("moved %+v below %+v", *instanceKey, *otherInstanceKey))

	return instance, err
}

// MoveBelowGTID will attempt moving instance indicated by instanceKey below another instance using either Oracle GTID or MariaDB GTID.
func MoveBelowGTID(instanceKey, otherKey *instmodel.InstanceKey) (*instmodel.Instance, error) {
	instance, err := instdiscovery.ReadTopologyInstance(instanceKey)
	if err != nil {
		return instance, err
	}
	other, err := instdiscovery.ReadTopologyInstance(otherKey)
	if err != nil {
		return instance, err
	}
	// Relocation of group secondaries makes no sense, group secondaries, by definition, always replicate from the group
	// primary
	if instance.IsReplicationGroupSecondary() {
		return instance, log.Errorf("MoveBelowGTID: %+v is a secondary replication group member, hence, it cannot be relocated", instance.Key)
	}
	return moveInstanceBelowViaGTID(instance, other)
}

// MoveReplicasViaGTID moves a list of replicas under another instance via GTID, returning those replicas
// that could not be moved (do not use GTID or had GTID errors)
func MoveReplicasViaGTID(replicas []*instmodel.Instance, other *instmodel.Instance, postponedFunctionsContainer *PostponedFunctionsContainer) (movedReplicas []*instmodel.Instance, unmovedReplicas []*instmodel.Instance, errs []error, err error) {
	replicas = instmodel.RemoveNilInstances(replicas)
	replicas = instmodel.RemoveInstance(replicas, &other.Key)
	if len(replicas) == 0 {
		// Nothing to do
		return movedReplicas, unmovedReplicas, errs, nil
	}

	log.Infof("moveReplicasViaGTID: Will move %+v replicas below %+v via GTID, max concurrency: %v",
		len(replicas),
		other.Key,
		config.Config.Topology.Operations.MaxConcurrentReplicaOperations)

	var waitGroup sync.WaitGroup
	var replicaMutex sync.Mutex

	var concurrencyChan = make(chan bool, config.Config.Topology.Operations.MaxConcurrentReplicaOperations)

	for _, replica := range replicas {

		// Parallelize repoints
		waitGroup.Go(func() {
			moveFunc := func() error {

				concurrencyChan <- true
				defer func() { recover(); <-concurrencyChan }()

				movedReplica, replicaErr := moveInstanceBelowViaGTID(replica, other)
				if replicaErr != nil && movedReplica != nil {
					replica = movedReplica
				}

				// After having moved replicas, update local shared variables:
				replicaMutex.Lock()
				defer replicaMutex.Unlock()

				if replicaErr == nil {
					movedReplicas = append(movedReplicas, replica)
				} else {
					unmovedReplicas = append(unmovedReplicas, replica)
					errs = append(errs, replicaErr)
				}
				return replicaErr
			}
			if shouldPostponeRelocatingReplica(replica, postponedFunctionsContainer) {
				postponedFunctionsContainer.AddPostponedFunction(moveFunc, fmt.Sprintf("move-replicas-gtid %+v", replica.Key))
				// We bail out and trust our invoker to later call upon this postponed function
			} else {
				instreplication.ExecuteOnTopology(func() { moveFunc() })
			}
		})
	}
	waitGroup.Wait()

	if len(errs) == len(replicas) {
		// All returned with error
		return movedReplicas, unmovedReplicas, errs, fmt.Errorf("moveReplicasViaGTID: Error on all %+v operations", len(errs))
	}
	instaudit.AuditOperation("move-replicas-gtid", &other.Key, fmt.Sprintf("moved %d/%d replicas below %+v via GTID", len(movedReplicas), len(replicas), other.Key))

	return movedReplicas, unmovedReplicas, errs, err
}

// MoveReplicasGTID will (attempt to) move all replicas of given master below given instance.
func MoveReplicasGTID(masterKey *instmodel.InstanceKey, belowKey *instmodel.InstanceKey, pattern string) (movedReplicas []*instmodel.Instance, unmovedReplicas []*instmodel.Instance, errs []error, err error) {
	belowInstance, err := instdiscovery.ReadTopologyInstance(belowKey)
	if err != nil {
		// Can't access "below" ==> can't move replicas beneath it
		return movedReplicas, unmovedReplicas, errs, err
	}

	// replicas involved
	replicas, err := instinventory.ReadReplicaInstancesIncludingBinlogServerSubReplicas(masterKey)
	if err != nil {
		return movedReplicas, unmovedReplicas, errs, err
	}
	replicas = instmodel.FilterInstancesByPattern(replicas, pattern)
	movedReplicas, unmovedReplicas, errs, err = MoveReplicasViaGTID(replicas, belowInstance, nil)
	if err != nil {
		log.Errore(err)
	}

	if len(unmovedReplicas) > 0 {
		err = fmt.Errorf("MoveReplicasGTID: only moved %d out of %d replicas of %+v; error is: %+v", len(movedReplicas), len(replicas), *masterKey, err)
	}

	return movedReplicas, unmovedReplicas, errs, err
}

// Repoint connects a replica to a master using its exact same executing coordinates.
// The given masterKey can be null, in which case the existing master is used.
// Two use cases:
// - masterKey is nil: use case is corrupted relay logs on replica
// - masterKey is not nil: using Binlog servers (coordinates remain the same)
func Repoint(instanceKey *instmodel.InstanceKey, masterKey *instmodel.InstanceKey, gtidHint modeldomain.OperationGTIDHint) (*instmodel.Instance, error) {
	instance, err := instdiscovery.ReadTopologyInstance(instanceKey)
	if err != nil {
		return instance, err
	}
	if !instance.IsReplica() {
		return instance, fmt.Errorf("instance is not a replica: %+v", *instanceKey)
	}
	// Relocation of group secondaries makes no sense, group secondaries, by definition, always replicate from the group
	// primary
	if instance.IsReplicationGroupSecondary() {
		return instance, fmt.Errorf("repoint: %+v is a secondary replication group member, hence, it cannot be relocated", instance.Key)
	}
	if masterKey == nil {
		masterKey = &instance.MasterKey
	}
	// With repoint we *prefer* the master to be alive, but we don't strictly require it.
	// The use case for the master being alive is with hostname-resolve or hostname-unresolve: asking the replica
	// to reconnect to its same master while changing the MASTER_HOST in CHANGE MASTER TO due to DNS changes etc.
	master, err := instdiscovery.ReadTopologyInstance(masterKey)
	masterIsAccessible := (err == nil)
	if !masterIsAccessible {
		master, _, err = instinventory.ReadInstance(masterKey)
		if master == nil || err != nil {
			return instance, err
		}
	}
	if canReplicate, err := instance.CanReplicateFromEx(master, "Repoint()"); !canReplicate {
		return instance, err
	}

	// if a binlog server check it is sufficiently up to date
	if master.IsBinlogServer() {
		// "Repoint" operation trusts the user. But only so much. Repoiting to a binlog server which is not yet there is strictly wrong.
		if !instance.ExecBinlogCoordinates.SmallerThanOrEquals(&master.SelfBinlogCoordinates) {
			return instance, fmt.Errorf("repoint: binlog server %+v is not sufficiently up to date to repoint %+v below it", *masterKey, *instanceKey)
		}
	}

	log.Infof("Will repoint %+v to master %+v", *instanceKey, *masterKey)

	if maintenanceToken, merr := instmaintenance.BeginMaintenance(instanceKey, instmaintenance.GetMaintenanceOwner(), "repoint"); merr != nil {
		err = fmt.Errorf("cannot begin maintenance on %+v: %v", *instanceKey, merr)
		goto Cleanup
	} else {
		defer instmaintenance.EndMaintenance(maintenanceToken)
	}

	instance, err = instreplication.StopReplication(instanceKey)
	if err != nil {
		goto Cleanup
	}

	// See above, we are relaxed about the master being accessible/inaccessible.
	// If accessible, we wish to do hostname-unresolve. If inaccessible, we can skip the test and not fail the
	// ChangeMasterTo operation. This is why we pass "!masterIsAccessible" below.
	if instance.ExecBinlogCoordinates.IsEmpty() {
		instance.ExecBinlogCoordinates.LogFile = "orchestrator-unknown-log-file"
	}
	instance, err = instreplication.ChangeMasterTo(instanceKey, masterKey, &instance.ExecBinlogCoordinates, !masterIsAccessible, gtidHint)
	if err != nil {
		goto Cleanup
	}

Cleanup:
	instance, err = restartReplicationAfterTopologyOperation(instanceKey, instance, err)
	if err != nil {
		return instance, log.Errore(err)
	}
	// and we're done (pending deferred functions)
	instaudit.AuditOperation("repoint", instanceKey, fmt.Sprintf("replica %+v repointed to master: %+v", *instanceKey, *masterKey))

	return instance, err

}

// RepointTo repoints list of replicas onto another master.
// Binlog Server is the major use case
func RepointTo(replicas []*instmodel.Instance, belowKey *instmodel.InstanceKey) ([]*instmodel.Instance, []error, error) {
	res := []*instmodel.Instance{}
	errs := []error{}

	replicas = instmodel.RemoveInstance(replicas, belowKey)
	if len(replicas) == 0 {
		// Nothing to do
		return res, errs, nil
	}
	if belowKey == nil {
		return res, errs, log.Errorf("RepointTo received nil belowKey")
	}

	log.Infof("Will repoint %+v replicas below %+v", len(replicas), *belowKey)
	barrier := make(chan *instmodel.InstanceKey)
	replicaMutex := make(chan bool, 1)
	for _, replica := range replicas {

		// Parallelize repoints
		go func() {
			defer func() { barrier <- &replica.Key }()
			instreplication.ExecuteOnTopology(func() {
				replica, replicaErr := Repoint(&replica.Key, belowKey, modeldomain.GTIDHintNeutral)

				func() {
					// Instantaneous mutex.
					replicaMutex <- true
					defer func() { <-replicaMutex }()
					if replicaErr == nil {
						res = append(res, replica)
					} else {
						errs = append(errs, replicaErr)
					}
				}()
			})
		}()
	}
	for range replicas {
		<-barrier
	}

	if len(errs) == len(replicas) {
		// All returned with error
		return res, errs, log.Error("Error on all operations")
	}
	instaudit.AuditOperation("repoint-to", belowKey, fmt.Sprintf("repointed %d/%d replicas to %+v", len(res), len(replicas), *belowKey))

	return res, errs, nil
}

// RepointReplicasTo repoints replicas of a given instance (possibly filtered) onto another master.
// Binlog Server is the major use case
func RepointReplicasTo(instanceKey *instmodel.InstanceKey, pattern string, belowKey *instmodel.InstanceKey) ([]*instmodel.Instance, []error, error) {
	res := []*instmodel.Instance{}
	errs := []error{}

	replicas, err := instinventory.ReadReplicaInstances(instanceKey)
	if err != nil {
		return res, errs, err
	}
	replicas = instmodel.RemoveInstance(replicas, belowKey)
	replicas = instmodel.FilterInstancesByPattern(replicas, pattern)
	if len(replicas) == 0 {
		// Nothing to do
		return res, errs, nil
	}
	if belowKey == nil {
		// Default to existing master. All replicas are of the same master, hence just pick one.
		belowKey = &replicas[0].MasterKey
	}
	log.Infof("Will repoint replicas of %+v to %+v", *instanceKey, *belowKey)
	return RepointTo(replicas, belowKey)
}

// RepointReplicas repoints all replicas of a given instance onto its existing master.
func RepointReplicas(instanceKey *instmodel.InstanceKey, pattern string) ([]*instmodel.Instance, []error, error) {
	return RepointReplicasTo(instanceKey, pattern, nil)
}

// MakeCoMaster will attempt to make an instance co-master with its master, by making its master a replica of its own.
// This only works out if the master is not replicating; the master does not have a known master (it may have an unknown master).
func MakeCoMaster(instanceKey *instmodel.InstanceKey) (*instmodel.Instance, error) {
	instance, err := instdiscovery.ReadTopologyInstance(instanceKey)
	if err != nil {
		return instance, err
	}
	if canMove, merr := instance.CanMove(); !canMove {
		return instance, merr
	}
	master, err := insttopology.GetInstanceMaster(instance)
	if err != nil {
		return instance, err
	}
	// Relocation of group secondaries makes no sense, group secondaries, by definition, always replicate from the group
	// primary
	if instance.IsReplicationGroupSecondary() {
		return instance, fmt.Errorf("MakeCoMaster: %+v is a secondary replication group member, hence, it cannot be relocated", instance.Key)
	}
	log.Debugf("Will check whether %+v's master (%+v) can become its co-master", instance.Key, master.Key)
	if canMove, merr := master.CanMoveAsCoMaster(); !canMove {
		return instance, merr
	}
	if instanceKey.Equals(&master.MasterKey) {
		return instance, fmt.Errorf("instance %+v is already co master of %+v", instance.Key, master.Key)
	}
	if !instance.ReadOnly {
		return instance, fmt.Errorf("instance %+v is not read-only; first make it read-only before making it co-master", instance.Key)
	}
	if master.IsCoMaster {
		// We allow breaking of an existing co-master replication. Here's the breakdown:
		// Ideally, this would not eb allowed, and we would first require the user to RESET SLAVE on 'master'
		// prior to making it participate as co-master with our 'instance'.
		// However there's the problem that upon RESET SLAVE we lose the replication's user/password info.
		// Thus, we come up with the following rule:
		// If S replicates from M1, and M1<->M2 are co masters, we allow S to become co-master of M1 (S<->M1) if:
		// - M1 is writeable
		// - M2 is read-only or is unreachable/invalid
		// - S  is read-only
		// And so we will be replacing one read-only co-master with another.
		otherCoMaster, found, _ := instinventory.ReadInstance(&master.MasterKey)
		if found && otherCoMaster.IsLastCheckValid && !otherCoMaster.ReadOnly {
			return instance, fmt.Errorf("master %+v is already co-master with %+v, and %+v is alive, and not read-only; cowardly refusing to demote it. Please set it as read-only beforehand", master.Key, otherCoMaster.Key, otherCoMaster.Key)
		}
		// OK, good to go.
	} else if _, found, _ := instinventory.ReadInstance(&master.MasterKey); found {
		return instance, fmt.Errorf("%+v is not a real master; it replicates from: %+v", master.Key, master.MasterKey)
	}
	if canReplicate, err := master.CanReplicateFromEx(instance, "MakeCoMaster()"); !canReplicate {
		return instance, err
	}
	log.Infof("Will make %+v co-master of %+v", instanceKey, master.Key)

	var gitHint modeldomain.OperationGTIDHint = modeldomain.GTIDHintNeutral
	if maintenanceToken, merr := instmaintenance.BeginMaintenance(instanceKey, instmaintenance.GetMaintenanceOwner(), fmt.Sprintf("make co-master of %+v", master.Key)); merr != nil {
		err = fmt.Errorf("cannot begin maintenance on %+v: %v", *instanceKey, merr)
		goto Cleanup
	} else {
		defer instmaintenance.EndMaintenance(maintenanceToken)
	}
	if maintenanceToken, merr := instmaintenance.BeginMaintenance(&master.Key, instmaintenance.GetMaintenanceOwner(), fmt.Sprintf("%+v turns into co-master of this", *instanceKey)); merr != nil {
		err = fmt.Errorf("cannot begin maintenance on %+v: %v", master.Key, merr)
		goto Cleanup
	} else {
		defer instmaintenance.EndMaintenance(maintenanceToken)
	}

	// the coMaster used to be merely a replica. Just point master into *some* position
	// within coMaster...
	if master.IsReplica() {
		// this is the case of a co-master. For masters, the StopReplication operation throws an error, and
		// there's really no point in doing it.
		master, err = instreplication.StopReplication(&master.Key)
		if err != nil {
			goto Cleanup
		}
	}
	if !master.HasReplicationCredentials {
		// Let's try , if possible, to get credentials from replica. Best effort.
		if credentials, credentialsErr := instreplication.ReadReplicationCredentials(&instance.Key); credentialsErr == nil {
			log.Debugf("Got credentials from a replica. will now apply")
			_, err = instreplication.ChangeMasterCredentials(&master.Key, credentials)
			if err != nil {
				goto Cleanup
			}
		}
	}

	if !instance.AllowTLS {
		_, err = instreplication.EnableMasterGetSourcePublicKey(&master.Key)
		if err != nil {
			goto Cleanup
		}
	}

	if instance.UsingOracleGTID {
		gitHint = modeldomain.GTIDHintForce
	}
	master, err = instreplication.ChangeMasterTo(&master.Key, instanceKey, &instance.SelfBinlogCoordinates, false, gitHint)
	if err != nil {
		goto Cleanup
	}

Cleanup:
	master, err = restartReplicationAfterTopologyOperation(&master.Key, master, err)
	if err != nil {
		return instance, log.Errore(err)
	}
	// and we're done (pending deferred functions)
	instaudit.AuditOperation("make-co-master", instanceKey, fmt.Sprintf("%+v made co-master of %+v", *instanceKey, master.Key))

	return instance, err
}

// ResetReplicationOperation will reset a replica
func ResetReplicationOperation(instanceKey *instmodel.InstanceKey) (*instmodel.Instance, error) {
	instance, err := instdiscovery.ReadTopologyInstance(instanceKey)
	if err != nil {
		return instance, err
	}

	log.Infof("Will reset replica on %+v", instanceKey)

	if maintenanceToken, merr := instmaintenance.BeginMaintenance(instanceKey, instmaintenance.GetMaintenanceOwner(), "reset replica"); merr != nil {
		err = fmt.Errorf("cannot begin maintenance on %+v: %v", *instanceKey, merr)
		goto Cleanup
	} else {
		defer instmaintenance.EndMaintenance(maintenanceToken)
	}

	if instance.IsReplica() {
		_, err = instreplication.StopReplication(instanceKey)
		if err != nil {
			goto Cleanup
		}
	}

	instance, err = instreplication.ResetReplication(instanceKey)
	if err != nil {
		goto Cleanup
	}

Cleanup:
	if err != nil {
		instance, err = restartReplicationAfterTopologyOperation(instanceKey, instance, err)
	}

	if err != nil {
		return instance, log.Errore(err)
	}

	// and we're done (pending deferred functions)
	instaudit.AuditOperation("reset-slave", instanceKey, fmt.Sprintf("%+v replication reset", *instanceKey))

	return instance, err
}

// DetachReplicaMasterHost detaches a replica from its master by corrupting the Master_Host (in such way that is reversible)
func DetachReplicaMasterHost(instanceKey *instmodel.InstanceKey) (*instmodel.Instance, error) {
	instance, err := instdiscovery.ReadTopologyInstance(instanceKey)
	if err != nil {
		return instance, err
	}
	if !instance.IsReplica() {
		return instance, fmt.Errorf("instance is not a replica: %+v", *instanceKey)
	}
	if instance.MasterKey.IsDetached() {
		return instance, fmt.Errorf("instance already detached: %+v", *instanceKey)
	}
	detachedMasterKey := instance.MasterKey.DetachedKey()

	log.Infof("Will detach master host on %+v. Detached key is %+v", *instanceKey, *detachedMasterKey)

	if maintenanceToken, merr := instmaintenance.BeginMaintenance(instanceKey, instmaintenance.GetMaintenanceOwner(), "detach-replica-master-host"); merr != nil {
		err = fmt.Errorf("cannot begin maintenance on %+v: %v", *instanceKey, merr)
		goto Cleanup
	} else {
		defer instmaintenance.EndMaintenance(maintenanceToken)
	}

	instance, err = instreplication.StopReplication(instanceKey)
	if err != nil {
		goto Cleanup
	}

	instance, err = instreplication.ChangeMasterTo(instanceKey, detachedMasterKey, &instance.ExecBinlogCoordinates, true, modeldomain.GTIDHintNeutral)
	if err != nil {
		goto Cleanup
	}

Cleanup:
	instance, err = restartReplicationAfterTopologyOperation(instanceKey, instance, err)
	if err != nil {
		return instance, log.Errore(err)
	}
	// and we're done (pending deferred functions)
	instaudit.AuditOperation("repoint", instanceKey, fmt.Sprintf("replica %+v detached from master into %+v", *instanceKey, *detachedMasterKey))

	return instance, err
}

// ReattachReplicaMasterHost reattaches a replica back onto its master by undoing a DetachReplicaMasterHost operation
func ReattachReplicaMasterHost(instanceKey *instmodel.InstanceKey) (*instmodel.Instance, error) {
	instance, err := instdiscovery.ReadTopologyInstance(instanceKey)
	if err != nil {
		return instance, err
	}
	if !instance.IsReplica() {
		return instance, fmt.Errorf("instance is not a replica: %+v", *instanceKey)
	}
	if !instance.MasterKey.IsDetached() {
		return instance, fmt.Errorf("instance does not seem to be detached: %+v", *instanceKey)
	}

	reattachedMasterKey := instance.MasterKey.ReattachedKey()

	log.Infof("Will reattach master host on %+v. Reattached key is %+v", *instanceKey, *reattachedMasterKey)

	if maintenanceToken, merr := instmaintenance.BeginMaintenance(instanceKey, instmaintenance.GetMaintenanceOwner(), "reattach-replica-master-host"); merr != nil {
		err = fmt.Errorf("cannot begin maintenance on %+v: %v", *instanceKey, merr)
		goto Cleanup
	} else {
		defer instmaintenance.EndMaintenance(maintenanceToken)
	}

	instance, err = instreplication.StopReplication(instanceKey)
	if err != nil {
		goto Cleanup
	}

	instance, err = instreplication.ChangeMasterTo(instanceKey, reattachedMasterKey, &instance.ExecBinlogCoordinates, true, modeldomain.GTIDHintNeutral)
	if err != nil {
		goto Cleanup
	}
	// Just in case this instance used to be a master:
	instcluster.ReplaceAliasClusterName(instanceKey.StringCode(), reattachedMasterKey.StringCode())

Cleanup:
	instance, err = restartReplicationAfterTopologyOperation(instanceKey, instance, err)
	if err != nil {
		return instance, log.Errore(err)
	}
	// and we're done (pending deferred functions)
	instaudit.AuditOperation("repoint", instanceKey, fmt.Sprintf("replica %+v reattached to master %+v", *instanceKey, *reattachedMasterKey))

	return instance, err
}

// EnableGTID will attempt to enable GTID-mode (either Oracle or MariaDB)
func EnableGTID(instanceKey *instmodel.InstanceKey) (*instmodel.Instance, error) {
	instance, err := instdiscovery.ReadTopologyInstance(instanceKey)
	if err != nil {
		return instance, err
	}
	if instance.UsingGTID() {
		return instance, fmt.Errorf("%+v already uses GTID", *instanceKey)
	}

	log.Infof("Will attempt to enable GTID on %+v", *instanceKey)

	instance, err = Repoint(instanceKey, nil, modeldomain.GTIDHintForce)
	if err != nil {
		return instance, err
	}
	if !instance.UsingGTID() {
		return instance, fmt.Errorf("cannot enable GTID on %+v", *instanceKey)
	}

	instaudit.AuditOperation("enable-gtid", instanceKey, fmt.Sprintf("enabled GTID on %+v", *instanceKey))

	return instance, err
}

// DisableGTID will attempt to disable GTID-mode (either Oracle or MariaDB) and revert to binlog file:pos replication
func DisableGTID(instanceKey *instmodel.InstanceKey) (*instmodel.Instance, error) {
	instance, err := instdiscovery.ReadTopologyInstance(instanceKey)
	if err != nil {
		return instance, err
	}
	if !instance.UsingGTID() {
		return instance, fmt.Errorf("%+v is not using GTID", *instanceKey)
	}

	log.Infof("Will attempt to disable GTID on %+v", *instanceKey)

	instance, err = Repoint(instanceKey, nil, modeldomain.GTIDHintDeny)
	if err != nil {
		return instance, err
	}
	if instance.UsingGTID() {
		return instance, fmt.Errorf("cannot disable GTID on %+v", *instanceKey)
	}

	instaudit.AuditOperation("disable-gtid", instanceKey, fmt.Sprintf("disabled GTID on %+v", *instanceKey))

	return instance, err
}

func LocateErrantGTID(instanceKey *instmodel.InstanceKey) (errantBinlogs []string, err error) {
	instance, err := instdiscovery.ReadTopologyInstance(instanceKey)
	if err != nil {
		return errantBinlogs, err
	}
	errantSearch := instance.GtidErrant
	if errantSearch == "" {
		return errantBinlogs, log.Errorf("locate-errant-gtid: no errant-gtid on %+v", *instanceKey)
	}
	subtract, err := instreplication.GTIDSubtract(instanceKey, errantSearch, instance.GtidPurged)
	if err != nil {
		return errantBinlogs, err
	}
	if subtract != errantSearch {
		return errantBinlogs, fmt.Errorf("locate-errant-gtid: %+v is already purged on %+v", subtract, *instanceKey)
	}
	binlogs, err := instreplication.ShowBinaryLogs(instanceKey)
	if err != nil {
		return errantBinlogs, err
	}
	previousGTIDs := make(map[string]*gtid.OracleGtidSet)
	for _, binlog := range binlogs {
		oracleGTIDSet, err := instbinlog.GetPreviousGTIDs(instanceKey, binlog)
		if err != nil {
			return errantBinlogs, err
		}
		previousGTIDs[binlog] = oracleGTIDSet
	}
	for i, binlog := range binlogs {
		if errantSearch == "" {
			break
		}
		previousGTID := previousGTIDs[binlog]
		subtract, err := instreplication.GTIDSubtract(instanceKey, errantSearch, previousGTID.String())
		if err != nil {
			return errantBinlogs, err
		}
		if subtract != errantSearch {
			// binlogs[i-1] is safe to use when i==0. because that implies GTIDs have been purged,
			// which covered by an earlier assertion
			errantBinlogs = append(errantBinlogs, binlogs[i-1])
			errantSearch = subtract
		}
	}
	if errantSearch != "" {
		// then it's in the last binary log
		errantBinlogs = append(errantBinlogs, binlogs[len(binlogs)-1])
	}
	return errantBinlogs, err
}

// ErrantGTIDResetMaster will issue a safe RESET MASTER on a replica that replicates via GTID:
// It will make sure the gtid_purged set matches the executed set value as read just before the RESET.
// this will enable new replicas to be attached to given instance without complaints about missing/purged entries.
// This function requires that the instance does not have replicas.
func ErrantGTIDResetMaster(instanceKey *instmodel.InstanceKey) (instance *instmodel.Instance, err error) {
	instance, err = instdiscovery.ReadTopologyInstance(instanceKey)
	if err != nil {
		return instance, err
	}
	if instance.GtidErrant == "" {
		return instance, log.Errorf("gtid-errant-reset-master will not operate on %+v because no errant GTID is found", *instanceKey)
	}
	if !instance.SupportsOracleGTID {
		return instance, log.Errorf("gtid-errant-reset-master requested for %+v but it is not using oracle-gtid", *instanceKey)
	}
	if len(instance.Replicas) > 0 {
		return instance, log.Errorf("gtid-errant-reset-master will not operate on %+v because it has %+v replicas. Expecting no replicas", *instanceKey, len(instance.Replicas))
	}

	gtidSubtract := ""
	executedGtidSet := ""
	masterStatusFound := false
	replicationStopped := false
	waitInterval := time.Second * 5

	if maintenanceToken, merr := instmaintenance.BeginMaintenance(instanceKey, instmaintenance.GetMaintenanceOwner(), "reset-master-gtid"); merr != nil {
		err = fmt.Errorf("cannot begin maintenance on %+v: %v", *instanceKey, merr)
		goto Cleanup
	} else {
		defer instmaintenance.EndMaintenance(maintenanceToken)
	}

	if instance.IsReplica() {
		instance, err = instreplication.StopReplication(instanceKey)
		if err != nil {
			goto Cleanup
		}
		replicationStopped, err = instreplication.WaitForReplicationState(instance, instanceKey, instmodel.ReplicationThreadStateStopped)
		if err != nil {
			goto Cleanup
		}
		if !replicationStopped {
			err = fmt.Errorf("gtid-errant-reset-master: timeout while waiting for replication to stop on %+v", instance.Key)
			goto Cleanup
		}
	}

	gtidSubtract, err = instreplication.GTIDSubtract(instanceKey, instance.ExecutedGtidSet, instance.GtidErrant)
	if err != nil {
		goto Cleanup
	}

	// We're about to perform a destructive operation. It is non transactional and cannot be rolled back.
	// The replica will be left in a broken state.
	// This is why we allow multiple attempts at the following:
	for range countRetries {
		instance, err = instreplication.ResetMaster(instanceKey)
		if err == nil {
			break
		}
		time.Sleep(waitInterval)
	}
	if err != nil {
		err = fmt.Errorf("gtid-errant-reset-master: error while resetting master on %+v, after which intended to set gtid_purged to: %s. Error was: %+v", instance.Key, gtidSubtract, err)
		goto Cleanup
	}

	masterStatusFound, executedGtidSet, err = instreplication.ShowMasterStatus(instance, instanceKey)
	if err != nil {
		err = fmt.Errorf("gtid-errant-reset-master: error getting master status on %+v, after which intended to set gtid_purged to: %s. Error was: %+v", instance.Key, gtidSubtract, err)
		goto Cleanup
	}
	if !masterStatusFound {
		err = fmt.Errorf("gtid-errant-reset-master: cannot get master status on %+v, after which intended to set gtid_purged to: %s", instance.Key, gtidSubtract)
		goto Cleanup
	}
	if executedGtidSet != "" {
		err = fmt.Errorf("gtid-errant-reset-master: Unexpected non-empty Executed_Gtid_Set found on %+v following RESET MASTER, after which intended to set gtid_purged to: %s. Executed_Gtid_Set found to be: %+v", instance.Key, gtidSubtract, executedGtidSet)
		goto Cleanup
	}

	// We've just made the destructive operation. Again, allow for retries:
	for range countRetries {
		err = instreplication.SetGTIDPurged(instance, gtidSubtract)
		if err == nil {
			break
		}
		time.Sleep(waitInterval)
	}
	if err != nil {
		err = fmt.Errorf("gtid-errant-reset-master: error setting gtid_purged on %+v to: %s. Error was: %+v", instance.Key, gtidSubtract, err)
		goto Cleanup
	}

Cleanup:
	instance, err = restartReplicationAfterTopologyOperation(instanceKey, instance, err)

	if err != nil {
		return instance, log.Errore(err)
	}

	// and we're done (pending deferred functions)
	instaudit.AuditOperation("gtid-errant-reset-master", instanceKey, fmt.Sprintf("%+v master reset", *instanceKey))

	return instance, err
}

// ErrantGTIDInjectEmpty will inject an empty transaction on the master of an instance's cluster in order to get rid
// of an errant transaction observed on the instance.
func ErrantGTIDInjectEmpty(instanceKey *instmodel.InstanceKey) (instance *instmodel.Instance, clusterMaster *instmodel.Instance, countInjectedTransactions int64, err error) {
	instance, err = instdiscovery.ReadTopologyInstance(instanceKey)
	if err != nil {
		return instance, clusterMaster, countInjectedTransactions, err
	}
	if instance.GtidErrant == "" {
		return instance, clusterMaster, countInjectedTransactions, log.Errorf("gtid-errant-inject-empty will not operate on %+v because no errant GTID is found", *instanceKey)
	}
	if !instance.SupportsOracleGTID {
		return instance, clusterMaster, countInjectedTransactions, log.Errorf("gtid-errant-inject-empty requested for %+v but it does not support oracle-gtid", *instanceKey)
	}

	masters, err := instinventory.ReadClusterWriteableMaster(instance.ClusterName)
	if err != nil {
		return instance, clusterMaster, countInjectedTransactions, err
	}
	if len(masters) == 0 {
		return instance, clusterMaster, countInjectedTransactions, log.Errorf("gtid-errant-inject-empty found no writabel master for %+v cluster", instance.ClusterName)
	}
	clusterMaster = masters[0]

	if !clusterMaster.SupportsOracleGTID {
		return instance, clusterMaster, countInjectedTransactions, log.Errorf("gtid-errant-inject-empty requested for %+v but the cluster's master %+v does not support oracle-gtid", *instanceKey, clusterMaster.Key)
	}

	gtidSet, err := gtid.NewOracleGtidSet(instance.GtidErrant)
	if err != nil {
		return instance, clusterMaster, countInjectedTransactions, err
	}
	explodedEntries := gtidSet.Explode()
	log.Infof("gtid-errant-inject-empty: about to inject %+v empty transactions %+v on cluster master %+v", len(explodedEntries), gtidSet.String(), clusterMaster.Key)
	for _, entry := range explodedEntries {
		if err := instreplication.InjectEmptyGTIDTransaction(&clusterMaster.Key, entry); err != nil {
			return instance, clusterMaster, countInjectedTransactions, err
		}
		countInjectedTransactions++
	}

	// and we're done (pending deferred functions)
	instaudit.AuditOperation("gtid-errant-inject-empty", instanceKey, fmt.Sprintf("injected %+v empty transactions on %+v", countInjectedTransactions, clusterMaster.Key))

	return instance, clusterMaster, countInjectedTransactions, err
}

// FindLastPseudoGTIDEntry will search an instance's binary logs or relay logs for the last pseudo-GTID entry,
// and return found coordinates as well as entry text
func FindLastPseudoGTIDEntry(instance *instmodel.Instance, recordedInstanceRelayLogCoordinates instmodel.BinlogCoordinates, maxBinlogCoordinates *instmodel.BinlogCoordinates, exhaustiveSearch bool, expectedBinlogFormat *string) (instancePseudoGtidCoordinates *instmodel.BinlogCoordinates, instancePseudoGtidText string, err error) {

	if config.Config.PseudoGTID.Pattern == "" {
		return instancePseudoGtidCoordinates, instancePseudoGtidText, fmt.Errorf("PseudoGTIDPattern not configured; cannot use Pseudo-GTID")
	}

	if instance.LogBinEnabled && instance.LogReplicationUpdatesEnabled && !*config.RuntimeCLIFlags.SkipBinlogSearch && (expectedBinlogFormat == nil || instance.Binlog_format == *expectedBinlogFormat) {
		minBinlogCoordinates, _, _ := instinventory.GetHeuristiclyRecentCoordinatesForInstance(&instance.Key)
		// Well no need to search this instance's binary logs if it doesn't have any...
		// With regard log-slave-updates, some edge cases are possible, like having this instance's log-slave-updates
		// enabled/disabled (of course having restarted it)
		// The approach is not to take chances. If log-slave-updates is disabled, fail and go for relay-logs.
		// If log-slave-updates was just enabled then possibly no pseudo-gtid is found, and so again we will go
		// for relay logs.
		// Also, if master has STATEMENT binlog format, and the replica has ROW binlog format, then comparing binlog entries would urely fail if based on the replica's binary logs.
		// Instead, we revert to the relay logs.
		instancePseudoGtidCoordinates, instancePseudoGtidText, err = instbinlog.GetLastPseudoGTIDEntryInInstance(instance, minBinlogCoordinates, maxBinlogCoordinates, exhaustiveSearch)
	}
	if err != nil || instancePseudoGtidCoordinates == nil {
		minRelaylogCoordinates, _ := instinventory.GetPreviousKnownRelayLogCoordinatesForInstance(instance)
		// Unable to find pseudo GTID in binary logs.
		// Then MAYBE we are lucky enough (chances are we are, if this replica did not crash) that we can
		// extract the Pseudo GTID entry from the last (current) relay log file.
		instancePseudoGtidCoordinates, instancePseudoGtidText, err = instbinlog.GetLastPseudoGTIDEntryInRelayLogs(instance, minRelaylogCoordinates, recordedInstanceRelayLogCoordinates, exhaustiveSearch)
	}
	return instancePseudoGtidCoordinates, instancePseudoGtidText, err
}

// CorrelateBinlogCoordinates find out, if possible, the binlog coordinates of given otherInstance that correlate
// with given coordinates of given instance.
func CorrelateBinlogCoordinates(instance *instmodel.Instance, binlogCoordinates *instmodel.BinlogCoordinates, otherInstance *instmodel.Instance) (*instmodel.BinlogCoordinates, int, error) {
	// We record the relay log coordinates just after the instance stopped since the coordinates can change upon
	// a FLUSH LOGS/FLUSH RELAY LOGS (or a START SLAVE, though that's an altogether different problem) etc.
	// We want to be on the safe side; we don't utterly trust that we are the only ones playing with the instance.
	recordedInstanceRelayLogCoordinates := instance.RelaylogCoordinates
	instancePseudoGtidCoordinates, instancePseudoGtidText, err := FindLastPseudoGTIDEntry(instance, recordedInstanceRelayLogCoordinates, binlogCoordinates, true, &otherInstance.Binlog_format)

	if err != nil {
		return nil, 0, err
	}
	entriesMonotonic := (config.Config.PseudoGTID.MonotonicHint != "") && strings.Contains(instancePseudoGtidText, config.Config.PseudoGTID.MonotonicHint)
	minBinlogCoordinates, _, err := instinventory.GetHeuristiclyRecentCoordinatesForInstance(&otherInstance.Key)
	if err != nil {
		return nil, 0, err
	}
	otherInstancePseudoGtidCoordinates, err := instbinlog.SearchEntryInInstanceBinlogs(otherInstance, instancePseudoGtidText, entriesMonotonic, minBinlogCoordinates)
	if err != nil {
		return nil, 0, err
	}

	// We've found a match: the latest Pseudo GTID position within instance and its identical twin in otherInstance
	// We now iterate the events in both, up to the completion of events in instance (recall that we looked for
	// the last entry in instance, hence, assuming pseudo GTID entries are frequent, the amount of entries to read
	// from instance is not long)
	// The result of the iteration will be either:
	// - bad conclusion that instance is actually more advanced than otherInstance (we find more entries in instance
	//   following the pseudo gtid than we can match in otherInstance), hence we cannot ask instance to replicate
	//   from otherInstance
	// - good result: both instances are exactly in same shape (have replicated the exact same number of events since
	//   the last pseudo gtid). Since they are identical, it is easy to point instance into otherInstance.
	// - good result: the first position within otherInstance where instance has not replicated yet. It is easy to point
	//   instance into otherInstance.
	nextBinlogCoordinatesToMatch, countMatchedEvents, err := instbinlog.GetNextBinlogCoordinatesToMatch(instance, *instancePseudoGtidCoordinates,
		recordedInstanceRelayLogCoordinates, binlogCoordinates, otherInstance, *otherInstancePseudoGtidCoordinates)
	if err != nil {
		return nil, 0, err
	}
	if countMatchedEvents == 0 {
		err = fmt.Errorf("unexpected: 0 events processed while iterating logs. Something went wrong; aborting. nextBinlogCoordinatesToMatch: %+v", nextBinlogCoordinatesToMatch)
		return nil, 0, err
	}
	return nextBinlogCoordinatesToMatch, countMatchedEvents, nil
}

func CorrelateRelaylogCoordinates(instance *instmodel.Instance, relaylogCoordinates *instmodel.BinlogCoordinates, otherInstance *instmodel.Instance) (instanceCoordinates, correlatedCoordinates, nextCoordinates *instmodel.BinlogCoordinates, found bool, err error) {
	// The two servers are expected to have the same master, or this doesn't work
	if !instance.MasterKey.Equals(&otherInstance.MasterKey) {
		return instanceCoordinates, correlatedCoordinates, nextCoordinates, found, log.Errorf("CorrelateRelaylogCoordinates requires sibling instances, however %+v has master %+v, and %+v has master %+v", instance.Key, instance.MasterKey, otherInstance.Key, otherInstance.MasterKey)
	}
	var binlogEvent *instbinlog.BinlogEvent
	if relaylogCoordinates == nil {
		instanceCoordinates = &instance.RelaylogCoordinates
		if minCoordinates, err := instinventory.GetPreviousKnownRelayLogCoordinatesForInstance(instance); err != nil {
			return instanceCoordinates, correlatedCoordinates, nextCoordinates, found, err
		} else if binlogEvent, err = instbinlog.GetLastExecutedEntryInRelayLogs(instance, minCoordinates, instance.RelaylogCoordinates); err != nil {
			return instanceCoordinates, correlatedCoordinates, nextCoordinates, found, err
		}
	} else {
		instanceCoordinates = relaylogCoordinates
		relaylogCoordinates.Type = instmodel.RelayLog
		if binlogEvent, err = instbinlog.ReadBinlogEventAtRelayLogCoordinates(&instance.Key, relaylogCoordinates); err != nil {
			return instanceCoordinates, correlatedCoordinates, nextCoordinates, found, err
		}
	}

	_, minCoordinates, err := instinventory.GetHeuristiclyRecentCoordinatesForInstance(&otherInstance.Key)
	if err != nil {
		return instanceCoordinates, correlatedCoordinates, nextCoordinates, found, err
	}
	correlatedCoordinates, nextCoordinates, found, err = instbinlog.SearchEventInRelayLogs(binlogEvent, otherInstance, minCoordinates, otherInstance.RelaylogCoordinates)
	return instanceCoordinates, correlatedCoordinates, nextCoordinates, found, err
}

// MatchBelow will attempt moving instance indicated by instanceKey below its the one indicated by otherKey.
// The refactoring is based on matching binlog entries, not on "classic" positions comparisons.
// The "other instance" could be the sibling of the moving instance any of its ancestors. It may actually be
// a cousin of some sort (though unlikely). The only important thing is that the "other instance" is more
// advanced in replication than given instance.
func MatchBelow(instanceKey, otherKey *instmodel.InstanceKey, requireInstanceMaintenance bool) (*instmodel.Instance, *instmodel.BinlogCoordinates, error) {
	instance, err := instdiscovery.ReadTopologyInstance(instanceKey)
	if err != nil {
		return instance, nil, err
	}
	// Relocation of group secondaries makes no sense, group secondaries, by definition, always replicate from the group
	// primary
	if instance.IsReplicationGroupSecondary() {
		return instance, nil, fmt.Errorf("MatchBelow: %+v is a secondary replication group member, hence, it cannot be relocated", *instanceKey)
	}
	if config.Config.PseudoGTID.Pattern == "" {
		return instance, nil, fmt.Errorf("PseudoGTIDPattern not configured; cannot use Pseudo-GTID")
	}
	if instanceKey.Equals(otherKey) {
		return instance, nil, fmt.Errorf("MatchBelow: attempt to match an instance below itself %+v", *instanceKey)
	}
	otherInstance, err := instdiscovery.ReadTopologyInstance(otherKey)
	if err != nil {
		return instance, nil, err
	}

	rinstance, _, _ := instinventory.ReadInstance(&instance.Key)
	if canMove, merr := rinstance.CanMoveViaMatch(); !canMove {
		return instance, nil, merr
	}

	if canReplicate, err := instance.CanReplicateFromEx(otherInstance, "MatchBelow()"); !canReplicate {
		return instance, nil, err
	}
	var nextBinlogCoordinatesToMatch *instmodel.BinlogCoordinates
	var countMatchedEvents int

	if otherInstance.IsBinlogServer() {
		// A Binlog Server does not do all the SHOW BINLOG EVENTS stuff
		err = fmt.Errorf("cannot use PseudoGTID with Binlog Server %+v", otherInstance.Key)
		goto Cleanup
	}

	log.Infof("Will match %+v below %+v", *instanceKey, *otherKey)

	if requireInstanceMaintenance {
		if maintenanceToken, merr := instmaintenance.BeginMaintenance(instanceKey, instmaintenance.GetMaintenanceOwner(), fmt.Sprintf("match below %+v", *otherKey)); merr != nil {
			err = fmt.Errorf("cannot begin maintenance on %+v: %v", *instanceKey, merr)
			goto Cleanup
		} else {
			defer instmaintenance.EndMaintenance(maintenanceToken)
		}

		// We don't require grabbing maintenance lock on otherInstance, but we do request
		// that it is not already under maintenance.
		if inMaintenance, merr := instmaintenance.InMaintenance(&otherInstance.Key); merr != nil {
			err = merr
			goto Cleanup
		} else if inMaintenance {
			err = fmt.Errorf("cannot match below %+v; it is in maintenance", otherInstance.Key)
			goto Cleanup
		}
	}

	log.Debugf("Stopping replica on %+v", *instanceKey)
	instance, err = instreplication.StopReplication(instanceKey)
	if err != nil {
		goto Cleanup
	}

	nextBinlogCoordinatesToMatch, countMatchedEvents, err = CorrelateBinlogCoordinates(instance, nil, otherInstance)
	if err != nil {
		goto Cleanup
	}

	if countMatchedEvents == 0 {
		err = fmt.Errorf("unexpected: 0 events processed while iterating logs. Something went wrong; aborting. nextBinlogCoordinatesToMatch: %+v", nextBinlogCoordinatesToMatch)
		goto Cleanup
	}
	log.Debugf("%+v will match below %+v at %+v; validated events: %d", *instanceKey, *otherKey, *nextBinlogCoordinatesToMatch, countMatchedEvents)

	// Drum roll...
	instance, err = instreplication.ChangeMasterTo(instanceKey, otherKey, nextBinlogCoordinatesToMatch, false, modeldomain.GTIDHintDeny)
	if err != nil {
		goto Cleanup
	}

Cleanup:
	instance, err = restartReplicationAfterTopologyOperation(instanceKey, instance, err)
	if err != nil {
		return instance, nextBinlogCoordinatesToMatch, log.Errore(err)
	}
	// and we're done (pending deferred functions)
	instaudit.AuditOperation("match-below", instanceKey, fmt.Sprintf("matched %+v below %+v", *instanceKey, *otherKey))

	return instance, nextBinlogCoordinatesToMatch, err
}

// RematchReplica will re-match a replica to its master, using pseudo-gtid
func RematchReplica(instanceKey *instmodel.InstanceKey, requireInstanceMaintenance bool) (*instmodel.Instance, *instmodel.BinlogCoordinates, error) {
	instance, err := instdiscovery.ReadTopologyInstance(instanceKey)
	if err != nil {
		return instance, nil, err
	}
	masterInstance, found, err := instinventory.ReadInstance(&instance.MasterKey)
	if err != nil || !found {
		return instance, nil, err
	}
	return MatchBelow(instanceKey, &masterInstance.Key, requireInstanceMaintenance)
}

// MakeMaster will take an instance, make all its siblings its replicas (via pseudo-GTID) and make it master
// (stop its replicaiton, make writeable).
func MakeMaster(instanceKey *instmodel.InstanceKey) (*instmodel.Instance, error) {
	instance, err := instdiscovery.ReadTopologyInstance(instanceKey)
	if err != nil {
		return instance, err
	}
	masterInstance, err := instdiscovery.ReadTopologyInstance(&instance.MasterKey)
	if err == nil {
		// If the read succeeded, check the master status.
		if masterInstance.IsReplica() {
			return instance, fmt.Errorf("MakeMaster: instance's master %+v seems to be replicating", masterInstance.Key)
		}
		if masterInstance.IsLastCheckValid {
			return instance, fmt.Errorf("MakeMaster: instance's master %+v seems to be accessible", masterInstance.Key)
		}
	}
	// Continue anyway if the read failed, because that means the master is
	// inaccessible... So it's OK to do the promotion.
	if !instance.SQLThreadUpToDate() {
		return instance, fmt.Errorf("MakeMaster: instance's SQL thread must be up-to-date with I/O thread for %+v", *instanceKey)
	}
	siblings, err := instinventory.ReadReplicaInstances(&masterInstance.Key)
	if err != nil {
		return instance, err
	}
	for _, sibling := range siblings {
		if instance.ExecBinlogCoordinates.SmallerThan(&sibling.ExecBinlogCoordinates) {
			return instance, fmt.Errorf("MakeMaster: instance %+v has more advanced sibling: %+v", *instanceKey, sibling.Key)
		}
	}

	if maintenanceToken, merr := instmaintenance.BeginMaintenance(instanceKey, instmaintenance.GetMaintenanceOwner(), fmt.Sprintf("siblings match below this: %+v", *instanceKey)); merr != nil {
		err = fmt.Errorf("cannot begin maintenance on %+v: %v", *instanceKey, merr)
		goto Cleanup
	} else {
		defer instmaintenance.EndMaintenance(maintenanceToken)
	}

	_, _, _, err = MultiMatchBelow(siblings, instanceKey, nil)
	if err != nil {
		goto Cleanup
	}

	instreplication.SetReadOnly(instanceKey, false)

Cleanup:
	if err != nil {
		return instance, log.Errore(err)
	}
	// and we're done (pending deferred functions)
	instaudit.AuditOperation("make-master", instanceKey, fmt.Sprintf("made master of %+v", *instanceKey))

	return instance, err
}

// TakeSiblings is a convenience method for turning siblings of a replica to be its subordinates.
// This operation is a syntatctic sugar on top relocate-replicas, which uses any available means to the objective:
// GTID, Pseudo-GTID, binlog servers, standard replication...
func TakeSiblings(instanceKey *instmodel.InstanceKey) (instance *instmodel.Instance, takenSiblings int, err error) {
	instance, err = instdiscovery.ReadTopologyInstance(instanceKey)
	if err != nil {
		return instance, 0, err
	}
	if !instance.IsReplica() {
		return instance, takenSiblings, log.Errorf("take-siblings: instance %+v is not a replica.", *instanceKey)
	}
	relocatedReplicas, _, _, err := RelocateReplicas(&instance.MasterKey, instanceKey, "")

	return instance, len(relocatedReplicas), err
}

// Created this function to allow a hook to be called after a successful TakeMaster event
func TakeMasterHook(successor *instmodel.Instance, demoted *instmodel.Instance) {
	if demoted == nil {
		return
	}
	if successor == nil {
		return
	}
	successorKey := successor.Key
	demotedKey := demoted.Key
	env := goos.Environ()

	env = append(env, fmt.Sprintf("ORC_SUCCESSOR_HOST=%s", successorKey))
	env = append(env, fmt.Sprintf("ORC_FAILED_HOST=%s", demotedKey))

	successorStr := successorKey.String()
	demotedStr := demotedKey.String()

	hooks, _, err := recoverypolicy.EffectiveHooks(context.Background(), successor.ClusterName)
	if err != nil {
		log.Errorf("Take-Master: resolve post_take_master hooks: %v", err)
		return
	}
	for _, profile := range hooks["post_take_master"] {
		for i, command := range profile.Commands {
			fullDescription := fmt.Sprintf("PostTakeMasterProcesses profile=%s revision=%d command=%d/%d", profile.ID, profile.Revision, i+1, len(profile.Commands))
			commandCtx, cancel := context.WithTimeout(context.Background(), time.Duration(profile.TimeoutSeconds)*time.Second)
			output, commandErr := os.CommandRunContext(commandCtx, command, env, profile.OutputLimitBytes, successorStr, demotedStr)
			output = recoverypolicy.RedactOutput(output)
			cancel()
			instaudit.AuditOperation("post-take-master-hook", &successor.Key, fmt.Sprintf("%s output=%q error=%v", fullDescription, output, commandErr))
			if commandErr != nil && profile.FailurePolicy == "abort" {
				return
			}
		}
	}

}

// TakeMaster will move an instance up the chain and cause its master to become its replica.
// It's almost a role change, just that other replicas of either 'instance' or its master are currently unaffected
// (they continue replicate without change)
// Note that the master must itself be a replica; however the grandparent does not necessarily have to be reachable
// and can in fact be dead.
func TakeMaster(instanceKey *instmodel.InstanceKey, allowTakingCoMaster bool) (*instmodel.Instance, error) {
	instance, err := instdiscovery.ReadTopologyInstance(instanceKey)
	if err != nil {
		return instance, err
	}
	// Relocation of group secondaries makes no sense, group secondaries, by definition, always replicate from the group
	// primary
	if instance.IsReplicationGroupSecondary() {
		return instance, fmt.Errorf("takeMaster: %+v is a secondary replication group member, hence, it cannot be relocated", instance.Key)
	}
	masterInstance, found, err := instinventory.ReadInstance(&instance.MasterKey)
	if err != nil || !found {
		return instance, err
	}
	if masterInstance.IsCoMaster && !allowTakingCoMaster {
		return instance, fmt.Errorf("%+v is co-master. Cannot take it", masterInstance.Key)
	}
	log.Debugf("TakeMaster: will attempt making %+v take its master %+v, now resolved as %+v", *instanceKey, instance.MasterKey, masterInstance.Key)

	if canReplicate, err := masterInstance.CanReplicateFromEx(instance, "TakeMaster()"); !canReplicate {
		return instance, err
	}

	// We begin
	masterInstance, err = instreplication.StopReplication(&masterInstance.Key)
	if err != nil {
		goto Cleanup
	}
	instance, err = instreplication.StopReplication(&instance.Key)
	if err != nil {
		goto Cleanup
	}

	instance, err = instreplication.StartReplicationUntilMasterCoordinates(&instance.Key, &masterInstance.SelfBinlogCoordinates)
	if err != nil {
		goto Cleanup
	}

	// instance and masterInstance are equal
	// We skip name unresolve. It is OK if the master's master is dead, unreachable, does not resolve properly.
	// We just copy+paste info from the master.
	// In particular, this is commonly calledin DeadMaster recovery
	instance, err = instreplication.ChangeMasterTo(&instance.Key, &masterInstance.MasterKey, &masterInstance.ExecBinlogCoordinates, true, modeldomain.GTIDHintNeutral)
	if err != nil {
		goto Cleanup
	}
	// instance is now sibling of master
	masterInstance, err = instreplication.ChangeMasterTo(&masterInstance.Key, &instance.Key, &instance.SelfBinlogCoordinates, false, modeldomain.GTIDHintNeutral)
	if err != nil {
		goto Cleanup
	}
	// swap is done!

Cleanup:
	if instance != nil {
		instance, err = restartReplicationAfterTopologyOperation(&instance.Key, instance, err)
	}
	if masterInstance != nil {
		masterInstance, err = restartReplicationAfterTopologyOperation(&masterInstance.Key, masterInstance, err)
	}
	if err != nil {
		return instance, err
	}
	instaudit.AuditOperation("take-master", instanceKey, fmt.Sprintf("took master: %+v", masterInstance.Key))

	demoted := masterInstance
	successor := instance
	TakeMasterHook(successor, demoted)

	return instance, err
}

// MakeLocalMaster promotes a replica above its master, making it replica of its grandparent, while also enslaving its siblings.
// This serves as a convenience method to recover replication when a local master fails; the instance promoted is one of its replicas,
// which is most advanced among its siblings.
// This method utilizes Pseudo GTID
func MakeLocalMaster(instanceKey *instmodel.InstanceKey) (*instmodel.Instance, error) {
	instance, err := instdiscovery.ReadTopologyInstance(instanceKey)
	if err != nil {
		return instance, err
	}
	masterInstance, found, err := instinventory.ReadInstance(&instance.MasterKey)
	if err != nil || !found {
		return instance, err
	}
	grandparentInstance, err := instdiscovery.ReadTopologyInstance(&masterInstance.MasterKey)
	if err != nil {
		return instance, err
	}
	siblings, err := instinventory.ReadReplicaInstances(&masterInstance.Key)
	if err != nil {
		return instance, err
	}
	for _, sibling := range siblings {
		if instance.ExecBinlogCoordinates.SmallerThan(&sibling.ExecBinlogCoordinates) {
			return instance, fmt.Errorf("MakeMaster: instance %+v has more advanced sibling: %+v", *instanceKey, sibling.Key)
		}
	}

	instance, err = instreplication.StopReplicationNicely(instanceKey, 0)
	if err != nil {
		goto Cleanup
	}

	_, _, err = MatchBelow(instanceKey, &grandparentInstance.Key, true)
	if err != nil {
		goto Cleanup
	}

	_, _, _, err = MultiMatchBelow(siblings, instanceKey, nil)
	if err != nil {
		goto Cleanup
	}

Cleanup:
	if err != nil {
		return instance, log.Errore(err)
	}
	// and we're done (pending deferred functions)
	instaudit.AuditOperation("make-local-master", instanceKey, fmt.Sprintf("made master of %+v", *instanceKey))

	return instance, err
}

// MultiMatchBelow will efficiently match multiple replicas below a given instance.
// It is assumed that all given replicas are siblings
func MultiMatchBelow(replicas []*instmodel.Instance, belowKey *instmodel.InstanceKey, postponedFunctionsContainer *PostponedFunctionsContainer) (matchedReplicas []*instmodel.Instance, belowInstance *instmodel.Instance, errs []error, err error) {
	belowInstance, found, err := instinventory.ReadInstance(belowKey)
	if err != nil || !found {
		return matchedReplicas, belowInstance, errs, err
	}

	replicas = instmodel.RemoveInstance(replicas, belowKey)
	if len(replicas) == 0 {
		// Nothing to do
		return replicas, belowInstance, errs, err
	}

	log.Infof("Will match %+v replicas below %+v via Pseudo-GTID, independently", len(replicas), belowKey)

	barrier := make(chan *instmodel.InstanceKey)
	replicaMutex := &sync.Mutex{}

	for _, replica := range replicas {

		// Parallelize repoints
		go func() {
			defer func() { barrier <- &replica.Key }()
			matchFunc := func() error {
				replica, _, replicaErr := MatchBelow(&replica.Key, belowKey, true)

				replicaMutex.Lock()
				defer replicaMutex.Unlock()

				if replicaErr == nil {
					matchedReplicas = append(matchedReplicas, replica)
				} else {
					errs = append(errs, replicaErr)
				}
				return replicaErr
			}
			if shouldPostponeRelocatingReplica(replica, postponedFunctionsContainer) {
				postponedFunctionsContainer.AddPostponedFunction(matchFunc, fmt.Sprintf("multi-match-below-independent %+v", replica.Key))
				// We bail out and trust our invoker to later call upon this postponed function
			} else {
				instreplication.ExecuteOnTopology(func() { matchFunc() })
			}
		}()
	}
	for range replicas {
		<-barrier
	}
	if len(errs) == len(replicas) {
		// All returned with error
		return matchedReplicas, belowInstance, errs, fmt.Errorf("MultiMatchBelowIndependently: Error on all %+v operations", len(errs))
	}
	instaudit.AuditOperation("multi-match-below-independent", belowKey, fmt.Sprintf("matched %d/%d replicas below %+v via Pseudo-GTID", len(matchedReplicas), len(replicas), belowKey))

	return matchedReplicas, belowInstance, errs, err
}

// MultiMatchReplicas will match (via pseudo-gtid) all replicas of given master below given instance.
func MultiMatchReplicas(masterKey *instmodel.InstanceKey, belowKey *instmodel.InstanceKey, pattern string) ([]*instmodel.Instance, *instmodel.Instance, []error, error) {
	res := []*instmodel.Instance{}
	errs := []error{}

	belowInstance, err := instdiscovery.ReadTopologyInstance(belowKey)
	if err != nil {
		// Can't access "below" ==> can't match replicas beneath it
		return res, nil, errs, err
	}

	masterInstance, found, err := instinventory.ReadInstance(masterKey)
	if err != nil || !found {
		return res, nil, errs, err
	}

	// See if we have a binlog server case (special handling):
	binlogCase := false
	if masterInstance.IsBinlogServer() && masterInstance.MasterKey.Equals(belowKey) {
		// repoint-up
		log.Debugf("MultiMatchReplicas: pointing replicas up from binlog server")
		binlogCase = true
	} else if belowInstance.IsBinlogServer() && belowInstance.MasterKey.Equals(masterKey) {
		// repoint-down
		log.Debugf("MultiMatchReplicas: pointing replicas down to binlog server")
		binlogCase = true
	} else if masterInstance.IsBinlogServer() && belowInstance.IsBinlogServer() && masterInstance.MasterKey.Equals(&belowInstance.MasterKey) {
		// Both BLS, siblings
		log.Debugf("MultiMatchReplicas: pointing replicas to binlong sibling")
		binlogCase = true
	}
	if binlogCase {
		replicas, operationErrors, err := RepointReplicasTo(masterKey, pattern, belowKey)
		// Bail out!
		return replicas, masterInstance, operationErrors, err
	}

	// Not binlog server

	// replicas involved
	replicas, err := instinventory.ReadReplicaInstancesIncludingBinlogServerSubReplicas(masterKey)
	if err != nil {
		return res, belowInstance, errs, err
	}
	replicas = instmodel.FilterInstancesByPattern(replicas, pattern)
	matchedReplicas, belowInstance, errs, err := MultiMatchBelow(replicas, &belowInstance.Key, nil)

	if len(matchedReplicas) != len(replicas) {
		err = fmt.Errorf("MultiMatchReplicas: only matched %d out of %d replicas of %+v; error is: %+v", len(matchedReplicas), len(replicas), *masterKey, err)
	}
	instaudit.AuditOperation("multi-match-replicas", masterKey, fmt.Sprintf("matched %d replicas under %+v", len(matchedReplicas), *belowKey))

	return matchedReplicas, belowInstance, errs, err
}

// MatchUp will move a replica up the replication chain, so that it becomes sibling of its master, via Pseudo-GTID
func MatchUp(instanceKey *instmodel.InstanceKey, requireInstanceMaintenance bool) (*instmodel.Instance, *instmodel.BinlogCoordinates, error) {
	instance, found, err := instinventory.ReadInstance(instanceKey)
	if err != nil || !found {
		return nil, nil, err
	}
	if !instance.IsReplica() {
		return instance, nil, fmt.Errorf("instance is not a replica: %+v", instanceKey)
	}
	// Relocation of group secondaries makes no sense, group secondaries, by definition, always replicate from the group
	// primary
	if instance.IsReplicationGroupSecondary() {
		return instance, nil, fmt.Errorf("MatchUp: %+v is a secondary replication group member, hence, it cannot be relocated", instance.Key)
	}
	master, found, err := instinventory.ReadInstance(&instance.MasterKey)
	if err != nil || !found {
		return instance, nil, log.Errorf("Cannot get master for %+v. error=%+v", instance.Key, err)
	}

	if !master.IsReplica() {
		return instance, nil, fmt.Errorf("master is not a replica itself: %+v", master.Key)
	}

	return MatchBelow(instanceKey, &master.MasterKey, requireInstanceMaintenance)
}

// MatchUpReplicas will move all replicas of given master up the replication chain,
// so that they become siblings of their master.
// This should be called when the local master dies, and all its replicas are to be resurrected via Pseudo-GTID
func MatchUpReplicas(masterKey *instmodel.InstanceKey, pattern string) ([]*instmodel.Instance, *instmodel.Instance, []error, error) {
	res := []*instmodel.Instance{}
	errs := []error{}

	masterInstance, found, err := instinventory.ReadInstance(masterKey)
	if err != nil || !found {
		return res, nil, errs, err
	}

	return MultiMatchReplicas(masterKey, &masterInstance.MasterKey, pattern)
}

// relocateBelowInternal is a protentially recursive function which chooses how to relocate an instance below another.
// It may choose to use Pseudo-GTID, or normal binlog positions, or take advantage of binlog servers,
// or it may combine any of the above in a multi-step operation.
func relocateBelowInternal(instance, other *instmodel.Instance) (*instmodel.Instance, error) {
	if canReplicate, err := instance.CanReplicateFromEx(other, "relocateBelowInternal()"); !canReplicate {
		return instance, log.Errorf("%+v cannot replicate from %+v. Reason: %+v", instance.Key, other.Key, err)
	}
	// simplest:
	if insttopology.InstanceIsMasterOf(other, instance) {
		// already the desired setup.
		return Repoint(&instance.Key, &other.Key, modeldomain.GTIDHintNeutral)
	}
	// Do we have record of equivalent coordinates?
	if !instance.IsBinlogServer() {
		if movedInstance, err := MoveEquivalent(&instance.Key, &other.Key); err == nil {
			return movedInstance, nil
		}
	}
	// Try and take advantage of binlog servers:
	if insttopology.InstancesAreSiblings(instance, other) && other.IsBinlogServer() {
		return MoveBelow(&instance.Key, &other.Key)
	}
	instanceMaster, _, err := instinventory.ReadInstance(&instance.MasterKey)
	if err != nil {
		return instance, err
	}
	if instanceMaster != nil && instanceMaster.MasterKey.Equals(&other.Key) && instanceMaster.IsBinlogServer() {
		// Moving to grandparent via binlog server
		return Repoint(&instance.Key, &instanceMaster.MasterKey, modeldomain.GTIDHintDeny)
	}
	if other.IsBinlogServer() {
		if instanceMaster != nil && instanceMaster.IsBinlogServer() && insttopology.InstancesAreSiblings(instanceMaster, other) {
			// Special case: this is a binlog server family; we move under the uncle, in one single step
			return Repoint(&instance.Key, &other.Key, modeldomain.GTIDHintDeny)
		}

		// Relocate to its master, then repoint to the binlog server
		otherMaster, found, err := instinventory.ReadInstance(&other.MasterKey)
		if err != nil {
			return instance, err
		}
		if !found {
			return instance, log.Errorf("Cannot find master %+v", other.MasterKey)
		}
		if !other.IsLastCheckValid {
			return instance, log.Errorf("Binlog server %+v is not reachable. It would take two steps to relocate %+v below it, and I won't even do the first step.", other.Key, instance.Key)
		}

		log.Debugf("Relocating to a binlog server; will first attempt to relocate to the binlog server's master: %+v, and then repoint down", otherMaster.Key)
		if _, err := relocateBelowInternal(instance, otherMaster); err != nil {
			return instance, err
		}
		return Repoint(&instance.Key, &other.Key, modeldomain.GTIDHintDeny)
	}
	if instance.IsBinlogServer() {
		// Can only move within the binlog-server family tree
		// And these have been covered just now: move up from a master binlog server, move below a binling binlog server.
		// sure, the family can be more complex, but we keep these operations atomic
		return nil, log.Errorf("Relocating binlog server %+v below %+v turns to be too complex; please do it manually", instance.Key, other.Key)
	}
	// Next, try GTID
	if _, _, gtidCompatible := instancesAreGTIDAndCompatible(instance, other); gtidCompatible {
		return moveInstanceBelowViaGTID(instance, other)
	}

	// Next, try Pseudo-GTID
	if instance.UsingPseudoGTID && other.UsingPseudoGTID {
		// We prefer PseudoGTID to anything else because, while it takes longer to run, it does not issue
		// a STOP SLAVE on any server other than "instance" itself.
		instance, _, err := MatchBelow(&instance.Key, &other.Key, true)
		return instance, err
	}
	// No Pseudo-GTID; check simple binlog file/pos operations:
	if insttopology.InstancesAreSiblings(instance, other) {
		// If comastering, only move below if it's read-only
		if !other.IsCoMaster || other.ReadOnly {
			return MoveBelow(&instance.Key, &other.Key)
		}
	}
	// See if we need to MoveUp
	if instanceMaster != nil && instanceMaster.MasterKey.Equals(&other.Key) {
		// Moving to grandparent--handles co-mastering writable case
		return MoveUp(&instance.Key)
	}
	if instanceMaster != nil && instanceMaster.IsBinlogServer() {
		// Break operation into two: move (repoint) up, then continue
		if _, err := MoveUp(&instance.Key); err != nil {
			return instance, err
		}
		return relocateBelowInternal(instance, other)
	}
	// Too complex
	return nil, log.Errorf("Relocating %+v below %+v turns to be too complex; please do it manually", instance.Key, other.Key)
}

// RelocateBelow will attempt moving instance indicated by instanceKey below another instance.
// Orchestrator will try and figure out the best way to relocate the server. This could span normal
// binlog-position, pseudo-gtid, repointing, binlog servers...
func RelocateBelow(instanceKey, otherKey *instmodel.InstanceKey) (*instmodel.Instance, error) {
	instance, found, err := instinventory.ReadInstance(instanceKey)
	if err != nil || !found {
		return instance, log.Errorf("Error reading %+v", *instanceKey)
	}
	// Relocation of group secondaries makes no sense, group secondaries, by definition, always replicate from the group
	// primary
	if instance.IsReplicationGroupSecondary() {
		return instance, log.Errorf("relocate: %+v is a secondary replication group member, hence, it cannot be relocated", instance.Key)
	}
	other, found, err := instinventory.ReadInstance(otherKey)
	if err != nil || !found {
		return instance, log.Errorf("Error reading %+v", *otherKey)
	}
	// Disallow setting up a group primary to replicate from a group secondary
	if instance.IsReplicationGroupPrimary() && other.ReplicationGroupName == instance.ReplicationGroupName {
		return instance, log.Errorf("relocate: Setting a group primary to replicate from another member of its group is disallowed")
	}
	if other.IsDescendantOf(instance) {
		return instance, log.Errorf("relocate: %+v is a descendant of %+v", *otherKey, instance.Key)
	}
	instance, err = relocateBelowInternal(instance, other)
	if err == nil {
		instaudit.AuditOperation("relocate-below", instanceKey, fmt.Sprintf("relocated %+v below %+v", *instanceKey, *otherKey))
	}
	return instance, err
}

// relocateReplicasInternal is a protentially recursive function which chooses how to relocate
// replicas of an instance below another.
// It may choose to use Pseudo-GTID, or normal binlog positions, or take advantage of binlog servers,
// or it may combine any of the above in a multi-step operation.
func relocateReplicasInternal(replicas []*instmodel.Instance, instance, other *instmodel.Instance) ([]*instmodel.Instance, []error, error) {
	errs := []error{}
	var err error
	// simplest:
	if instance.Key.Equals(&other.Key) {
		// already the desired setup.
		return RepointTo(replicas, &other.Key)
	}
	// Try and take advantage of binlog servers:
	if insttopology.InstanceIsMasterOf(other, instance) && instance.IsBinlogServer() {
		// Up from a binlog server
		return RepointTo(replicas, &other.Key)
	}
	if insttopology.InstanceIsMasterOf(instance, other) && other.IsBinlogServer() {
		// Down under a binlog server
		return RepointTo(replicas, &other.Key)
	}
	if insttopology.InstancesAreSiblings(instance, other) && instance.IsBinlogServer() && other.IsBinlogServer() {
		// Between siblings
		return RepointTo(replicas, &other.Key)
	}
	if other.IsBinlogServer() {
		// Relocate to binlog server's parent (recursive call), then repoint down
		otherMaster, found, err := instinventory.ReadInstance(&other.MasterKey)
		if err != nil || !found {
			return nil, errs, err
		}
		replicas, errs, err = relocateReplicasInternal(replicas, instance, otherMaster)
		if err != nil {
			return replicas, errs, err
		}

		return RepointTo(replicas, &other.Key)
	}
	// GTID
	gtidErrorsMsg := ""
	{
		movedReplicas, unmovedReplicas, errs, err := MoveReplicasViaGTID(replicas, other, nil)

		if len(movedReplicas) == len(replicas) {
			// Moved (or tried moving) everything via GTID
			return movedReplicas, errs, err
		} else if len(movedReplicas) > 0 {
			// something was moved via GTID; let's try further on
			return relocateReplicasInternal(unmovedReplicas, instance, other)
		}

		// Making sure that if there are any errors in errs, they are reported
		if len(errs) > 0 {
			// There are errors, maybe more than one. Let's concatenate them
			// so they can be reported correctly in the UI
			gtidErrorsMsg = "Error(s): "

			for _, err := range errs {
				gtidErrorsMsg = fmt.Sprintf("%s; %v", gtidErrorsMsg, err)
			}

		}
		// Otherwise nothing was moved via GTID. Maybe we don't have any GTIDs, we continue.
	}

	// Pseudo GTID
	if other.UsingPseudoGTID {
		// Which replicas are using Pseudo GTID?
		var pseudoGTIDReplicas []*instmodel.Instance
		for _, replica := range replicas {
			_, _, hasToBeGTID := instancesAreGTIDAndCompatible(replica, other)
			if replica.UsingPseudoGTID && !hasToBeGTID {
				pseudoGTIDReplicas = append(pseudoGTIDReplicas, replica)
			}
		}
		pseudoGTIDReplicas, _, errs, err = MultiMatchBelow(pseudoGTIDReplicas, &other.Key, nil)
		return pseudoGTIDReplicas, errs, err
	}

	// Normal binlog file:pos
	if insttopology.InstanceIsMasterOf(other, instance) {
		// MoveUpReplicas -- but not supporting "replicas" argument at this time.
	}

	// Too complex
	// if the len of gtidErrorsMsg is less than 21, no errors were added
	if len(gtidErrorsMsg) > 0 {
		gtidErrorsMsg = "Additional Errors: " + gtidErrorsMsg
	}
	return nil, errs, log.Errorf("Relocating %+v replicas of %+v below %+v turns to be too complex; please do it manually. %v", len(replicas), instance.Key, other.Key, gtidErrorsMsg)
}

// RelocateReplicas will attempt moving replicas of an instance indicated by instanceKey below another instance.
// Orchestrator will try and figure out the best way to relocate the servers. This could span normal
// binlog-position, pseudo-gtid, repointing, binlog servers...
func RelocateReplicas(instanceKey, otherKey *instmodel.InstanceKey, pattern string) (replicas []*instmodel.Instance, other *instmodel.Instance, errs []error, err error) {

	instance, found, err := instinventory.ReadInstance(instanceKey)
	if err != nil || !found {
		return replicas, other, errs, log.Errorf("Error reading %+v", *instanceKey)
	}
	other, found, err = instinventory.ReadInstance(otherKey)
	if err != nil || !found {
		return replicas, other, errs, log.Errorf("Error reading %+v", *otherKey)
	}

	replicas, err = instinventory.ReadReplicaInstances(instanceKey)
	if err != nil {
		return replicas, other, errs, err
	}
	replicas = instmodel.RemoveInstance(replicas, otherKey)
	replicas = instmodel.FilterInstancesByPattern(replicas, pattern)
	if len(replicas) == 0 {
		// Nothing to do
		return replicas, other, errs, nil
	}
	for _, replica := range replicas {
		if other.IsDescendantOf(replica) {
			return replicas, other, errs, log.Errorf("relocate-replicas: %+v is a descendant of %+v", *otherKey, replica.Key)
		}
	}
	replicas, errs, err = relocateReplicasInternal(replicas, instance, other)

	if err == nil {
		instaudit.AuditOperation("relocate-replicas", instanceKey, fmt.Sprintf("relocated %+v replicas of %+v below %+v", len(replicas), *instanceKey, *otherKey))
	}
	return replicas, other, errs, err
}

// PurgeBinaryLogsTo attempts to 'PURGE BINARY LOGS' until given binary log is reached
func PurgeBinaryLogsTo(instanceKey *instmodel.InstanceKey, logFile string, force bool) (*instmodel.Instance, error) {
	replicas, err := instinventory.ReadReplicaInstances(instanceKey)
	if err != nil {
		return nil, err
	}
	if !force {
		purgeCoordinates := &instmodel.BinlogCoordinates{LogFile: logFile, LogPos: 0}
		for _, replica := range replicas {
			if !purgeCoordinates.SmallerThan(&replica.ExecBinlogCoordinates) {
				return nil, log.Errorf("Unsafe to purge binary logs on %+v up to %s because replica %+v has only applied up to %+v", *instanceKey, logFile, replica.Key, replica.ExecBinlogCoordinates)
			}
		}
	}
	return instreplication.PurgeBinaryLogsTo(instanceKey, logFile)
}

// PurgeBinaryLogsToLatest attempts to 'PURGE BINARY LOGS' until latest binary log
func PurgeBinaryLogsToLatest(instanceKey *instmodel.InstanceKey, force bool) (*instmodel.Instance, error) {
	instance, err := instdiscovery.ReadTopologyInstance(instanceKey)
	if err != nil {
		return instance, log.Errore(err)
	}
	return PurgeBinaryLogsTo(instanceKey, instance.SelfBinlogCoordinates.LogFile, force)
}
