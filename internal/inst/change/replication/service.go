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

// Package replication owns low-level replication and server state changes.
package replication

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/golib/log"
	instaudit "github.com/openark/orchestrator/internal/inst/audit"
	instdiscovery "github.com/openark/orchestrator/internal/inst/discovery"
	instequivalence "github.com/openark/orchestrator/internal/inst/equivalence"
	"github.com/openark/orchestrator/internal/inst/gtid"
	instmodel "github.com/openark/orchestrator/internal/inst/instance"
	instinventory "github.com/openark/orchestrator/internal/inst/inventory"
	mysqlquery "github.com/openark/orchestrator/internal/inst/mysql"
	instresolve "github.com/openark/orchestrator/internal/inst/resolve"
	modeldomain "github.com/openark/orchestrator/internal/models/domain"
	"github.com/openark/orchestrator/internal/recoverypolicy"
	"github.com/openark/orchestrator/internal/repository/topology"
	"github.com/openark/orchestrator/internal/util"
	"github.com/patrickmn/go-cache"
)

type StopReplicationMethod string

const (
	NoStopReplication     = "NoStopReplication"
	StopReplicationNormal = "StopReplicationNormal"
	StopReplicationNice   = "StopReplicationNice"
)

var ErrReplicationNotRunning = errors.New("replication not running")

// Max concurrency for bulk topology operations
const topologyConcurrency = 128

const retryInterval = 500 * time.Millisecond

var topologyConcurrencyChan = make(chan bool, topologyConcurrency)
var supportedAutoPseudoGTIDWriters *cache.Cache = cache.New(config.CheckAutoPseudoGTIDGrantsIntervalSeconds*time.Second, time.Second)

const (
	Error1201CouldnotInitializeMasterInfoStructure = "Error 1201:"
)

// ExecuteInstanceCommand executes a given query on the given MySQL topology instance
func ExecuteInstanceCommand(instanceKey *instmodel.InstanceKey, query string, args ...any) error {
	db, err := topology.Open(instanceKey.Hostname, instanceKey.Port)
	if err != nil {
		return err
	}
	return db.Execute(query, args...)
}

// ExecuteOnTopology will execute given function while maintaining concurrency limit
// on topology servers. It is safe in the sense that we will not leak tokens.
func ExecuteOnTopology(f func()) {
	topologyConcurrencyChan <- true
	defer func() { recover(); <-topologyConcurrencyChan }()
	f()
}

// ReadInstanceRow executes a read-a-single-row query on a given MySQL topology instance
func ReadInstanceRow(instanceKey *instmodel.InstanceKey, query string, dest ...any) error {
	db, err := topology.Open(instanceKey.Hostname, instanceKey.Port)
	if err != nil {
		return err
	}
	err = db.Read(query, dest...)
	return err
}

// ProbeInstanceCommit issues an empty COMMIT on a given instance
func ProbeInstanceCommit(instanceKey *instmodel.InstanceKey) error {
	db, err := topology.Open(instanceKey.Hostname, instanceKey.Port)
	if err != nil {
		return err
	}
	return db.CommitProbe(context.Background())
}

// RefreshTopologyInstance will synchronuously re-read topology instance
func RefreshTopologyInstance(instanceKey *instmodel.InstanceKey) (*instmodel.Instance, error) {
	_, err := instdiscovery.ReadTopologyInstance(instanceKey)
	if err != nil {
		return nil, err
	}

	inst, found, err := instinventory.ReadInstance(instanceKey)
	if err != nil || !found {
		return nil, err
	}

	return inst, nil
}

// RefreshTopologyInstances will do a blocking (though concurrent) refresh of all given instances
func RefreshTopologyInstances(instances []*instmodel.Instance) {
	// use concurrency but wait for all to complete
	barrier := make(chan instmodel.InstanceKey)
	for _, instance := range instances {
		go func() {
			// Signal completed replica
			defer func() { barrier <- instance.Key }()
			// Wait your turn to read a replica
			ExecuteOnTopology(func() {
				log.Debugf("... reading instance: %+v", instance.Key)
				instdiscovery.ReadTopologyInstance(&instance.Key)
			})
		}()
	}
	for range instances {
		<-barrier
	}
}

// GetReplicationRestartPreserveStatements returns a sequence of statements that make sure a replica is stopped
// and then returned to the same state. For example, if the replica was fully running, this will issue
// a STOP on both io_thread and sql_thread, followed by START on both. If one of them is not running
// at the time this function is called, said thread will be neither stopped nor started.
// The caller may provide an injected statememt, to be executed while the replica is stopped.
// This is useful for CHANGE MASTER TO commands, that unfortunately must take place while the replica
// is completely stopped.
func GetReplicationRestartPreserveStatements(instanceKey *instmodel.InstanceKey, injectedStatement string) (statements []string, err error) {
	instance, err := instdiscovery.ReadTopologyInstance(instanceKey)
	if err != nil {
		return statements, err
	}
	if instance.ReplicationIOThreadRuning {
		statements = append(statements, instmodel.SemicolonTerminated(mysqlquery.Query(instance.Version, mysqlquery.StopSlaveIOThread)))
	}
	if instance.ReplicationSQLThreadRuning {
		statements = append(statements, instmodel.SemicolonTerminated(mysqlquery.Query(instance.Version, mysqlquery.StopSlaveSQLThread)))
	}
	if injectedStatement != "" {
		statements = append(statements, instmodel.SemicolonTerminated(injectedStatement))
	}
	if instance.ReplicationSQLThreadRuning {
		statements = append(statements, instmodel.SemicolonTerminated(mysqlquery.Query(instance.Version, mysqlquery.StartSlaveSQLThread)))
	}
	if instance.ReplicationIOThreadRuning {
		statements = append(statements, instmodel.SemicolonTerminated(mysqlquery.Query(instance.Version, mysqlquery.StartSlaveIOThread)))
	}
	return statements, err
}

// FlushBinaryLogs attempts a 'FLUSH BINARY LOGS' statement on the given instance.
func FlushBinaryLogs(instanceKey *instmodel.InstanceKey, count int) (*instmodel.Instance, error) {
	if *config.RuntimeCLIFlags.Noop {
		return nil, fmt.Errorf("noop: aborting flush-binary-logs operation on %+v; signalling error but nothing went wrong", *instanceKey)
	}

	for range count {
		err := ExecuteInstanceCommand(instanceKey, `flush binary logs`)
		if err != nil {
			return nil, log.Errore(err)
		}
	}

	log.Infof("flush-binary-logs count=%+v on %+v", count, *instanceKey)
	instaudit.AuditOperation("flush-binary-logs", instanceKey, "success")

	return instdiscovery.ReadTopologyInstance(instanceKey)
}

// FlushBinaryLogsTo attempts to 'FLUSH BINARY LOGS' until given binary log is reached
func FlushBinaryLogsTo(instanceKey *instmodel.InstanceKey, logFile string) (*instmodel.Instance, error) {
	instance, err := instdiscovery.ReadTopologyInstance(instanceKey)
	if err != nil {
		return instance, log.Errore(err)
	}

	distance := instance.SelfBinlogCoordinates.FileNumberDistance(&instmodel.BinlogCoordinates{LogFile: logFile})
	if distance < 0 {
		return nil, log.Errorf("FlushBinaryLogsTo: target log file %+v is smaller than current log file %+v", logFile, instance.SelfBinlogCoordinates.LogFile)
	}
	return FlushBinaryLogs(instanceKey, distance)
}

// purgeBinaryLogsTo attempts to 'PURGE BINARY LOGS' until given binary log is reached
func PurgeBinaryLogsTo(instanceKey *instmodel.InstanceKey, logFile string) (*instmodel.Instance, error) {
	if *config.RuntimeCLIFlags.Noop {
		return nil, fmt.Errorf("noop: aborting purge-binary-logs operation on %+v; signalling error but nothing went wrong", *instanceKey)
	}

	err := ExecuteInstanceCommand(instanceKey, "purge binary logs to ?", logFile)
	if err != nil {
		return nil, log.Errore(err)
	}

	log.Infof("purge-binary-logs to=%+v on %+v", logFile, *instanceKey)
	instaudit.AuditOperation("purge-binary-logs", instanceKey, "success")

	return instdiscovery.ReadTopologyInstance(instanceKey)
}

func SetSemiSyncMaster(instanceKey *instmodel.InstanceKey, enableMaster bool) (*instmodel.Instance, error) {
	instance, err := instdiscovery.ReadTopologyInstance(instanceKey)
	if err != nil {
		return instance, err
	}
	query := "set @@global.rpl_semi_sync_master_enabled=?"
	if instance.SemiSyncMasterPluginNewVersion {
		query = "set @@global.rpl_semi_sync_source_enabled=?"
	}
	if err := ExecuteInstanceCommand(instanceKey, query, enableMaster); err != nil {
		return instance, log.Errore(err)
	}
	return instdiscovery.ReadTopologyInstance(instanceKey)
}

