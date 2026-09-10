package inst

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/golib/log"
	modeldomain "github.com/openark/orchestrator/internal/models/domain"
	"github.com/openark/orchestrator/internal/observability"
	"github.com/openark/orchestrator/internal/recoverypolicy"
	"github.com/openark/orchestrator/internal/repository/metadata"
	"github.com/patrickmn/go-cache"
)

const tooManyPlaceholders = "Error 1390: Prepared statement contains too many placeholders"

func instanceWriteRow(instance *Instance) modeldomain.BackendInstanceRecord {
	return modeldomain.BackendInstanceRecord{
		Hostname:                          instance.Key.Hostname,
		Port:                              instance.Key.Port,
		Uptime:                            instance.Uptime,
		ServerID:                          instance.ServerID,
		ServerUUID:                        instance.ServerUUID,
		Version:                           instance.Version,
		MajorVersion:                      instance.MajorVersionString(),
		VersionComment:                    instance.VersionComment,
		BinlogServer:                      instance.IsBinlogServer(),
		ReadOnly:                          instance.ReadOnly,
		BinlogFormat:                      instance.Binlog_format,
		BinlogRowImage:                    instance.BinlogRowImage,
		LogBin:                            instance.LogBinEnabled,
		LogSlaveUpdates:                   instance.LogReplicationUpdatesEnabled,
		BinaryLogFile:                     instance.SelfBinlogCoordinates.LogFile,
		BinaryLogPos:                      instance.SelfBinlogCoordinates.LogPos,
		MasterHost:                        instance.MasterKey.Hostname,
		MasterPort:                        instance.MasterKey.Port,
		SlaveSQLRunning:                   instance.ReplicationSQLThreadRuning,
		SlaveIORunning:                    instance.ReplicationIOThreadRuning,
		ReplicationSQLThreadState:         int(instance.ReplicationSQLThreadState),
		ReplicationIOThreadState:          int(instance.ReplicationIOThreadState),
		HasReplicationFilters:             instance.HasReplicationFilters,
		SupportsOracleGTID:                instance.SupportsOracleGTID,
		OracleGTID:                        instance.UsingOracleGTID,
		MasterUUID:                        instance.MasterUUID,
		AncestryUUID:                      instance.AncestryUUID,
		ExecutedGTIDSet:                   instance.ExecutedGtidSet,
		GTIDMode:                          instance.GTIDMode,
		GTIDPurged:                        instance.GtidPurged,
		GTIDErrant:                        instance.GtidErrant,
		MariaDBGTID:                       instance.UsingMariaDBGTID,
		PseudoGTID:                        instance.UsingPseudoGTID,
		MasterLogFile:                     instance.ReadBinlogCoordinates.LogFile,
		ReadMasterLogPos:                  instance.ReadBinlogCoordinates.LogPos,
		RelayMasterLogFile:                instance.ExecBinlogCoordinates.LogFile,
		ExecMasterLogPos:                  instance.ExecBinlogCoordinates.LogPos,
		RelayLogFile:                      instance.RelaylogCoordinates.LogFile,
		RelayLogPos:                       instance.RelaylogCoordinates.LogPos,
		LastSQLError:                      instance.LastSQLError,
		LastIOError:                       instance.LastIOError,
		SecondsBehindMaster:               instance.SecondsBehindMaster.SQLNullInt64(),
		SlaveLagSeconds:                   instance.ReplicationLagSeconds.SQLNullInt64(),
		SQLDelay:                          instance.SQLDelay,
		NumSlaveHosts:                     len(instance.Replicas),
		SlaveHosts:                        instance.Replicas.ToJSONString(),
		ClusterName:                       instance.ClusterName,
		SuggestedClusterAlias:             instance.SuggestedClusterAlias,
		DataCenter:                        instance.DataCenter,
		Region:                            instance.Region,
		PhysicalEnvironment:               instance.PhysicalEnvironment,
		ReplicationDepth:                  instance.ReplicationDepth,
		CoMaster:                          instance.IsCoMaster,
		ReplicationCredentialsAvailable:   instance.ReplicationCredentialsAvailable,
		HasReplicationCredentials:         instance.HasReplicationCredentials,
		AllowTLS:                          instance.AllowTLS,
		SemiSyncEnforced:                  instance.SemiSyncPriority,
		SemiSyncAvailable:                 instance.SemiSyncAvailable,
		SemiSyncMasterEnabled:             instance.SemiSyncMasterEnabled,
		SemiSyncMasterTimeout:             instance.SemiSyncMasterTimeout,
		SemiSyncMasterWaitForReplicaCount: instance.SemiSyncMasterWaitForReplicaCount,
		SemiSyncReplicaEnabled:            instance.SemiSyncReplicaEnabled,
		SemiSyncMasterStatus:              instance.SemiSyncMasterStatus,
		SemiSyncMasterClients:             instance.SemiSyncMasterClients,
		SemiSyncReplicaStatus:             instance.SemiSyncReplicaStatus,
		InstanceAlias:                     instance.InstanceAlias,
		LastDiscoveryLatency:              instance.LastDiscoveryLatency.Nanoseconds(),
		ReplicationGroupName:              instance.ReplicationGroupName,
		ReplicationGroupSinglePrimary:     instance.ReplicationGroupIsSinglePrimary,
		ReplicationGroupMemberState:       instance.ReplicationGroupMemberState,
		ReplicationGroupMemberRole:        instance.ReplicationGroupMemberRole,
		ReplicationGroupMembers:           instance.ReplicationGroupMembers.ToJSONString(),
		ReplicationGroupPrimaryHost:       instance.ReplicationGroupPrimaryInstanceKey.Hostname,
		ReplicationGroupPrimaryPort:       instance.ReplicationGroupPrimaryInstanceKey.Port,
	}
}

