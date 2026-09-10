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
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/golib/log"
	"github.com/openark/orchestrator/internal/golib/math"
	modeldomain "github.com/openark/orchestrator/internal/models/domain"
	"github.com/openark/orchestrator/internal/recoverypolicy"
)

const ReasonableDiscoveryLatency = 500 * time.Millisecond

// Instance represents a database instance, including its current configuration & status.
// It presents important replication configuration and detailed replication status.
type Instance struct {
	Key                          InstanceKey
	InstanceAlias                string
	Uptime                       uint
	ServerID                     uint
	ServerUUID                   string
	Version                      string
	VersionComment               string
	FlavorName                   string
	ReadOnly                     bool
	Binlog_format                string
	BinlogRowImage               string
	LogBinEnabled                bool
	LogSlaveUpdatesEnabled       bool // for API backwards compatibility. Equals `LogReplicationUpdatesEnabled`
	LogReplicationUpdatesEnabled bool
	SelfBinlogCoordinates        BinlogCoordinates
	MasterKey                    InstanceKey
	MasterUUID                   string
	AncestryUUID                 string
	IsDetachedMaster             bool

	Slave_SQL_Running          bool // for API backwards compatibility. Equals `ReplicationSQLThreadRuning`
	ReplicationSQLThreadRuning bool
	Slave_IO_Running           bool // for API backwards compatibility. Equals `ReplicationIOThreadRuning`
	ReplicationIOThreadRuning  bool
	ReplicationSQLThreadState  ReplicationThreadState
	ReplicationIOThreadState   ReplicationThreadState

	HasReplicationFilters bool
	GTIDMode              string
	SupportsOracleGTID    bool
	UsingOracleGTID       bool
	UsingMariaDBGTID      bool
	UsingPseudoGTID       bool
	ReadBinlogCoordinates BinlogCoordinates
	ExecBinlogCoordinates BinlogCoordinates
	IsDetached            bool
	RelaylogCoordinates   BinlogCoordinates
	LastSQLError          string
	LastIOError           string
	SecondsBehindMaster   modeldomain.NullInt64
	SQLDelay              uint
	ExecutedGtidSet       string
	GtidPurged            string
	GtidErrant            string

	masterExecutedGtidSet string // Not exported

	SlaveLagSeconds                   modeldomain.NullInt64 // for API backwards compatibility. Equals `ReplicationLagSeconds`
	ReplicationLagSeconds             modeldomain.NullInt64
	SlaveHosts                        InstanceKeyMap // for API backwards compatibility. Equals `Replicas`
	Replicas                          InstanceKeyMap
	ClusterName                       string
	SuggestedClusterAlias             string
	DataCenter                        string
	Region                            string
	PhysicalEnvironment               string
	ReplicationDepth                  uint
	IsCoMaster                        bool
	HasReplicationCredentials         bool
	ReplicationCredentialsAvailable   bool
	SemiSyncAvailable                 bool // when both semi sync plugins (master & replica) are loaded
	SemiSyncPriority                  uint // higher value means higher priority, zero means async replica
	SemiSyncMasterPluginNewVersion    bool // true for the plugin introduced with MySql 8.0.26
	SemiSyncReplicaPluginNewVersion   bool // true for the plugin introduced with MySql 8.0.26
	SemiSyncMasterEnabled             bool
	SemiSyncReplicaEnabled            bool
	SemiSyncMasterTimeout             uint64
	SemiSyncMasterWaitForReplicaCount uint
	SemiSyncMasterStatus              bool
	SemiSyncMasterClients             uint
	SemiSyncReplicaStatus             bool

	LastSeenTimestamp    string
	IsLastCheckValid     bool
	IsUpToDate           bool
	IsRecentlyChecked    bool
	SecondsSinceLastSeen modeldomain.NullInt64
	CountMySQLSnapshots  int

	// Careful. IsCandidate and PromotionRule are used together
	// and probably need to be merged. IsCandidate's value may
	// be picked up from daabase_candidate_instance's value when
	// reading an instance from the db.
	IsCandidate          bool
	PromotionRule        CandidatePromotionRule
	IsDowntimed          bool
	DowntimeReason       string
	DowntimeOwner        string
	DowntimeEndTimestamp string
	ElapsedDowntime      time.Duration
	UnresolvedHostname   string
	AllowTLS             bool
	// Replication TLS settings from SHOW SLAVE/REPLICA STATUS, reapplied on CHANGE REPLICATION SOURCE / CHANGE MASTER.
	ReplicationSSLCAFile           string
	ReplicationSSLCAPath           string
	ReplicationSSLCert             string
	ReplicationSSLCipher           string
	ReplicationSSLCRLFile          string
	ReplicationSSLCRLPath          string
	ReplicationSSLKey              string
	ReplicationSSLVerifyServerCert modeldomain.NullBool
	ReplicationTLSVersion          string
	ReplicationTLSCiphersuites     string
	ReplicationSourcePublicKeyPath string
	ReplicationGetSourcePublicKey  modeldomain.NullBool

	Problems []string

	LastDiscoveryLatency time.Duration

	seed bool // Means we force this instance to be written to backend, even if it's invalid, empty or forgotten

	/* All things Group Replication below */

	// Group replication global variables
	ReplicationGroupName            string
	ReplicationGroupIsSinglePrimary bool

	// Replication group members information. See
	// https://dev.mysql.com/doc/refman/8.0/en/replication-group-members-table.html for details.
	ReplicationGroupMemberState string
	ReplicationGroupMemberRole  string

	// List of all known members of the same group
	ReplicationGroupMembers InstanceKeyMap

	// Primary of the replication group
	ReplicationGroupPrimaryInstanceKey InstanceKey

	// Query string provider
	QSP QueryStringProvider
}