func SetSemiSyncReplica(instanceKey *instmodel.InstanceKey, enableReplica bool) (*instmodel.Instance, error) {
	instance, err := instdiscovery.ReadTopologyInstance(instanceKey)
	if err != nil {
		return instance, err
	}
	if instance.SemiSyncReplicaEnabled == enableReplica {
		return instance, nil
	}
	query := "set @@global.rpl_semi_sync_slave_enabled=?"
	if instance.SemiSyncReplicaPluginNewVersion {
		query = "set @@global.rpl_semi_sync_replica_enabled=?"
	}

	if err := ExecuteInstanceCommand(instanceKey, query, enableReplica); err != nil {
		return instance, log.Errore(err)
	}
	if instance.ReplicationIOThreadRuning {
		// Need to apply change by stopping starting IO thread
		ExecuteInstanceCommand(instanceKey, mysqlquery.Query(instance.Version, mysqlquery.StopSlaveIOThread))
		if err := ExecuteInstanceCommand(instanceKey, mysqlquery.Query(instance.Version, mysqlquery.StartSlaveIOThread)); err != nil {
			return instance, log.Errore(err)
		}
	}
	return instdiscovery.ReadTopologyInstance(instanceKey)
}

func RestartReplicationQuick(instance *instmodel.Instance, instanceKey *instmodel.InstanceKey) error {
	for _, cmd := range []string{mysqlquery.Query(instance.Version, mysqlquery.StopSlaveIOThread), mysqlquery.Query(instance.Version, mysqlquery.StartSlaveIOThread)} {
		if err := ExecuteInstanceCommand(instanceKey, cmd); err != nil {
			return log.Errorf("%+v: RestartReplicationQuick: '%q' failed: %+v", *instanceKey, cmd, err)
		} else {
			log.Infof("%s on %+v as part of RestartReplicationQuick", cmd, *instanceKey)
		}
	}
	return nil
}

// StopReplicationNicely stops a replica such that SQL_thread and IO_thread are aligned (i.e.
// SQL_thread consumes all relay log entries)
// It will actually START the sql_thread even if the replica is completely stopped.
func StopReplicationNicely(instanceKey *instmodel.InstanceKey, timeout time.Duration) (*instmodel.Instance, error) {
	instance, err := instdiscovery.ReadTopologyInstance(instanceKey)
	if err != nil {
		return instance, log.Errore(err)
	}

	if !instance.ReplicationThreadsExist() {
		return instance, fmt.Errorf("instance is not a replica: %+v", instanceKey)
	}

	// stop io_thread, start sql_thread but catch any errors
	for _, cmd := range []string{mysqlquery.Query(instance.Version, mysqlquery.StopSlaveIOThread), mysqlquery.Query(instance.Version, mysqlquery.StartSlaveSQLThread)} {
		if err := ExecuteInstanceCommand(instanceKey, cmd); err != nil {
			return nil, log.Errorf("%+v: StopReplicationNicely: '%q' failed: %+v", *instanceKey, cmd, err)
		}
	}

	if instance.SQLDelay == 0 {
		// Otherwise we don't bother.
		if instance, err = WaitForSQLThreadUpToDate(instanceKey, timeout, 0); err != nil {
			return instance, err
		}
	}

	err = ExecuteInstanceCommand(instanceKey, mysqlquery.Query(instance.Version, mysqlquery.StopSlave))
	if err != nil {
		// Patch; current MaxScale behavior for STOP SLAVE is to throw an error if replica already stopped.
		if instance.IsMaxScale() && err.Error() == "Error 1199: Slave connection is not running" {
			err = nil
		}
	}
	if err != nil {
		return instance, log.Errore(err)
	}

	instance, err = instdiscovery.ReadTopologyInstance(instanceKey)
	log.Infof("Stopped replication nicely on %+v, Self:%+v, Exec:%+v", *instanceKey, instance.SelfBinlogCoordinates, instance.ExecBinlogCoordinates)
	return instance, err
}

func WaitForSQLThreadUpToDate(instanceKey *instmodel.InstanceKey, overallTimeout time.Duration, staleCoordinatesTimeout time.Duration) (instance *instmodel.Instance, err error) {
	// Otherwise we don't bother.
	var lastExecBinlogCoordinates instmodel.BinlogCoordinates

	if overallTimeout == 0 {
		overallTimeout = 24 * time.Hour
	}
	if staleCoordinatesTimeout == 0 {
		staleCoordinatesTimeout = time.Duration(recoverypolicy.Current("").ReasonableReplicationLagSeconds) * time.Second
	}
	generalTimer := time.NewTimer(overallTimeout)
	staleTimer := time.NewTimer(staleCoordinatesTimeout)
	for {
		instance, err := instdiscovery.RetryInstanceFunction(func() (*instmodel.Instance, error) {
			return instdiscovery.ReadTopologyInstance(instanceKey)
		})
		if err != nil {
			return instance, log.Errore(err)
		}

		if instance.SQLThreadUpToDate() {
			// Woohoo
			return instance, nil
		}
		if instance.SQLDelay != 0 {
			return instance, log.Errorf("WaitForSQLThreadUpToDate: instance %+v has SQL Delay %+v. Operation is irrelevant", *instanceKey, instance.SQLDelay)
		}

		if !instance.ExecBinlogCoordinates.Equals(&lastExecBinlogCoordinates) {
			// means we managed to apply binlog events. We made progress...
			// so we reset the "staleness" timer
			if !staleTimer.Stop() {
				<-staleTimer.C
			}
			staleTimer.Reset(staleCoordinatesTimeout)
		}
		lastExecBinlogCoordinates = instance.ExecBinlogCoordinates

		select {
		case <-generalTimer.C:
			return instance, log.Errorf("WaitForSQLThreadUpToDate timeout on %+v after duration %+v", *instanceKey, overallTimeout)
		case <-staleTimer.C:
			return instance, log.Errorf("WaitForSQLThreadUpToDate stale coordinates timeout on %+v after duration %+v", *instanceKey, staleCoordinatesTimeout)
		default:
			log.Debugf("WaitForSQLThreadUpToDate waiting on %+v", *instanceKey)
			time.Sleep(retryInterval)
		}
	}
}

// StopReplicas will stop replication concurrently on given set of replicas.
// It will potentially do nothing, or attempt to stop _nicely_ or just stop normally, all according to stopReplicationMethod
func StopReplicas(replicas []*instmodel.Instance, stopReplicationMethod StopReplicationMethod, timeout time.Duration) []*instmodel.Instance {
	if stopReplicationMethod == NoStopReplication {
		return replicas
	}
	refreshedReplicas := []*instmodel.Instance{}

	log.Debugf("Stopping %d replicas via %s", len(replicas), string(stopReplicationMethod))
	// use concurrency but wait for all to complete
	barrier := make(chan *instmodel.Instance)
	for _, replica := range replicas {
		go func() {
			updatedReplica := &replica
			// Signal completed replica
			defer func() { barrier <- *updatedReplica }()
			// Wait your turn to read a replica
			ExecuteOnTopology(func() {
				if stopReplicationMethod == StopReplicationNice && !replica.IsMariaDB() {
					StopReplicationNicely(&replica.Key, timeout)
				}
				replica, _ = StopReplication(&replica.Key)
				updatedReplica = &replica
			})
		}()
	}
	for range replicas {
		refreshedReplicas = append(refreshedReplicas, <-barrier)
	}
	return refreshedReplicas
}

// StopReplication stops replication on a given instance
func StopReplication(instanceKey *instmodel.InstanceKey) (*instmodel.Instance, error) {
	instance, err := instdiscovery.ReadTopologyInstance(instanceKey)
	if err != nil {
		return instance, log.Errore(err)
	}

	if !instance.IsReplica() {
		return instance, fmt.Errorf("instance is not a replica: %+v", instanceKey)
	}

	err = ExecuteInstanceCommand(instanceKey, mysqlquery.Query(instance.Version, mysqlquery.StopSlave))
	if err != nil {
		// Patch; current MaxScale behavior for STOP SLAVE is to throw an error if replica already stopped.
		if instance.IsMaxScale() && err.Error() == "Error 1199: Slave connection is not running" {
			err = nil
		}
	}
	if err != nil {
		return instance, log.Errore(err)
	}
	instance, err = instdiscovery.ReadTopologyInstance(instanceKey)

	log.Infof("Stopped replication on %+v, Self:%+v, Exec:%+v", *instanceKey, instance.SelfBinlogCoordinates, instance.ExecBinlogCoordinates)
	return instance, err
}

// waitForReplicationState waits for both replication threads to be either running or not running, together.
// This is useful post- `start slave` operation, ensuring both threads are actually running,
// or post `stop slave` operation, ensuring both threads are not running.
func WaitForReplicationState(instance *instmodel.Instance, instanceKey *instmodel.InstanceKey, expectedState instmodel.ReplicationThreadState) (expectationMet bool, err error) {
	waitDuration := time.Second
	waitInterval := 10 * time.Millisecond
	startTime := time.Now()

	for {
		// Since this is an incremental aggressive polling, it's OK if an occasional
		// error is observed. We don't bail out on a single error.
		if expectationMet, _ := instdiscovery.ExpectReplicationThreadsState(instance, instanceKey, expectedState); expectationMet {
			return true, nil
		}
		if time.Since(startTime)+waitInterval > waitDuration {
			break
		}
		time.Sleep(waitInterval)
		waitInterval = 2 * waitInterval
	}
	return false, nil
}