func writeManyInstances(instances []*Instance, instanceWasActuallyFound bool, updateLastSeen bool) error {
	writeInstances := [](*Instance){}
	for _, instance := range instances {
		if InstanceIsForgotten(&instance.Key) && !instance.IsSeed() {
			continue
		}
		writeInstances = append(writeInstances, instance)
	}
	if len(writeInstances) == 0 {
		return nil // nothing to write
	}
	rows := make([]modeldomain.BackendInstanceRecord, 0, len(writeInstances))
	for _, instance := range writeInstances {
		rows = append(rows, instanceWriteRow(instance))
	}
	argumentCount, err := metadata.WriteInstanceRows(context.Background(), rows, instanceWasActuallyFound, updateLastSeen)
	if err != nil {
		if strings.Contains(err.Error(), tooManyPlaceholders) {
			return fmt.Errorf("writeManyInstances(?,%v,%v): error: %+v, len(instances): %v, len(args): %v.  Reduce InstanceWriteBufferSize to avoid len(args) being > 64k, a limit in the MySQL source code.",
				instanceWasActuallyFound,
				updateLastSeen,
				err.Error(),
				len(writeInstances),
				argumentCount)
		}
		return err
	}
	return nil
}

type instanceUpdateObject struct {
	instance                 *Instance
	instanceWasActuallyFound bool
	lastError                error
}

// instances sorter by instanceKey
type byInstanceKey []*Instance

func (a byInstanceKey) Len() int           { return len(a) }
func (a byInstanceKey) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a byInstanceKey) Less(i, j int) bool { return a[i].Key.SmallerThan(&a[j].Key) }

var instanceWriteBuffer chan instanceUpdateObject
var forceFlushInstanceWriteBuffer = make(chan bool)