// NewInstance creates a new, empty instance

func NewInstance() *Instance {
	return &Instance{
		Replicas:                make(map[InstanceKey]bool),
		ReplicationGroupMembers: make(map[InstanceKey]bool),
		Problems:                []string{},
	}
}

func (instance *Instance) MarshalJSON() ([]byte, error) {
	i := struct {
		Instance
	}{
		Instance: *instance,
	}
	// change terminology. Users of the orchestrator API can switch to new terminology and avoid using old terminology
	// flip
	i.SlaveHosts = i.Replicas
	i.SlaveLagSeconds = instance.ReplicationLagSeconds
	i.LogSlaveUpdatesEnabled = instance.LogReplicationUpdatesEnabled
	i.Slave_SQL_Running = instance.ReplicationSQLThreadRuning
	i.Slave_IO_Running = instance.ReplicationIOThreadRuning

	return json.Marshal(i)
}

// Equals tests that this instance is the same instance as other. The function does not test
// configuration or status.
func (instance *Instance) Equals(other *Instance) bool {
	return instance.Key == other.Key
}

// MajorVersion returns this instance's major version number (e.g. for 5.5.36 it returns "5.5")
func (instance *Instance) MajorVersion() []string {
	return MajorVersion(instance.Version)
}

// MajorVersion returns this instance's major version number (e.g. for 5.5.36 it returns "5.5")
func (instance *Instance) MajorVersionString() string {
	return strings.Join(instance.MajorVersion(), ".")
}

func (instance *Instance) IsMySQL51() bool {
	return instance.MajorVersionString() == "5.1"
}

func (instance *Instance) IsMySQL55() bool {
	return instance.MajorVersionString() == "5.5"
}

func (instance *Instance) IsMySQL56() bool {
	return instance.MajorVersionString() == "5.6"
}

func (instance *Instance) IsMySQL57() bool {
	return instance.MajorVersionString() == "5.7"
}

func (instance *Instance) IsMySQL80() bool {
	return instance.MajorVersionString() == "8.0"
}

// IsSmallerBinlogFormat returns true when this instance's binlgo format is
// "smaller" than the other's, i.e. binary logs cannot flow from the other instance to this one
func (instance *Instance) IsSmallerBinlogFormat(other *Instance) bool {
	return IsSmallerBinlogFormat(instance.Binlog_format, other.Binlog_format)
}

// IsSmallerMajorVersion tests this instance against another and returns true if this instance is of a smaller "major" varsion.
// e.g. 5.5.36 is NOT a smaller major version as comapred to 5.5.36, but IS as compared to 5.6.9
func (instance *Instance) IsSmallerMajorVersion(other *Instance) bool {
	return IsSmallerMajorVersion(instance.Version, other.Version)
}

// IsSmallerMajorVersionByString checks if this instance has a smaller major version number than given one
func (instance *Instance) IsSmallerMajorVersionByString(otherVersion string) bool {
	return IsSmallerMajorVersion(instance.Version, otherVersion)
}

// IsMariaDB checks whether this is any version of MariaDB
func (instance *Instance) IsMariaDB() bool {
	return strings.Contains(instance.Version, "MariaDB")
}