// StartReplication starts replication on a given instance.
func StartReplication(instanceKey *instmodel.InstanceKey) (*instmodel.Instance, error) {
	instance, err := instdiscovery.ReadTopologyInstance(instanceKey)
	if err != nil {
		return instance, log.Errore(err)
	}

	if !instance.IsReplica() {
		return instance, fmt.Errorf("instance is not a replica: %+v", instanceKey)
	}

	instance, err = MaybeDisableSemiSyncMaster(instance)
	if err != nil {
		return instance, log.Errore(err)
	}

	// If async fallback is disallowed, we'd better make sure to enable replicas to
	// send ACKs before START SLAVE. Replica ACKing is off at mysqld startup because
	// some replicas (those that must never be promoted) should never ACK.
	// Note: We assume that replicas use 'skip-slave-start' so they won't
	//       START SLAVE on their own upon restart.
	instance, err = MaybeEnableSemiSyncReplica(instance)
	if err != nil {
		return instance, log.Errore(err)
	}

	err = ExecuteInstanceCommand(instanceKey, mysqlquery.Query(instance.Version, mysqlquery.StartSlave))
	if err != nil {
		return instance, log.Errore(err)
	}
	log.Infof("Started replication on %+v", instanceKey)

	WaitForReplicationState(instance, instanceKey, instmodel.ReplicationThreadStateRunning)

	instance, err = instdiscovery.ReadTopologyInstance(instanceKey)
	if err != nil {
		return instance, log.Errore(err)
	}
	if !instance.ReplicaRunning() {
		return instance, ErrReplicationNotRunning
	}
	return instance, nil
}

// RestartReplication stops & starts replication on a given instance
func RestartReplication(instanceKey *instmodel.InstanceKey) (instance *instmodel.Instance, err error) {
	instance, err = StopReplication(instanceKey)
	if err != nil {
		return instance, log.Errore(err)
	}
	instance, err = StartReplication(instanceKey)
	return instance, log.Errore(err)
}

// StartReplicas will do concurrent start-replica
func StartReplicas(replicas []*instmodel.Instance) {
	// use concurrency but wait for all to complete
	log.Debugf("Starting %d replicas", len(replicas))
	barrier := make(chan instmodel.InstanceKey)
	for _, instance := range replicas {
		go func() {
			// Signal compelted replica
			defer func() { barrier <- instance.Key }()
			// Wait your turn to read a replica
			ExecuteOnTopology(func() { StartReplication(&instance.Key) })
		}()
	}
	for range replicas {
		<-barrier
	}
}

func WaitForExecBinlogCoordinatesToReach(instanceKey *instmodel.InstanceKey, coordinates *instmodel.BinlogCoordinates, maxWait time.Duration) (instance *instmodel.Instance, exactMatch bool, err error) {
	startTime := time.Now()
	for {
		if maxWait != 0 && time.Since(startTime) > maxWait {
			return nil, exactMatch, fmt.Errorf("WaitForExecBinlogCoordinatesToReach: reached maxWait %+v on %+v", maxWait, *instanceKey)
		}
		instance, err = instdiscovery.ReadTopologyInstance(instanceKey)
		if err != nil {
			return instance, exactMatch, log.Errore(err)
		}

		switch {
		case instance.ExecBinlogCoordinates.SmallerThan(coordinates):
			time.Sleep(retryInterval)
		case instance.ExecBinlogCoordinates.Equals(coordinates):
			return instance, true, nil
		case coordinates.SmallerThan(&instance.ExecBinlogCoordinates):
			return instance, false, nil
		}
	}
}

// StartReplicationUntilMasterCoordinates issues a START SLAVE UNTIL... statement on given instance
func StartReplicationUntilMasterCoordinates(instanceKey *instmodel.InstanceKey, masterCoordinates *instmodel.BinlogCoordinates) (*instmodel.Instance, error) {
	instance, err := instdiscovery.ReadTopologyInstance(instanceKey)
	if err != nil {
		return instance, log.Errore(err)
	}

	if !instance.IsReplica() {
		return instance, fmt.Errorf("instance is not a replica: %+v", instanceKey)
	}
	if !instance.ReplicationThreadsStopped() {
		return instance, fmt.Errorf("replication threads are not stopped: %+v", instanceKey)
	}

	log.Infof("Will start replication on %+v until coordinates: %+v", instanceKey, masterCoordinates)

	instance, err = MaybeDisableSemiSyncMaster(instance)
	if err != nil {
		return instance, log.Errore(err)
	}
	instance, err = MaybeEnableSemiSyncReplica(instance)
	if err != nil {
		return instance, log.Errore(err)
	}

	// MariaDB has a bug: a CHANGE MASTER TO statement does not work properly with prepared statement... :P
	// See https://mariadb.atlassian.net/browse/MDEV-7640
	// This is the reason for ExecuteInstanceCommand
	err = ExecuteInstanceCommand(instanceKey, mysqlquery.Query(instance.Version, mysqlquery.StartSlaveUntilMasterLog),
		masterCoordinates.LogFile, masterCoordinates.LogPos)
	if err != nil {
		return instance, log.Errore(err)
	}

	instance, exactMatch, err := WaitForExecBinlogCoordinatesToReach(instanceKey, masterCoordinates, 0)
	if err != nil {
		return instance, log.Errore(err)
	}
	if !exactMatch {
		return instance, fmt.Errorf("start SLAVE UNTIL is past coordinates: %+v", instanceKey)
	}

	instance, err = StopReplication(instanceKey)
	if err != nil {
		return instance, log.Errore(err)
	}

	return instance, err
}

// MaybeDisableSemiSyncMaster always disables the semi-sync master (rpl_semi_sync_master_enabled) if the semi-sync priority is > 0. This is
// a little odd but in line with the legacy behavior and we really should disable the semi-sync master flag for replicas when starting replication.
func MaybeDisableSemiSyncMaster(replicaInstance *instmodel.Instance) (*instmodel.Instance, error) {
	if replicaInstance.SemiSyncPriority > 0 && replicaInstance.SemiSyncMasterEnabled {
		log.Infof("semi-sync: %s: setting rpl_semi_sync_master_enabled: %t", &replicaInstance.Key, false)
		replicaInstance, err := SetSemiSyncMaster(&replicaInstance.Key, false)
		if err != nil {
			log.Warningf("semi-sync: %s: cannot disable rpl_semi_sync_master_enabled; that's not that bad though", &replicaInstance.Key)
		}
		return replicaInstance, err
	}
	return replicaInstance, nil
}

// MaybeEnableSemiSyncReplica sets the semi-sync replica variable (rpl_semi_sync_replica_enabled) on a given instance based on the config and
// state of the world. If EnforceExactSemiSyncReplicas or RecoverLockedSemiSyncMaster are enabled, the semi-sync replica variable is enabled
// only if the given instance is supposed to be enabled according to the semi-sync priority order and the number of desired semi-sync replicas.
// If the flags are both turned off, the legacy behavior kicks in: If SemiSyncPriority > 0 and the instance is promotable (not "must_not"),
// semi-sync is enabled.
func MaybeEnableSemiSyncReplica(replicaInstance *instmodel.Instance) (*instmodel.Instance, error) {
	// Backwards compatible logic: Enable semi-sync if SemiSyncPriority > 0 (formerly SemiSyncEnforced)
	// Note that this logic NEVER enables semi-sync if the promotion rule is "must_not".
	policy := recoverypolicy.Current(replicaInstance.ClusterName)
	if !policy.EnforceExactSemiSyncReplicas && !policy.RecoverLockedSemiSyncMaster {
		return maybeEnableSemiSyncReplicaLegacy(replicaInstance)
	}

	// New logic: If EnforceExactSemiSyncReplicas or RecoverLockedSemiSyncMaster are set, we enable semi-sync only if the
	// given replica instance is in the list of replicas to have semi-sync enabled (according to the priority).
	_, _, actions, err := AnalyzeSemiSyncReplicaTopology(&replicaInstance.MasterKey, &replicaInstance.Key, policy.EnforceExactSemiSyncReplicas)
	if err != nil {
		return replicaInstance, log.Errorf("semi-sync: %s", err.Error())
	}
	for replica, enable := range actions {
		if replica.Key.Equals(&replicaInstance.Key) {
			log.Infof("semi-sync: %s: setting rpl_semi_sync_slave_enabled=%t, restarting slave_io thread", replica.Key.String(), enable)
			if _, err := SetSemiSyncReplica(&replica.Key, enable); err != nil {
				return nil, fmt.Errorf("cannot enable semi sync on replica %+v", replica.Key)
			}
			return replicaInstance, nil
		}
	}

	// We are not taking any action for anything but replicaInstance, so if we detect that another replica has to be enabled,
	// we won't act here and leave it to a future MasterWithTooManySemiSyncReplicas or LockedSemiSyncMaster event to correct.

	log.Infof("semi-sync: %+v: no action taken; this may lead to future recoveries", &replicaInstance.Key)
	return replicaInstance, nil
}

// maybeEnableSemiSyncReplicaLegacy enable semi-sync if SemiSyncPriority > 0 (formerly SemiSyncEnforced). This is a backwards
// compatible logic that NEVER enables semi-sync if the promotion rule is "must_not".
func maybeEnableSemiSyncReplicaLegacy(replicaInstance *instmodel.Instance) (*instmodel.Instance, error) {
	if replicaInstance.SemiSyncPriority > 0 {
		enable := replicaInstance.PromotionRule != instmodel.MustNotPromoteRule // Send ACK only from promotable instances
		log.Infof("semi-sync: %+v: setting rpl_semi_sync_slave_enabled = %t (legacy behavior)", &replicaInstance.Key, enable)
		return SetSemiSyncReplica(&replicaInstance.Key, enable)
	}
	return replicaInstance, nil
}