func enqueueInstanceWrite(instance *Instance, instanceWasActuallyFound bool, lastError error) {
	if len(instanceWriteBuffer) == config.Config.Topology.WriteBuffer.Size {
		// Signal the "flushing" goroutine that there's work.
		// We prefer doing all bulk flushes from one goroutine.
		// Non blocking send to avoid blocking goroutines on sending a flush,
		// if the "flushing" goroutine is not able read is because a flushing is ongoing.
		select {
		case forceFlushInstanceWriteBuffer <- true:
		default:
		}
	}
	instanceWriteBuffer <- instanceUpdateObject{instance, instanceWasActuallyFound, lastError}
}

// flushInstanceWriteBuffer saves enqueued instances to Orchestrator Db
func flushInstanceWriteBuffer() {
	var instances []*Instance
	var lastseen []*Instance // instances to update with last_seen field

	if len(instanceWriteBuffer) == 0 {
		return
	}

	// There are `DiscoveryMaxConcurrency` many goroutines trying to enqueue an instance into the buffer
	// when one instance is flushed from the buffer then one discovery goroutine is ready to enqueue a new instance
	// this is why we want to flush all instances in the buffer until a max of `InstanceWriteBufferSize`.
	// Otherwise we can flush way more instances than what's expected.
	for i := 0; i < config.Config.Topology.WriteBuffer.Size && len(instanceWriteBuffer) > 0; i++ {
		upd := <-instanceWriteBuffer
		if upd.instanceWasActuallyFound && upd.lastError == nil {
			lastseen = append(lastseen, upd.instance)
		} else {
			instances = append(instances, upd.instance)
			log.Debugf("flushInstanceWriteBuffer: will not update database_instance.last_seen due to error: %+v", upd.lastError)
		}
	}

	flushStarted := time.Now()

	// sort instances by instanceKey (table pk) to make locking predictable
	sort.Sort(byInstanceKey(instances))
	sort.Sort(byInstanceKey(lastseen))

	writeFunc := func() error {
		err := writeManyInstances(instances, true, false)
		if err != nil {
			return log.Errorf("flushInstanceWriteBuffer writemany: %v", err)
		}
		err = writeManyInstances(lastseen, true, true)
		if err != nil {
			return log.Errorf("flushInstanceWriteBuffer last_seen: %v", err)
		}

		writeInstanceCounter.Add(context.Background(), int64(len(instances)+len(lastseen)))
		return nil
	}
	err := ExecDBWriteFunc(writeFunc)
	if err != nil {
		log.Errorf("flushInstanceWriteBuffer: %v", err)
	}

	observability.RecordFlush(context.Background(), err, time.Since(flushStarted), len(lastseen)+len(instances))
}

// WriteInstance stores an instance in the orchestrator backend
func WriteInstance(instance *Instance, instanceWasActuallyFound bool, lastError error) error {
	if lastError != nil {
		log.Debugf("writeInstance: will not update database_instance due to error: %+v", lastError)
		return nil
	}
	return writeManyInstances([]*Instance{instance}, instanceWasActuallyFound, true)
}

// UpdateInstanceLastChecked updates the last_check timestamp in the orchestrator backed database
// for a given instance
func UpdateInstanceLastChecked(instanceKey *InstanceKey, partialSuccess bool) error {
	writeFunc := func() error {
		err := metadata.UpdateInstanceLastChecked(
			context.Background(), instanceKey.Hostname, instanceKey.Port, partialSuccess,
		)
		return log.Errore(err)
	}
	return ExecDBWriteFunc(writeFunc)
}

// UpdateInstanceLastAttemptedCheck updates the last_attempted_check timestamp in the orchestrator backed database
// for a given instance.
// This is used as a failsafe mechanism in case access to the instance gets hung (it happens), in which case
// the entire ReadTopology gets stuck (and no, connection timeout nor driver timeouts don't help. Don't look at me,
// the world is a harsh place to live in).
// And so we make sure to note down *before* we even attempt to access the instance; and this raises a red flag when we
// wish to access the instance again: if last_attempted_check is *newer* than last_checked, that's bad news and means
// we have a "hanging" issue.
func UpdateInstanceLastAttemptedCheck(instanceKey *InstanceKey) error {
	writeFunc := func() error {
		err := metadata.UpdateInstanceLastAttemptedCheck(
			context.Background(), instanceKey.Hostname, instanceKey.Port,
		)
		return log.Errore(err)
	}
	return ExecDBWriteFunc(writeFunc)
}