// IsPercona checks whether this is any version of Percona Server
func (instance *Instance) IsPercona() bool {
	return strings.Contains(instance.VersionComment, "Percona")
}

// isMaxScale checks whether this is any version of MaxScale
func (instance *Instance) isMaxScale() bool {
	return strings.Contains(instance.Version, "maxscale")
}

// isNDB check whether this is NDB Cluster (aka MySQL Cluster)
func (instance *Instance) IsNDB() bool {
	return strings.Contains(instance.Version, "-ndb-")
}

// IsReplicationGroup checks whether the host thinks it is part of a known replication group. Notice that this might
// return True even if the group has decided to expel the member represented by this instance, as the instance might not
// know that under certain circumstances
func (instance *Instance) IsReplicationGroupMember() bool {
	return instance.ReplicationGroupName != ""
}

func (instance *Instance) IsReplicationGroupPrimary() bool {
	return instance.IsReplicationGroupMember() && instance.ReplicationGroupPrimaryInstanceKey.Equals(&instance.Key)
}

func (instance *Instance) IsReplicationGroupSecondary() bool {
	return instance.IsReplicationGroupMember() && !instance.ReplicationGroupPrimaryInstanceKey.Equals(&instance.Key)
}

// IsBinlogServer checks whether this is any type of a binlog server (currently only maxscale)
func (instance *Instance) IsBinlogServer() bool {
	return instance.isMaxScale()
}

// IsOracleMySQL checks whether this is an Oracle MySQL distribution
func (instance *Instance) IsOracleMySQL() bool {
	if instance.IsMariaDB() {
		return false
	}
	if instance.IsPercona() {
		return false
	}
	if instance.isMaxScale() {
		return false
	}
	if instance.IsBinlogServer() {
		return false
	}
	return true
}

func (instance *Instance) SetSeed() {
	instance.seed = true
}
func (instance *Instance) IsSeed() bool {
	return instance.seed
}

// applyFlavorName
func (instance *Instance) applyFlavorName() {
	if instance == nil {
		return
	}
	if instance.IsOracleMySQL() {
		instance.FlavorName = "MySQL"
	} else if instance.IsMariaDB() {
		instance.FlavorName = "MariaDB"
	} else if instance.IsPercona() {
		instance.FlavorName = "Percona"
	} else if instance.isMaxScale() {
		instance.FlavorName = "MaxScale"
	} else {
		instance.FlavorName = "unknown"
	}
}

// FlavorNameAndMajorVersion returns a string of the combined
// flavor and major version which is useful in some checks.
func (instance *Instance) FlavorNameAndMajorVersion() string {
	if instance.FlavorName == "" {
		instance.applyFlavorName()
	}

	return instance.FlavorName + "-" + instance.MajorVersionString()
}

// IsReplica makes simple heuristics to decide whether this instance is a replica of another instance
func (instance *Instance) IsReplica() bool {
	return instance.MasterKey.Hostname != "" && instance.MasterKey.Hostname != "_" && instance.MasterKey.Port != 0 && (instance.ReadBinlogCoordinates.LogFile != "" || instance.UsingGTID())
}

// IsMaster makes simple heuristics to decide whether this instance is a master (not replicating from any other server),
// either via traditional async/semisync replication or group replication
func (instance *Instance) IsMaster() bool {
	// If traditional replication is configured, it is for sure not a master
	if instance.IsReplica() {
		return false
	}
	// If traditional replication is not configured, and it is also not part of a replication group, this host is
	// a master
	if !instance.IsReplicationGroupMember() {
		return true
	}
	// If traditional replication is not configured, and this host is part of a group, it is only considered a
	// master if it has the role of group Primary. Otherwise it is not a master.
	if instance.ReplicationGroupMemberRole == GroupReplicationMemberRolePrimary {
		return true
	}
	return false
}

// ReplicaRunning returns true when this instance's status is of a replicating replica.
func (instance *Instance) ReplicaRunning() bool {
	return instance.IsReplica() && instance.ReplicationSQLThreadState.IsRunning() && instance.ReplicationIOThreadState.IsRunning()
}

// NoReplicationThreadRunning returns true when neither SQL nor IO threads are running (including the case where isn't even a replica)
func (instance *Instance) ReplicationThreadsStopped() bool {
	return instance.ReplicationSQLThreadState.IsStopped() && instance.ReplicationIOThreadState.IsStopped()
}