// AnalyzeSemiSyncReplicaTopology analyzes the replica topology for the given master and determines actions for the semi-sync replica enabled
// variable. It does not take any action itself.
func AnalyzeSemiSyncReplicaTopology(masterKey *instmodel.InstanceKey, includeNonReplicatingInstance *instmodel.InstanceKey, exactReplicaTopology bool) (masterInstance *instmodel.Instance, replicas []*instmodel.Instance, actions map[*instmodel.Instance]bool, err error) {
	// Read entire topology of master and its replicas to ensure we have the most up-to-date information
	masterInstance, err = instdiscovery.ReadTopologyInstance(masterKey)
	if err != nil {
		return nil, nil, nil, err
	}
	replicas, err = instdiscovery.ReadTopologyInstances(masterInstance.Replicas.GetInstanceKeys())
	if err != nil {
		replicas, err = instinventory.ReadReplicaInstances(masterKey) // Falling back to just reading from our backend
		if err != nil {
			return nil, nil, nil, err
		}
	}

	// Classify and prioritize replicas & figure out which replicas need to be acted upon
	possibleSemiSyncReplicas, asyncReplicas, excludedReplicas := classifyAndPrioritizeReplicas(replicas, includeNonReplicatingInstance)
	actions = determineSemiSyncReplicaActions(masterInstance, possibleSemiSyncReplicas, asyncReplicas, exactReplicaTopology)
	logSemiSyncReplicaAnalysis(masterInstance, possibleSemiSyncReplicas, asyncReplicas, excludedReplicas, actions)

	return masterInstance, replicas, actions, nil
}

// classifyAndPrioritizeReplicas takes a list of replica instances and classifies them based on their semi-sync priority, excluding replicas
// that are down. The function furthermore prioritizes the possible semi-sync replicas based on SemiSyncPriority, PromotionRule and hostname (fallback).
func classifyAndPrioritizeReplicas(replicas []*instmodel.Instance, includeNonReplicatingInstance *instmodel.InstanceKey) (possibleSemiSyncReplicas []*instmodel.Instance, asyncReplicas []*instmodel.Instance, excludedReplicas []*instmodel.Instance) {
	// Classify based on state and semi-sync priority
	possibleSemiSyncReplicas = make([]*instmodel.Instance, 0)
	asyncReplicas = make([]*instmodel.Instance, 0)
	excludedReplicas = make([]*instmodel.Instance, 0)
	for _, replica := range replicas {
		isReplicating := replica.Key.Equals(includeNonReplicatingInstance) || replica.ReplicaRunning()
		if !replica.IsLastCheckValid || !isReplicating {
			excludedReplicas = append(excludedReplicas, replica)
		} else if replica.SemiSyncPriority == 0 {
			asyncReplicas = append(asyncReplicas, replica)
		} else {
			possibleSemiSyncReplicas = append(possibleSemiSyncReplicas, replica)
		}
	}

	// Sort replicas by priority (higher number means higher priority), promotion rule and name
	sort.Slice(possibleSemiSyncReplicas, func(i, j int) bool {
		if possibleSemiSyncReplicas[i].SemiSyncPriority != possibleSemiSyncReplicas[j].SemiSyncPriority {
			return possibleSemiSyncReplicas[i].SemiSyncPriority > possibleSemiSyncReplicas[j].SemiSyncPriority
		}
		if possibleSemiSyncReplicas[i].PromotionRule != possibleSemiSyncReplicas[j].PromotionRule {
			return possibleSemiSyncReplicas[i].PromotionRule.BetterThan(possibleSemiSyncReplicas[j].PromotionRule)
		}
		return strings.Compare(possibleSemiSyncReplicas[i].Key.String(), possibleSemiSyncReplicas[j].Key.String()) < 0
	})

	return
}

// determineSemiSyncReplicaActions returns a map of replicas for which to change the semi-sync replica setting.
// A value of true indicates semi-sync needs to be enabled, false that it needs to be disabled.
func determineSemiSyncReplicaActions(masterInstance *instmodel.Instance, possibleSemiSyncReplicas []*instmodel.Instance, asyncReplicas []*instmodel.Instance, exactReplicaTopology bool) map[*instmodel.Instance]bool {
	if exactReplicaTopology {
		return determineSemiSyncReplicaActionsForExactTopology(masterInstance, possibleSemiSyncReplicas, asyncReplicas)
	}
	return determineSemiSyncReplicaActionsForEnoughTopology(masterInstance, possibleSemiSyncReplicas)
}

// determineSemiSyncReplicaActionsForExactTopology takes a priority-list of possible semi-sync replicas and always-async replicas and returns a list
// of actions to perform on them. If the current state of a replica's semi-sync flag does not match the desired state, an action is returned for it.
func determineSemiSyncReplicaActionsForExactTopology(masterInstance *instmodel.Instance, possibleSemiSyncReplicas []*instmodel.Instance, asyncReplicas []*instmodel.Instance) map[*instmodel.Instance]bool {
	actions := make(map[*instmodel.Instance]bool, 0) // true = enable semi-sync, false = disable semi-sync
	for i, replica := range possibleSemiSyncReplicas {
		isSemiSyncEnabled := replica.SemiSyncReplicaEnabled
		shouldSemiSyncBeEnabled := uint(i) < masterInstance.SemiSyncMasterWaitForReplicaCount
		if shouldSemiSyncBeEnabled && !isSemiSyncEnabled {
			actions[replica] = true
		} else if !shouldSemiSyncBeEnabled && isSemiSyncEnabled {
			actions[replica] = false
		}
	}
	for _, replica := range asyncReplicas {
		if replica.SemiSyncReplicaEnabled {
			actions[replica] = false
		}
	}
	return actions
}

// determineSemiSyncReplicaActionsForEnoughTopology takes a priority-list of possible semi-sync replicas and returns a list of actions to increase the
// number of semi-sync replicas to the semi-sync master wait count. This function will never return actions to disable a semi-sync replica.
func determineSemiSyncReplicaActionsForEnoughTopology(masterInstance *instmodel.Instance, possibleSemiSyncReplicas []*instmodel.Instance) map[*instmodel.Instance]bool {
	actions := make(map[*instmodel.Instance]bool, 0) // true = enable semi-sync, false = disable semi-sync
	enabled := uint(0)
	for _, replica := range possibleSemiSyncReplicas {
		if !replica.SemiSyncReplicaEnabled {
			actions[replica] = true
			enabled++
		}
		if enabled == masterInstance.SemiSyncMasterWaitForReplicaCount-masterInstance.SemiSyncMasterClients {
			break
		}
	}
	return actions
}

func logSemiSyncReplicaAnalysis(masterInstance *instmodel.Instance, possibleSemiSyncReplicas []*instmodel.Instance, asyncReplicas []*instmodel.Instance, excludedReplicas []*instmodel.Instance, actions map[*instmodel.Instance]bool) {
	log.Debugf("semi-sync: analysis results for recovery of cluster %+v:", masterInstance.ClusterName)
	log.Debugf("semi-sync: master = %+v, master semi-sync wait count = %d, master semi-sync replica count = %d", masterInstance.Key, masterInstance.SemiSyncMasterWaitForReplicaCount, masterInstance.SemiSyncMasterClients)
	logSemiSyncReplicaList("possible semi-sync replicas (in priority order)", possibleSemiSyncReplicas)
	logSemiSyncReplicaList("always-async replicas", asyncReplicas)
	logSemiSyncReplicaList("excluded replicas (defunct)", excludedReplicas)
	if len(actions) > 0 {
		log.Debugf("semi-sync: suggested actions:")
		for replica, enable := range actions {
			log.Debugf("semi-sync: - %+v: should set semi-sync enabled = %t", replica.Key, enable)
		}
	} else {
		log.Debugf("semi-sync: suggested actions: (none)")
	}
}

func logSemiSyncReplicaList(description string, replicas []*instmodel.Instance) {
	if len(replicas) > 0 {
		log.Debugf("semi-sync: %s:", description)
		for _, replica := range replicas {
			log.Debugf("semi-sync: - %s: semi-sync enabled = %t, priority = %d, promotion rule = %s, last check = %t, replicating = %t", replica.Key.String(), replica.SemiSyncReplicaEnabled, replica.SemiSyncPriority, replica.PromotionRule, replica.IsLastCheckValid, replica.ReplicaRunning())
		}
	} else {
		log.Debugf("semi-sync: %s: (none)", description)
	}
}

// DelayReplication set the replication delay given seconds
// keeping the current state of the replication threads.
func DelayReplication(instanceKey *instmodel.InstanceKey, seconds int) error {
	if seconds < 0 {
		return fmt.Errorf("invalid seconds: %d, it should be greater or equal to 0", seconds)
	}

	instance, err := instdiscovery.ReadTopologyInstance(instanceKey)

	if err != nil {
		return err
	}

	query := fmt.Sprintf(mysqlquery.Query(instance.Version, mysqlquery.ChangeMasterToMasterDelay), seconds)
	statements, err := GetReplicationRestartPreserveStatements(instanceKey, query)
	if err != nil {
		return err
	}
	for _, cmd := range statements {
		if err := ExecuteInstanceCommand(instanceKey, cmd); err != nil {
			return log.Errorf("%+v: DelayReplication: '%q' failed: %+v", *instanceKey, cmd, err)
		} else {
			log.Infof("DelayReplication: %s on %+v", cmd, *instanceKey)
		}
	}
	instaudit.AuditOperation("delay-replication", instanceKey, fmt.Sprintf("set to %d", seconds))
	return nil
}