func InstanceIsForgotten(instanceKey *InstanceKey) bool {
	_, found := forgetInstanceKeys.Get(instanceKey.StringCode())
	return found
}

// ForgetInstance removes an instance entry from the orchestrator backed database.
// It may be auto-rediscovered through topology or requested for discovery by multiple means.
func ForgetInstance(instanceKey *InstanceKey) error {
	if instanceKey == nil {
		return log.Errorf("ForgetInstance(): nil instanceKey")
	}
	forgetInstanceKeys.Set(instanceKey.StringCode(), true, cache.DefaultExpiration)
	rows, err := metadata.ForgetInstance(context.Background(), instanceKey.Hostname, instanceKey.Port)
	if err != nil {
		return log.Errore(err)
	}
	if rows == 0 {
		return log.Errorf("ForgetInstance(): instance %+v not found", *instanceKey)
	}
	AuditOperation("forget", instanceKey, "")
	DeadInstancesFilter.UnregisterInstance(instanceKey)
	return nil
}

// ForgetInstance removes an instance entry from the orchestrator backed database.
// It may be auto-rediscovered through topology or requested for discovery by multiple means.
func ForgetCluster(clusterName string) error {
	clusterInstances, err := ReadClusterInstances(clusterName)
	if err != nil {
		return err
	}
	if len(clusterInstances) == 0 {
		return nil
	}
	for _, instance := range clusterInstances {
		forgetInstanceKeys.Set(instance.Key.StringCode(), true, cache.DefaultExpiration)
		AuditOperation("forget", &instance.Key, "")
		DeadInstancesFilter.UnregisterInstance(&instance.Key)
	}
	err = metadata.ForgetClusterInstances(context.Background(), clusterName)
	return err
}

// ForgetLongUnseenInstances will remove entries of all instacnes that have long since been last seen.
func ForgetLongUnseenInstances() error {
	rows, err := metadata.ForgetLongUnseenInstances(
		context.Background(), config.Config.Topology.Discovery.UnseenForgetHours,
	)
	if err != nil {
		return log.Errore(err)
	}
	AuditOperation("forget-unseen", nil, fmt.Sprintf("Forgotten instances: %d", rows))
	return err
}

// SnapshotTopologies records topology graph for all existing topologies
func SnapshotTopologies() error {
	writeFunc := func() error {
		err := metadata.SnapshotTopologies(context.Background())
		if err != nil {
			return log.Errore(err)
		}

		return nil
	}
	return ExecDBWriteFunc(writeFunc)
}

// ReadHistoryClusterInstances reads (thin) instances from history
func ReadHistoryClusterInstances(clusterName string, historyTimestampPattern string) ([](*Instance), error) {
	instances := [](*Instance){}

	rows, err := metadata.ReadHistoryClusterInstanceRows(context.Background(), clusterName, historyTimestampPattern)
	for _, row := range rows {
		instance := NewInstance()

		instance.Key.Hostname = row.Hostname
		instance.Key.Port = row.Port
		instance.MasterKey.Hostname = row.MasterHost
		instance.MasterKey.Port = row.MasterPort
		instance.ClusterName = row.ClusterName

		instances = append(instances, instance)
	}
	if err != nil {
		return instances, log.Errore(err)
	}
	return instances, err
}