// NoReplicationThreadRunning returns true when neither SQL nor IO threads are running (including the case where isn't even a replica)
func (instance *Instance) ReplicationThreadsExist() bool {
	return instance.ReplicationSQLThreadState.Exists() && instance.ReplicationIOThreadState.Exists()
}

// SQLThreadUpToDate returns true when the instance had consumed all relay logs.
func (instance *Instance) SQLThreadUpToDate() bool {
	return instance.ReadBinlogCoordinates.Equals(&instance.ExecBinlogCoordinates)
}

// UsingGTID returns true when this replica is currently replicating via GTID (either Oracle or MariaDB)
func (instance *Instance) UsingGTID() bool {
	return instance.UsingOracleGTID || instance.UsingMariaDBGTID
}

// NextGTID returns the next (Oracle) GTID to be executed. Useful for skipping queries
func (instance *Instance) NextGTID() (string, error) {
	if instance.ExecutedGtidSet == "" {
		return "", fmt.Errorf("no value found in Executed_Gtid_Set; cannot compute NextGTID")
	}

	firstToken := func(s string, delimiter string) string {
		tokens := strings.Split(s, delimiter)
		return tokens[0]
	}
	lastToken := func(s string, delimiter string) string {
		tokens := strings.Split(s, delimiter)
		return tokens[len(tokens)-1]
	}
	// executed GTID set: 4f6d62ed-df65-11e3-b395-60672090eb04:1,b9b4712a-df64-11e3-b391-60672090eb04:1-6
	executedGTIDsFromMaster := lastToken(instance.ExecutedGtidSet, ",")
	// executedGTIDsFromMaster: b9b4712a-df64-11e3-b391-60672090eb04:1-6
	executedRange := lastToken(executedGTIDsFromMaster, ":")
	// executedRange: 1-6
	lastExecutedNumberToken := lastToken(executedRange, "-")
	// lastExecutedNumber: 6
	lastExecutedNumber, err := strconv.Atoi(lastExecutedNumberToken)
	if err != nil {
		return "", err
	}
	nextNumber := lastExecutedNumber + 1
	nextGTID := fmt.Sprintf("%s:%d", firstToken(executedGTIDsFromMaster, ":"), nextNumber)
	return nextGTID, nil
}

// AddReplicaKey adds a replica to the list of this instance's replicas.
func (instance *Instance) AddReplicaKey(replicaKey *InstanceKey) {
	instance.Replicas.AddKey(*replicaKey)
}

// AddGroupMemberKey adds a group member to the list of this instance's group members.
func (instance *Instance) AddGroupMemberKey(groupMemberKey *InstanceKey) {
	instance.ReplicationGroupMembers.AddKey(*groupMemberKey)
}

// GetNextBinaryLog returns the successive, if any, binary log file to the one given
func (instance *Instance) GetNextBinaryLog(binlogCoordinates BinlogCoordinates) (BinlogCoordinates, error) {
	if binlogCoordinates.LogFile == instance.SelfBinlogCoordinates.LogFile {
		return binlogCoordinates, fmt.Errorf("cannot find next binary log for %+v", binlogCoordinates)
	}
	return binlogCoordinates.NextFileCoordinates()
}

// IsReplicaOf returns true if this instance claims to replicate from given master
func (instance *Instance) IsReplicaOf(master *Instance) bool {
	return instance.MasterKey.Equals(&master.Key)
}

// IsReplicaOf returns true if this i supposed master of given replica
func (instance *Instance) IsMasterOf(replica *Instance) bool {
	return replica.IsReplicaOf(instance)
}

// IsDescendantOf returns true if this is replication directly or indirectly from other
func (instance *Instance) IsDescendantOf(other *Instance) bool {
	for uuid := range strings.SplitSeq(instance.AncestryUUID, ",") {
		if uuid == other.ServerUUID && uuid != "" {
			return true
		}
	}
	return false
}