// ChangeMasterCredentials issues a CHANGE MASTER TO... MASTER_USER=, MASTER_PASSWORD=...
func ChangeMasterCredentials(instanceKey *instmodel.InstanceKey, creds *modeldomain.ReplicationCredentials) (*instmodel.Instance, error) {
	instance, err := instdiscovery.ReadTopologyInstance(instanceKey)
	if err != nil {
		return instance, log.Errore(err)
	}
	if creds.User == "" {
		return instance, log.Errorf("Empty user in ChangeMasterCredentials() for %+v", *instanceKey)
	}

	if instance.ReplicationThreadsExist() && !instance.ReplicationThreadsStopped() {
		return instance, fmt.Errorf("ChangeMasterTo: Cannot change master on: %+v because replication is running", *instanceKey)
	}
	log.Debugf("ChangeMasterTo: will attempt changing master credentials on %+v", *instanceKey)

	if *config.RuntimeCLIFlags.Noop {
		return instance, fmt.Errorf("noop: aborting CHANGE MASTER TO operation on %+v; signalling error but nothing went wrong", *instanceKey)
	}

	var query_params []string
	var query_params_args []any

	// User
	query_params = append(query_params, mysqlquery.Query(instance.Version, mysqlquery.MasterUserParam))
	query_params_args = append(query_params_args, creds.User)
	// Password
	if creds.Password != "" {
		query_params = append(query_params, mysqlquery.Query(instance.Version, mysqlquery.MasterPasswordParam))
		query_params_args = append(query_params_args, creds.Password)
	}

	// Prefer SSL material supplied via creds over what we read from the replica,
	// then let appendReplicationChangeTLSFragments emit the full TLS profile
	// (CA path, cipher, verify-server-cert, TLS version, etc.) so this CHANGE
	// REPLICATION SOURCE does not drop TLS metadata the replica already had.
	if creds.SSLCaCert != "" {
		instance.ReplicationSSLCAFile = creds.SSLCaCert
		instance.AllowTLS = true
	}
	if creds.SSLCert != "" {
		instance.ReplicationSSLCert = creds.SSLCert
		instance.AllowTLS = true
	}
	if creds.SSLKey != "" {
		instance.ReplicationSSLKey = creds.SSLKey
		instance.AllowTLS = true
	}
	appendReplicationChangeTLSFragments(instance, &query_params, &query_params_args)

	query := fmt.Sprintf(mysqlquery.Query(instance.Version, mysqlquery.ChangeMasterToWithParams), strings.Join(query_params, ", "))
	err = ExecuteInstanceCommand(instanceKey, query, query_params_args...)

	if err != nil {
		return instance, log.Errore(err)
	}

	log.Infof("ChangeMasterTo: Changed master credentials on %+v", *instanceKey)

	instance, err = instdiscovery.ReadTopologyInstance(instanceKey)
	return instance, err
}

func EnableMasterGetSourcePublicKey(instanceKey *instmodel.InstanceKey) (*instmodel.Instance, error) {
	instance, err := instdiscovery.ReadTopologyInstance(instanceKey)
	if err != nil {
		return instance, log.Errore(err)
	}

	if instance.ReplicationThreadsExist() && !instance.ReplicationThreadsStopped() {
		return instance, fmt.Errorf("EnableMasterGetSourcePublicKey: Cannot enable GetSourcePublicKey replication on %+v because replication threads are not stopped", *instanceKey)
	}
	log.Debugf("EnableMasterGetSourcePublicKey: Will attempt enabling GetSourcePublicKey replication on %+v", *instanceKey)

	if *config.RuntimeCLIFlags.Noop {
		return instance, fmt.Errorf("noop: aborting CHANGE REPLICATION SOURCE TO GET_SOURCE_PUBLIC_KEY=1 operation on %+v; signaling error but nothing went wrong", *instanceKey)
	}
	err = ExecuteInstanceCommand(instanceKey, mysqlquery.Query(instance.Version, mysqlquery.ChangeMasterToGetSourcePublicKey))

	if err != nil {
		return instance, log.Errore(err)
	}

	log.Infof("EnableMasterGetSourcePublicKey: Enabled GetSourcePublicKey replication on %+v", *instanceKey)

	instance, err = instdiscovery.ReadTopologyInstance(instanceKey)
	return instance, err
}

func appendReplicationChangeTLSFragments(instance *instmodel.Instance, queryParams *[]string, queryArgs *[]any) {
	if instance.AllowTLS {
		*queryParams = append(*queryParams, mysqlquery.Query(instance.Version, mysqlquery.MasterSSL)+" = 1")
		if instance.ReplicationSSLCAFile != "" {
			*queryParams = append(*queryParams, mysqlquery.Query(instance.Version, mysqlquery.MasterSSLCAParam))
			*queryArgs = append(*queryArgs, instance.ReplicationSSLCAFile)
		}
		if instance.ReplicationSSLCAPath != "" {
			*queryParams = append(*queryParams, mysqlquery.Query(instance.Version, mysqlquery.MasterSSLCapathParam))
			*queryArgs = append(*queryArgs, instance.ReplicationSSLCAPath)
		}
		if instance.ReplicationSSLCert != "" {
			*queryParams = append(*queryParams, mysqlquery.Query(instance.Version, mysqlquery.MasterSSLCertParam))
			*queryArgs = append(*queryArgs, instance.ReplicationSSLCert)
		}
		if instance.ReplicationSSLCipher != "" {
			*queryParams = append(*queryParams, mysqlquery.Query(instance.Version, mysqlquery.MasterSSLCipherParam))
			*queryArgs = append(*queryArgs, instance.ReplicationSSLCipher)
		}
		if instance.ReplicationSSLCRLFile != "" {
			*queryParams = append(*queryParams, mysqlquery.Query(instance.Version, mysqlquery.MasterSSLCRLParam))
			*queryArgs = append(*queryArgs, instance.ReplicationSSLCRLFile)
		}
		if instance.ReplicationSSLCRLPath != "" {
			*queryParams = append(*queryParams, mysqlquery.Query(instance.Version, mysqlquery.MasterSSLCRLPathParam))
			*queryArgs = append(*queryArgs, instance.ReplicationSSLCRLPath)
		}
		if instance.ReplicationSSLKey != "" {
			*queryParams = append(*queryParams, mysqlquery.Query(instance.Version, mysqlquery.MasterSSLKeyParam))
			*queryArgs = append(*queryArgs, instance.ReplicationSSLKey)
		}
		if instance.ReplicationSSLVerifyServerCert.Valid {
			v := 0
			if instance.ReplicationSSLVerifyServerCert.Bool {
				v = 1
			}
			*queryParams = append(*queryParams, mysqlquery.Query(instance.Version, mysqlquery.MasterSSLVerifyServerCertParam))
			*queryArgs = append(*queryArgs, v)
		}
		if instance.ReplicationTLSVersion != "" {
			*queryParams = append(*queryParams, mysqlquery.Query(instance.Version, mysqlquery.MasterTLSVersionParam))
			*queryArgs = append(*queryArgs, instance.ReplicationTLSVersion)
		}
		if instance.ReplicationTLSCiphersuites != "" {
			*queryParams = append(*queryParams, mysqlquery.Query(instance.Version, mysqlquery.MasterTLSCipherSuitesParam))
			*queryArgs = append(*queryArgs, instance.ReplicationTLSCiphersuites)
		}
	}
	// RSA public-key auth settings (SOURCE_PUBLIC_KEY_PATH / GET_SOURCE_PUBLIC_KEY)
	// are independent of TLS and must be preserved on every repoint, otherwise
	// replicas using caching_sha2_password without TLS stop authenticating after
	// a failover/switchover.
	if instance.ReplicationSourcePublicKeyPath != "" {
		*queryParams = append(*queryParams, mysqlquery.Query(instance.Version, mysqlquery.MasterPublicKeyPathParam))
		*queryArgs = append(*queryArgs, instance.ReplicationSourcePublicKeyPath)
	}
	if instance.ReplicationGetSourcePublicKey.Valid {
		v := 0
		if instance.ReplicationGetSourcePublicKey.Bool {
			v = 1
		}
		*queryParams = append(*queryParams, mysqlquery.Query(instance.Version, mysqlquery.MasterGetSourcePublicKeyParam))
		*queryArgs = append(*queryArgs, v)
	}
}

func changeReplicationSourceWithTLS(instanceKey *instmodel.InstanceKey, instance *instmodel.Instance, params []string, args []any) error {
	appendReplicationChangeTLSFragments(instance, &params, &args)
	query := fmt.Sprintf(mysqlquery.Query(instance.Version, mysqlquery.ChangeMasterToWithParams), strings.Join(params, ", "))
	err := ExecuteInstanceCommand(instanceKey, query, args...)
	return err
}