// RecordInstanceCoordinatesHistory snapshots the binlog coordinates of instances
func RecordInstanceCoordinatesHistory() error {
	{
		writeFunc := func() error {
			err := metadata.PurgeInstanceCoordinatesHistory(
				context.Background(), config.PseudoGTIDCoordinatesHistoryHeuristicMinutes+2,
			)
			return log.Errore(err)
		}
		ExecDBWriteFunc(writeFunc)
	}
	writeFunc := func() error {
		err := metadata.SnapshotInstanceCoordinatesHistory(context.Background())
		return log.Errore(err)
	}
	return ExecDBWriteFunc(writeFunc)
}

// GetHeuristiclyRecentCoordinatesForInstance returns valid and reasonably recent coordinates for given instance.
func GetHeuristiclyRecentCoordinatesForInstance(instanceKey *InstanceKey) (selfCoordinates *BinlogCoordinates, relayLogCoordinates *BinlogCoordinates, err error) {
	rows, err := metadata.ReadRecentInstanceCoordinates(
		context.Background(), instanceKey.Hostname, instanceKey.Port,
		config.PseudoGTIDCoordinatesHistoryHeuristicMinutes,
	)
	if err == nil && len(rows) > 0 {
		selfCoordinates = &BinlogCoordinates{LogFile: rows[0].BinaryLogFile, LogPos: rows[0].BinaryLogPos}
		relayLogCoordinates = &BinlogCoordinates{LogFile: rows[0].RelayLogFile, LogPos: rows[0].RelayLogPos}
	}
	return selfCoordinates, relayLogCoordinates, err
}

// RecordInstanceCoordinatesHistory snapshots the binlog coordinates of instances
func RecordStaleInstanceBinlogCoordinates(instanceKey *InstanceKey, binlogCoordinates *BinlogCoordinates) error {
	err := metadata.RecordStaleInstanceBinlogCoordinates(
		context.Background(), instanceKey.Hostname, instanceKey.Port,
		binlogCoordinates.LogFile, binlogCoordinates.LogPos,
	)
	return log.Errore(err)
}

func ExpireStaleInstanceBinlogCoordinates() error {
	expireSeconds := recoverypolicy.Current("").ReasonableReplicationLagSeconds * 2
	if expireSeconds < config.StaleInstanceCoordinatesExpireSeconds {
		expireSeconds = config.StaleInstanceCoordinatesExpireSeconds
	}
	writeFunc := func() error {
		err := metadata.ExpireStaleInstanceBinlogCoordinates(context.Background(), expireSeconds)
		return log.Errore(err)
	}
	return ExecDBWriteFunc(writeFunc)
}

// GetPreviousKnownRelayLogCoordinatesForInstance returns known relay log coordinates, that are not the
// exact current coordinates
func GetPreviousKnownRelayLogCoordinatesForInstance(instance *Instance) (relayLogCoordinates *BinlogCoordinates, err error) {
	rows, err := metadata.ReadPreviousRelayLogCoordinates(context.Background(),
		instance.Key.Hostname,
		instance.Key.Port,
		instance.RelaylogCoordinates.LogFile,
		instance.RelaylogCoordinates.LogPos,
	)
	if err == nil && len(rows) > 0 {
		relayLogCoordinates = &BinlogCoordinates{LogFile: rows[0].RelayLogFile, LogPos: rows[0].RelayLogPos}
	}
	return relayLogCoordinates, err
}

// ResetInstanceRelaylogCoordinatesHistory forgets about the history of an instance. This action is desirable
// when relay logs become obsolete or irrelevant. Such is the case on `CHANGE MASTER TO`: servers gets compeltely
// new relay logs.
func ResetInstanceRelaylogCoordinatesHistory(instanceKey *InstanceKey) error {
	writeFunc := func() error {
		err := metadata.ResetInstanceRelayLogCoordinatesHistory(
			context.Background(), instanceKey.Hostname, instanceKey.Port,
		)
		return log.Errore(err)
	}
	return ExecDBWriteFunc(writeFunc)
}

// FigureClusterName will make a best effort to deduce a cluster name using either a given alias