// CanReplicateFrom uses heuristics to decide whether this instacne can practically replicate from other instance.
// Checks are made to binlog format, version number, binary logs etc.
func (instance *Instance) CanReplicateFrom(other *Instance) (bool, error) {
	var warningMsg error = nil

	if instance.Key.Equals(&other.Key) {
		return false, fmt.Errorf("instance cannot replicate from itself: %+v", instance.Key)
	}
	if !other.LogBinEnabled {
		return false, fmt.Errorf("instance does not have binary logs enabled: %+v", other.Key)
	}
	if other.IsReplica() {
		if !other.LogReplicationUpdatesEnabled {
			return false, fmt.Errorf("instance does not have log_slave_updates enabled: %+v", other.Key)
		}
		// OK for a master to not have log_slave_updates
		// Not OK for a replica, for it has to relay the logs.
	}
	if instance.IsSmallerMajorVersion(other) && !instance.IsBinlogServer() {
		warningMsg = fmt.Errorf("instance %+v has version %s, which is lower than %s on %+v ", instance.Key, instance.Version, other.Version, other.Key)
		if !config.Config.Topology.Compatibility.LowerReplicaVersionAllowed {
			return false, warningMsg
		}
	}
	if instance.LogBinEnabled && instance.LogReplicationUpdatesEnabled {
		if instance.IsSmallerBinlogFormat(other) {
			return false, fmt.Errorf("cannot replicate from %+v binlog format on %+v to %+v on %+v", other.Binlog_format, other.Key, instance.Binlog_format, instance.Key)
		}
	}
	policy := recoverypolicy.Current(instance.ClusterName)
	if policy.VerifyReplicationFilters {
		if other.HasReplicationFilters && !instance.HasReplicationFilters {
			return false, fmt.Errorf("%+v has replication filters", other.Key)
		}
	}
	if instance.ServerID == other.ServerID && !instance.IsBinlogServer() {
		return false, fmt.Errorf("identical server id: %+v, %+v both have %d", other.Key, instance.Key, instance.ServerID)
	}
	if instance.ServerUUID == other.ServerUUID && instance.ServerUUID != "" && !instance.IsBinlogServer() {
		return false, fmt.Errorf("identical server UUID: %+v, %+v both have %s", other.Key, instance.Key, instance.ServerUUID)
	}
	if instance.SQLDelay < other.SQLDelay && int64(other.SQLDelay) > int64(policy.ReasonableMaintenanceReplicationLagSeconds) {
		return false, fmt.Errorf("%+v has higher SQL_Delay (%+v seconds) than %+v does (%+v seconds)", other.Key, other.SQLDelay, instance.Key, instance.SQLDelay)
	}
	return true, warningMsg
}

const logPrefix = "Replicating from higher version source is enabled by LowerReplicaVersionAllowed config parameter. " +
	"Note that such a configuration is not officially supported by MySQL."

func (instance *Instance) CanReplicateFromEx(other *Instance, logContext string) (bool, error) {
	canReplicate, err := instance.CanReplicateFrom(other)

	if config.Config.Topology.Compatibility.LowerReplicaVersionAllowed && canReplicate && err != nil {
		log.Warningf("%v: %v Details: %v", logContext, logPrefix, err)
		err = nil
	}
	return canReplicate, err
}

// HasReasonableMaintenanceReplicationLag returns true when the replica lag is reasonable, and maintenance operations should have a green light to go.
func (instance *Instance) HasReasonableMaintenanceReplicationLag() bool {
	threshold := recoverypolicy.Current(instance.ClusterName).ReasonableMaintenanceReplicationLagSeconds
	// replicas with SQLDelay are a special case
	if instance.SQLDelay > 0 {
		return math.AbsInt64(instance.SecondsBehindMaster.Int64-int64(instance.SQLDelay)) <= int64(threshold)
	}
	return instance.SecondsBehindMaster.Int64 <= int64(threshold)
}

// CanMove returns true if this instance's state allows it to be repositioned. For example,
// if this instance lags too much, it will not be moveable.
func (instance *Instance) CanMove() (bool, error) {
	if !instance.IsLastCheckValid {
		return false, fmt.Errorf("%+v: last check invalid", instance.Key)
	}
	if !instance.IsRecentlyChecked {
		return false, fmt.Errorf("%+v: not recently checked", instance.Key)
	}
	if !instance.ReplicationSQLThreadState.IsRunning() {
		return false, fmt.Errorf("%+v: instance is not replicating", instance.Key)
	}
	if !instance.ReplicationIOThreadState.IsRunning() {
		return false, fmt.Errorf("%+v: instance is not replicating", instance.Key)
	}
	if !instance.SecondsBehindMaster.Valid {
		return false, fmt.Errorf("%+v: cannot determine replication lag", instance.Key)
	}
	if !instance.HasReasonableMaintenanceReplicationLag() {
		return false, fmt.Errorf("%+v: lags too much", instance.Key)
	}
	return true, nil
}