// See https://bugs.mysql.com/bug.php?id=83713
func workaroundBug83713(instance *instmodel.Instance, instanceKey *instmodel.InstanceKey) {
	log.Debugf("workaroundBug83713: %+v", *instanceKey)
	queries := []string{
		mysqlquery.Query(instance.Version, mysqlquery.ResetSlave),
		mysqlquery.Query(instance.Version, mysqlquery.StartSlaveIOThread),
		mysqlquery.Query(instance.Version, mysqlquery.StopSlaveIOThread),
		mysqlquery.Query(instance.Version, mysqlquery.ResetSlave),
	}
	for _, query := range queries {
		if err := ExecuteInstanceCommand(instanceKey, query); err != nil {
			log.Debugf("workaroundBug83713: error on %s: %+v", query, err)
		}
	}
}

// ChangeMasterTo changes the given instance's master according to given input.
func ChangeMasterTo(instanceKey *instmodel.InstanceKey, masterKey *instmodel.InstanceKey, masterBinlogCoordinates *instmodel.BinlogCoordinates, skipUnresolve bool, gtidHint modeldomain.OperationGTIDHint) (*instmodel.Instance, error) {
	instance, err := instdiscovery.ReadTopologyInstance(instanceKey)
	if err != nil {
		return instance, log.Errore(err)
	}

	if instance.ReplicationThreadsExist() && !instance.ReplicationThreadsStopped() {
		return instance, fmt.Errorf("ChangeMasterTo: Cannot change master on: %+v because replication threads are not stopped", *instanceKey)
	}
	log.Debugf("ChangeMasterTo: will attempt changing master on %+v to %+v, %+v", *instanceKey, *masterKey, *masterBinlogCoordinates)
	changeToMasterKey := masterKey
	if !skipUnresolve {
		unresolvedMasterKey, nameUnresolved, err := instresolve.UnresolveHostname(masterKey, instdiscovery.ReadTopologyInstance)
		if err != nil {
			log.Debugf("ChangeMasterTo: aborting operation on %+v due to resolving error on %+v: %+v", *instanceKey, *masterKey, err)
			return instance, err
		}
		if nameUnresolved {
			log.Debugf("ChangeMasterTo: Unresolved %+v into %+v", *masterKey, unresolvedMasterKey)
		}
		changeToMasterKey = &unresolvedMasterKey
	}

	if *config.RuntimeCLIFlags.Noop {
		return instance, fmt.Errorf("noop: aborting CHANGE MASTER TO operation on %+v; signalling error but nothing went wrong", *instanceKey)
	}

	originalMasterKey := instance.MasterKey
	originalExecBinlogCoordinates := instance.ExecBinlogCoordinates

	var changeMasterFunc func() error
	changedViaGTID := false
	if instance.UsingMariaDBGTID && gtidHint != modeldomain.GTIDHintDeny {
		// Keep on using GTID
		changeMasterFunc = func() error {
			params := []string{mysqlquery.Query(instance.Version, mysqlquery.MasterHostAssign), mysqlquery.Query(instance.Version, mysqlquery.MasterPortAssign)}
			args := []any{changeToMasterKey.Hostname, changeToMasterKey.Port}
			return changeReplicationSourceWithTLS(instanceKey, instance, params, args)
		}
		changedViaGTID = true
	} else if instance.UsingMariaDBGTID && gtidHint == modeldomain.GTIDHintDeny {
		// Make sure to not use GTID
		changeMasterFunc = func() error {
			params := []string{
				mysqlquery.Query(instance.Version, mysqlquery.MasterHostAssign),
				mysqlquery.Query(instance.Version, mysqlquery.MasterPortAssign),
				mysqlquery.Query(instance.Version, mysqlquery.MasterLogFileAssign),
				mysqlquery.Query(instance.Version, mysqlquery.MasterLogPosAssign),
				"master_use_gtid=no",
			}
			args := []any{
				changeToMasterKey.Hostname,
				changeToMasterKey.Port,
				masterBinlogCoordinates.LogFile,
				masterBinlogCoordinates.LogPos,
			}
			return changeReplicationSourceWithTLS(instanceKey, instance, params, args)
		}
	} else if instance.IsMariaDB() && gtidHint == modeldomain.GTIDHintForce {
		// Is MariaDB; not using GTID, turn into GTID
		// his is MariaDB. Leave master/slave wording for now
		mariadbGTIDHint := "slave_pos"
		if !instance.ReplicationThreadsExist() {
			// This instance is currently a master. As per https://mariadb.com/kb/en/change-master-to/#master_use_gtid
			// we should be using current_pos.
			// See also:
			// - https://github.com/openark/orchestrator/issues/1146
			// - https://dba.stackexchange.com/a/234323
			mariadbGTIDHint = "current_pos"
		}
		changeMasterFunc = func() error {
			params := []string{
				mysqlquery.Query(instance.Version, mysqlquery.MasterHostAssign),
				mysqlquery.Query(instance.Version, mysqlquery.MasterPortAssign),
				fmt.Sprintf("master_use_gtid=%s", mariadbGTIDHint),
			}
			args := []any{changeToMasterKey.Hostname, changeToMasterKey.Port}
			return changeReplicationSourceWithTLS(instanceKey, instance, params, args)
		}
		changedViaGTID = true
	} else if instance.UsingOracleGTID && gtidHint != modeldomain.GTIDHintDeny {
		// Is Oracle; already uses GTID; keep using it.
		changeMasterFunc = func() error {
			params := []string{mysqlquery.Query(instance.Version, mysqlquery.MasterHostAssign), mysqlquery.Query(instance.Version, mysqlquery.MasterPortAssign)}
			args := []any{changeToMasterKey.Hostname, changeToMasterKey.Port}
			return changeReplicationSourceWithTLS(instanceKey, instance, params, args)
		}
		changedViaGTID = true
	} else if instance.UsingOracleGTID && gtidHint == modeldomain.GTIDHintDeny {
		// Is Oracle; already uses GTID
		changeMasterFunc = func() error {
			params := []string{
				mysqlquery.Query(instance.Version, mysqlquery.MasterHostAssign),
				mysqlquery.Query(instance.Version, mysqlquery.MasterPortAssign),
				mysqlquery.Query(instance.Version, mysqlquery.MasterLogFileAssign),
				mysqlquery.Query(instance.Version, mysqlquery.MasterLogPosAssign),
				mysqlquery.Query(instance.Version, mysqlquery.MasterAutoPositionAssign),
			}
			args := []any{
				changeToMasterKey.Hostname,
				changeToMasterKey.Port,
				masterBinlogCoordinates.LogFile,
				masterBinlogCoordinates.LogPos,
				0,
			}
			return changeReplicationSourceWithTLS(instanceKey, instance, params, args)
		}
	} else if instance.SupportsOracleGTID && gtidHint == modeldomain.GTIDHintForce {
		// Is Oracle; not using GTID right now; turn into GTID
		changeMasterFunc = func() error {
			params := []string{
				mysqlquery.Query(instance.Version, mysqlquery.MasterHostAssign),
				mysqlquery.Query(instance.Version, mysqlquery.MasterPortAssign),
				mysqlquery.Query(instance.Version, mysqlquery.MasterAutoPositionAssign),
			}
			args := []any{changeToMasterKey.Hostname, changeToMasterKey.Port, 1}
			return changeReplicationSourceWithTLS(instanceKey, instance, params, args)
		}
		changedViaGTID = true
	} else {
		// Normal binlog file:pos
		changeMasterFunc = func() error {
			params := []string{
				mysqlquery.Query(instance.Version, mysqlquery.MasterHostAssign),
				mysqlquery.Query(instance.Version, mysqlquery.MasterPortAssign),
				mysqlquery.Query(instance.Version, mysqlquery.MasterLogFileAssign),
				mysqlquery.Query(instance.Version, mysqlquery.MasterLogPosAssign),
			}
			args := []any{
				changeToMasterKey.Hostname,
				changeToMasterKey.Port,
				masterBinlogCoordinates.LogFile,
				masterBinlogCoordinates.LogPos,
			}
			return changeReplicationSourceWithTLS(instanceKey, instance, params, args)
		}
	}
	err = changeMasterFunc()
	if err != nil && instance.UsingOracleGTID && strings.Contains(err.Error(), Error1201CouldnotInitializeMasterInfoStructure) {
		log.Debugf("ChangeMasterTo: got %+v", err)
		workaroundBug83713(instance, instanceKey)
		err = changeMasterFunc()
	}
	if err != nil {
		return instance, log.Errore(err)
	}
	instequivalence.WriteMasterPositionEquivalence(&originalMasterKey, &originalExecBinlogCoordinates, changeToMasterKey, masterBinlogCoordinates)
	instinventory.ResetInstanceRelaylogCoordinatesHistory(instanceKey)

	log.Infof("ChangeMasterTo: Changed master on %+v to: %+v, %+v. GTID: %+v", *instanceKey, masterKey, masterBinlogCoordinates, changedViaGTID)

	instance, err = instdiscovery.ReadTopologyInstance(instanceKey)
	return instance, err
}

// SkipToNextBinaryLog changes master position to beginning of next binlog
// USE WITH CARE!
// Use case is binlog servers where the master was gone & replaced by another.
func SkipToNextBinaryLog(instanceKey *instmodel.InstanceKey) (*instmodel.Instance, error) {
	instance, err := instdiscovery.ReadTopologyInstance(instanceKey)
	if err != nil {
		return instance, log.Errore(err)
	}

	nextFileCoordinates, err := instance.ExecBinlogCoordinates.NextFileCoordinates()
	if err != nil {
		return instance, log.Errore(err)
	}
	nextFileCoordinates.LogPos = 4
	log.Debugf("Will skip replication on %+v to next binary log: %+v", instance.Key, nextFileCoordinates.LogFile)

	instance, err = ChangeMasterTo(&instance.Key, &instance.MasterKey, &nextFileCoordinates, false, modeldomain.GTIDHintNeutral)
	if err != nil {
		return instance, log.Errore(err)
	}
	instaudit.AuditOperation("skip-binlog", instanceKey, fmt.Sprintf("Skipped replication to next binary log: %+v", nextFileCoordinates.LogFile))
	return StartReplication(instanceKey)
}