// CanMoveAsCoMaster returns true if this instance's state allows it to be repositioned.
func (instance *Instance) CanMoveAsCoMaster() (bool, error) {
	if !instance.IsLastCheckValid {
		return false, fmt.Errorf("%+v: last check invalid", instance.Key)
	}
	if !instance.IsRecentlyChecked {
		return false, fmt.Errorf("%+v: not recently checked", instance.Key)
	}
	return true, nil
}

// CanMoveViaMatch returns true if this instance's state allows it to be repositioned via pseudo-GTID matching
func (instance *Instance) CanMoveViaMatch() (bool, error) {
	if !instance.IsLastCheckValid {
		return false, fmt.Errorf("%+v: last check invalid", instance.Key)
	}
	if !instance.IsRecentlyChecked {
		return false, fmt.Errorf("%+v: not recently checked", instance.Key)
	}
	return true, nil
}

// StatusString returns a human readable description of this instance's status
func (instance *Instance) StatusString() string {
	if !instance.IsLastCheckValid {
		return "invalid"
	}
	if !instance.IsRecentlyChecked {
		return "unchecked"
	}
	if instance.IsReplica() && !instance.ReplicaRunning() {
		return "nonreplicating"
	}
	if instance.IsReplica() && !instance.HasReasonableMaintenanceReplicationLag() {
		return "lag"
	}
	return "ok"
}

// LagStatusString returns a human readable representation of current lag
func (instance *Instance) LagStatusString() string {
	if instance.IsDetached {
		return "detached"
	}
	if !instance.IsLastCheckValid {
		return "unknown"
	}
	if !instance.IsRecentlyChecked {
		return "unknown"
	}
	if instance.IsReplica() && !instance.ReplicaRunning() {
		return "null"
	}
	if instance.IsReplica() && !instance.SecondsBehindMaster.Valid {
		return "null"
	}
	if instance.IsReplica() && instance.ReplicationLagSeconds.Int64 > int64(recoverypolicy.Current(instance.ClusterName).ReasonableMaintenanceReplicationLagSeconds) {
		return fmt.Sprintf("%+vs", instance.ReplicationLagSeconds.Int64)
	}
	return fmt.Sprintf("%+vs", instance.ReplicationLagSeconds.Int64)
}

func (instance *Instance) descriptionTokens() (tokens []string) {
	tokens = append(tokens, instance.LagStatusString())
	tokens = append(tokens, instance.StatusString())
	tokens = append(tokens, instance.Version)
	if instance.ReadOnly {
		tokens = append(tokens, "ro")
	} else {
		tokens = append(tokens, "rw")
	}
	if instance.LogBinEnabled {
		tokens = append(tokens, instance.Binlog_format)
	} else {
		tokens = append(tokens, "nobinlog")
	}
	{
		extraTokens := []string{}
		if instance.LogBinEnabled && instance.LogReplicationUpdatesEnabled {
			extraTokens = append(extraTokens, ">>")
		}
		if instance.UsingGTID() || instance.SupportsOracleGTID {
			token := "GTID"
			if instance.GtidErrant != "" {
				token = fmt.Sprintf("%s:errant", token)
			}
			extraTokens = append(extraTokens, token)
		}
		if instance.UsingPseudoGTID {
			extraTokens = append(extraTokens, "P-GTID")
		}
		if instance.SemiSyncMasterStatus {
			extraTokens = append(extraTokens, "semi:master")
		}
		if instance.SemiSyncReplicaStatus {
			extraTokens = append(extraTokens, "semi:replica")
		}
		if instance.IsDowntimed {
			extraTokens = append(extraTokens, "downtimed")
		}
		tokens = append(tokens, strings.Join(extraTokens, ","))
	}
	return tokens
}

// HumanReadableDescription returns a simple readable string describing the status, version,
// etc. properties of this instance
func (instance *Instance) HumanReadableDescription() string {
	tokens := instance.descriptionTokens()
	nonEmptyTokens := []string{}
	for _, token := range tokens {
		if token != "" {
			nonEmptyTokens = append(nonEmptyTokens, token)
		}
	}
	description := fmt.Sprintf("[%s]", strings.Join(nonEmptyTokens, ","))
	return description
}

// TabulatedDescription returns a simple tabulated string of various properties
func (instance *Instance) TabulatedDescription(separator string) string {
	tokens := instance.descriptionTokens()
	description := strings.Join(tokens, separator)
	return description
}