// ResetReplication resets a replica, breaking the replication
func ResetReplication(instanceKey *instmodel.InstanceKey) (*instmodel.Instance, error) {
	instance, err := instdiscovery.ReadTopologyInstance(instanceKey)
	if err != nil {
		return instance, log.Errore(err)
	}

	if instance.ReplicationThreadsExist() && !instance.ReplicationThreadsStopped() {
		return instance, fmt.Errorf("cannot reset replication on: %+v because replication threads are not stopped", instanceKey)
	}

	if *config.RuntimeCLIFlags.Noop {
		return instance, fmt.Errorf("noop: aborting reset-replication operation on %+v; signalling error but nothing went wrong", *instanceKey)
	}

	// MySQL's RESET SLAVE is done correctly; however SHOW SLAVE STATUS still returns old hostnames etc
	// and only resets till after next restart. This leads to orchestrator still thinking the instance replicates
	// from old host. We therefore forcibly modify the hostname.
	// RESET SLAVE ALL command solves this, but only as of 5.6.3
	err = ExecuteInstanceCommand(instanceKey, mysqlquery.Query(instance.Version, mysqlquery.ChangeMasterToMasterHost))
	if err != nil {
		return instance, log.Errore(err)
	}
	err = ExecuteInstanceCommand(instanceKey, mysqlquery.Query(instance.Version, mysqlquery.ResetSlave50603All))
	if err != nil && strings.Contains(err.Error(), Error1201CouldnotInitializeMasterInfoStructure) {
		log.Debugf("ResetReplication: got %+v", err)
		workaroundBug83713(instance, instanceKey)
		err = ExecuteInstanceCommand(instanceKey, mysqlquery.Query(instance.Version, mysqlquery.ResetSlave50603All))
	}
	if err != nil {
		return instance, log.Errore(err)
	}
	log.Infof("Reset replication %+v", instanceKey)

	instance, err = instdiscovery.ReadTopologyInstance(instanceKey)
	return instance, err
}

// ResetMaster issues a RESET MASTER statement on given instance. Use with extreme care!
func ResetMaster(instanceKey *instmodel.InstanceKey) (*instmodel.Instance, error) {
	instance, err := instdiscovery.ReadTopologyInstance(instanceKey)
	if err != nil {
		return instance, log.Errore(err)
	}

	if instance.ReplicationThreadsExist() && !instance.ReplicationThreadsStopped() {
		return instance, fmt.Errorf("cannot reset master on: %+v because replication threads are not stopped", instanceKey)
	}

	if *config.RuntimeCLIFlags.Noop {
		return instance, fmt.Errorf("noop: aborting reset-master operation on %+v; signalling error but nothing went wrong", *instanceKey)
	}

	err = ExecuteInstanceCommand(instanceKey, mysqlquery.Query(instance.Version, mysqlquery.ResetMaster))
	if err != nil {
		return instance, log.Errore(err)
	}
	log.Infof("Reset master %+v", instanceKey)

	instance, err = instdiscovery.ReadTopologyInstance(instanceKey)
	return instance, err
}

// skipQueryClassic skips a query in normal binlog file:pos replication
func SetGTIDPurged(instance *instmodel.Instance, gtidPurged string) error {
	if *config.RuntimeCLIFlags.Noop {
		return fmt.Errorf("noop: aborting set-gtid-purged operation on %+v; signalling error but nothing went wrong", instance.Key)
	}

	err := ExecuteInstanceCommand(&instance.Key, `set global gtid_purged := ?`, gtidPurged)
	return err
}

// injectEmptyGTIDTransaction
func InjectEmptyGTIDTransaction(instanceKey *instmodel.InstanceKey, gtidEntry *gtid.OracleGtidSetEntry) error {
	db, err := topology.Open(instanceKey.Hostname, instanceKey.Port)
	if err != nil {
		return err
	}
	ctx := context.Background()
	return db.InjectEmptyGTIDTransaction(ctx, gtidEntry.String())
}

// skipQueryClassic skips a query in normal binlog file:pos replication
func skipQueryClassic(instance *instmodel.Instance) error {
	err := ExecuteInstanceCommand(&instance.Key, mysqlquery.Query(instance.Version, mysqlquery.SetSQLSlaveSkipCounter))
	return err
}

// skipQueryOracleGtid skips a single query in an Oracle GTID replicating replica, by injecting an empty transaction
func skipQueryOracleGtid(instance *instmodel.Instance) error {
	nextGtid, err := instance.NextGTID()
	if err != nil {
		return err
	}
	if nextGtid == "" {
		return fmt.Errorf("empty NextGTID() in skipQueryGtid() for %+v", instance.Key)
	}
	if err := ExecuteInstanceCommand(&instance.Key, `SET GTID_NEXT=?`, nextGtid); err != nil {
		return err
	}
	if err := ProbeInstanceCommit(&instance.Key); err != nil {
		return err
	}
	if err := ExecuteInstanceCommand(&instance.Key, `SET GTID_NEXT='AUTOMATIC'`); err != nil {
		return err
	}
	return nil
}

// SkipQuery skip a single query in a failed replication instance
func SkipQuery(instanceKey *instmodel.InstanceKey) (*instmodel.Instance, error) {
	instance, err := instdiscovery.ReadTopologyInstance(instanceKey)
	if err != nil {
		return instance, log.Errore(err)
	}

	if !instance.IsReplica() {
		return instance, fmt.Errorf("instance is not a replica: %+v", instanceKey)
	}
	if instance.ReplicationSQLThreadRuning {
		return instance, fmt.Errorf("replication SQL thread is running on %+v", instanceKey)
	}
	if instance.LastSQLError == "" {
		return instance, fmt.Errorf("no SQL error on %+v", instanceKey)
	}

	if *config.RuntimeCLIFlags.Noop {
		return instance, fmt.Errorf("noop: aborting skip-query operation on %+v; signalling error but nothing went wrong", *instanceKey)
	}

	log.Debugf("Skipping one query on %+v", instanceKey)
	if instance.UsingOracleGTID {
		err = skipQueryOracleGtid(instance)
	} else if instance.UsingMariaDBGTID {
		return instance, log.Errorf("%+v is replicating with MariaDB GTID. To skip a query first disable GTID, then skip, then enable GTID again", *instanceKey)
	} else {
		err = skipQueryClassic(instance)
	}
	if err != nil {
		return instance, log.Errore(err)
	}
	instaudit.AuditOperation("skip-query", instanceKey, "Skipped one query")
	return StartReplication(instanceKey)
}

// MasterPosWait issues a MASTER_POS_WAIT() an given instance according to given coordinates.
func MasterPosWait(instanceKey *instmodel.InstanceKey, binlogCoordinates *instmodel.BinlogCoordinates) (*instmodel.Instance, error) {
	instance, err := instdiscovery.ReadTopologyInstance(instanceKey)
	if err != nil {
		return instance, log.Errore(err)
	}

	err = ExecuteInstanceCommand(instanceKey, mysqlquery.Query(instance.Version, mysqlquery.SelectMasterPosWait), binlogCoordinates.LogFile, binlogCoordinates.LogPos)
	if err != nil {
		return instance, log.Errore(err)
	}
	log.Infof("Instance %+v has reached coordinates: %+v", instanceKey, binlogCoordinates)

	instance, err = instdiscovery.ReadTopologyInstance(instanceKey)
	return instance, err
}

// Attempt to read and return replication credentials from the mysql.slave_master_info system table
func ReadReplicationCredentials(instanceKey *instmodel.InstanceKey) (creds *modeldomain.ReplicationCredentials, err error) {
	creds = &modeldomain.ReplicationCredentials{}
	if config.Config.Topology.Replication.CredentialsQuery != "" {
		db, err := topology.Open(instanceKey.Hostname, instanceKey.Port)
		if err != nil {
			return creds, log.Errore(err)
		}
		{
			resultData, err := db.ReadResultData(config.Config.Topology.Replication.CredentialsQuery)
			if err != nil {
				return creds, log.Errore(err)
			}
			if len(resultData) > 0 {
				// A row is found
				row := resultData[0]
				if len(row) > 0 {
					creds.User = row[0].String
				}
				if len(row) > 1 {
					creds.Password = row[1].String
				}
				if len(row) > 2 {
					creds.SSLCaCert = row[2].String
				}
				if len(row) > 3 {
					creds.SSLCert = row[3].String
				}
				if len(row) > 4 {
					creds.SSLKey = row[4].String
				}
			}
		}
		if err == nil && creds.User == "" {
			err = fmt.Errorf("empty username retrieved by ReplicationCredentialsQuery")
		}
		if err == nil {
			return creds, nil
		}
		log.Errore(err)
	}
	// Didn't get credentials from ReplicationCredentialsQuery, or ReplicationCredentialsQuery doesn't exist in the first place?
	// We brute force our way through mysql.slave_master_info
	{
		// mysql.slave_master_info remains available in MySQL 8.4.
		query := `
			select
				ifnull(max(User_name), '') as user,
				ifnull(max(User_password), '') as password
			from
				mysql.slave_master_info
		`
		err = ReadInstanceRow(instanceKey, query, &creds.User, &creds.Password)
		if err == nil && creds.User == "" {
			err = fmt.Errorf("empty username found in mysql.slave_master_info")
		}

	}
	return creds, log.Errore(err)
}

// SetReadOnly sets or clears the instance's global read_only variable
func SetReadOnly(instanceKey *instmodel.InstanceKey, readOnly bool) (*instmodel.Instance, error) {
	instance, err := instdiscovery.ReadTopologyInstance(instanceKey)
	if err != nil {
		return instance, log.Errore(err)
	}

	if *config.RuntimeCLIFlags.Noop {
		return instance, fmt.Errorf("noop: aborting set-read-only operation on %+v; signalling error but nothing went wrong", *instanceKey)
	}

	// If async fallback is disallowed, we're responsible for flipping the master
	// semi-sync switch ON before accepting writes. The setting is off by default.
	if instance.SemiSyncPriority > 0 && !readOnly {
		if _, err := SetSemiSyncMaster(instanceKey, true); err != nil {
			return instance, log.Errore(err)
		}
	}

	if err := ExecuteInstanceCommand(instanceKey, "set global read_only = ?", readOnly); err != nil {
		return instance, log.Errore(err)
	}
	if config.Config.Topology.Operations.UseSuperReadOnly {
		if err := ExecuteInstanceCommand(instanceKey, "set global super_read_only = ?", readOnly); err != nil {
			// We don't bail out here. super_read_only is only available on
			// MySQL 5.7.8 and Percona Server 5.6.21-70
			// At this time orchestrator does not verify whether a server supports super_read_only or not.
			// It makes a best effort to set it.
			log.Errore(err)
		}
	}
	instance, err = instdiscovery.ReadTopologyInstance(instanceKey)
	if err != nil {
		return instance, log.Errore(err)
	}

	// If we just went read-only, it's safe to flip the master semi-sync switch
	// OFF, which is the default value so that replicas can make progress.
	if instance.SemiSyncPriority > 0 && readOnly {
		if _, err := SetSemiSyncMaster(instanceKey, false); err != nil {
			return instance, log.Errore(err)
		}
	}

	log.Infof("instance %+v read_only: %t", instanceKey, readOnly)
	instaudit.AuditOperation("read-only", instanceKey, fmt.Sprintf("set as %t", readOnly))

	return instance, err
}

// KillQuery stops replication on a given instance
func KillQuery(instanceKey *instmodel.InstanceKey, process int64) (*instmodel.Instance, error) {
	instance, err := instdiscovery.ReadTopologyInstance(instanceKey)
	if err != nil {
		return instance, log.Errore(err)
	}

	if *config.RuntimeCLIFlags.Noop {
		return instance, fmt.Errorf("noop: aborting kill-query operation on %+v; signalling error but nothing went wrong", *instanceKey)
	}

	err = ExecuteInstanceCommand(instanceKey, `kill query ?`, process)
	if err != nil {
		return instance, log.Errore(err)
	}

	instance, err = instdiscovery.ReadTopologyInstance(instanceKey)
	if err != nil {
		return instance, log.Errore(err)
	}

	log.Infof("Killed query on %+v", *instanceKey)
	instaudit.AuditOperation("kill-query", instanceKey, fmt.Sprintf("Killed query %d", process))
	return instance, err
}

// injectPseudoGTID injects a Pseudo-GTID statement on a writable instance
func injectPseudoGTID(instance *instmodel.Instance) (hint string, err error) {
	if *config.RuntimeCLIFlags.Noop {
		return hint, fmt.Errorf("noop: aborting inject-pseudo-gtid operation on %+v; signalling error but nothing went wrong", instance.Key)
	}

	now := time.Now()
	randomHash := util.RandomHash()[0:16]
	hint = fmt.Sprintf("%.8x:%.8x:%s", now.Unix(), instance.ServerID, randomHash)
	query := fmt.Sprintf("drop view if exists `%s`.`_asc:%s`", config.PseudoGTIDSchema, hint)
	err = ExecuteInstanceCommand(&instance.Key, query)
	return hint, log.Errore(err)
}

// canInjectPseudoGTID checks orchestrator's grants to determine whether is has the
// privilege of auto-injecting pseudo-GTID
func canInjectPseudoGTID(instanceKey *instmodel.InstanceKey) (canInject bool, err error) {
	if canInject, found := supportedAutoPseudoGTIDWriters.Get(instanceKey.StringCode()); found {
		return canInject.(bool), nil
	}
	db, err := topology.Open(instanceKey.Hostname, instanceKey.Port)
	if err != nil {
		return canInject, err
	}

	foundAll := false
	foundDropOnAll := false
	foundAllOnSchema := false
	foundDropOnSchema := false

	err = db.ReadDynamicRows(`show grants for current_user()`, func(m modeldomain.DynamicRow) error {
		for _, grantData := range m {
			grant := grantData.String
			if strings.Contains(grant, `GRANT ALL PRIVILEGES ON *.*`) {
				foundAll = true
			}
			if strings.Contains(grant, `DROP`) && strings.Contains(grant, ` ON *.*`) {
				foundDropOnAll = true
			}
			if strings.Contains(grant, fmt.Sprintf("GRANT ALL PRIVILEGES ON `%s`.*", config.PseudoGTIDSchema)) {
				foundAllOnSchema = true
			}
			if strings.Contains(grant, fmt.Sprintf(`GRANT ALL PRIVILEGES ON "%s".*`, config.PseudoGTIDSchema)) {
				foundAllOnSchema = true
			}
			if strings.Contains(grant, `DROP`) && strings.Contains(grant, fmt.Sprintf(" ON `%s`.*", config.PseudoGTIDSchema)) {
				foundDropOnSchema = true
			}
			if strings.Contains(grant, `DROP`) && strings.Contains(grant, fmt.Sprintf(` ON "%s".*`, config.PseudoGTIDSchema)) {
				foundDropOnSchema = true
			}
		}
		return nil
	})
	if err != nil {
		return canInject, err
	}

	canInject = foundAll || foundDropOnAll || foundAllOnSchema || foundDropOnSchema
	supportedAutoPseudoGTIDWriters.Set(instanceKey.StringCode(), canInject, cache.DefaultExpiration)

	return canInject, nil
}

// CheckAndInjectPseudoGTIDOnWriter checks whether pseudo-GTID can and
// should be injected on given instance, and if so, attempts to inject.
func CheckAndInjectPseudoGTIDOnWriter(instance *instmodel.Instance) (injected bool, err error) {
	if instance == nil {
		return injected, log.Errorf("CheckAndInjectPseudoGTIDOnWriter: instance is nil")
	}
	if instance.ReadOnly {
		return injected, log.Errorf("CheckAndInjectPseudoGTIDOnWriter: instance is read-only: %+v", instance.Key)
	}
	if !instance.IsLastCheckValid {
		return injected, nil
	}
	canInject, err := canInjectPseudoGTID(&instance.Key)
	if err != nil {
		return injected, log.Errore(err)
	}
	if !canInject {
		if util.ClearToLog("CheckAndInjectPseudoGTIDOnWriter", instance.Key.StringCode()) {
			log.Warningf("AutoPseudoGTID enabled, but orchestrator has no privileges on %+v to inject pseudo-gtid", instance.Key)
		}

		return injected, nil
	}
	if _, err := injectPseudoGTID(instance); err != nil {
		return injected, log.Errore(err)
	}
	injected = true
	if err := instinventory.RegisterInjectedPseudoGTID(instance.ClusterName); err != nil {
		return injected, log.Errore(err)
	}
	return injected, nil
}

func GTIDSubtract(instanceKey *instmodel.InstanceKey, gtidSet string, gtidSubset string) (gtidSubtract string, err error) {
	db, err := topology.Open(instanceKey.Hostname, instanceKey.Port)
	if err != nil {
		return gtidSubtract, err
	}
	err = db.ReadArgs("select gtid_subtract(?, ?)", []any{gtidSet, gtidSubset}, &gtidSubtract)
	return gtidSubtract, err
}

func ShowMasterStatus(instance *instmodel.Instance, instanceKey *instmodel.InstanceKey) (masterStatusFound bool, executedGtidSet string, err error) {
	db, err := topology.Open(instanceKey.Hostname, instanceKey.Port)
	if err != nil {
		return masterStatusFound, executedGtidSet, err
	}
	err = db.ReadDynamicRows(mysqlquery.Query(instance.Version, mysqlquery.ShowMasterStatus), func(m modeldomain.DynamicRow) error {
		masterStatusFound = true
		executedGtidSet = m.GetStringD("Executed_Gtid_Set", "")
		return nil
	})
	return masterStatusFound, executedGtidSet, err
}

func ShowBinaryLogs(instanceKey *instmodel.InstanceKey) (binlogs []string, err error) {
	db, err := topology.Open(instanceKey.Hostname, instanceKey.Port)
	if err != nil {
		return binlogs, err
	}
	err = db.ReadDynamicRows("show binary logs", func(m modeldomain.DynamicRow) error {
		binlogs = append(binlogs, m.GetString("Log_name"))
		return nil
	})
	return binlogs, err
}
