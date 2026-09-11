// Package inventory owns persisted instance inventory and lookup operations.
package inventory

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/openark/orchestrator/internal/attributes"
	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/golib/log"
	"github.com/openark/orchestrator/internal/golib/math"
	instaudit "github.com/openark/orchestrator/internal/inst/audit"
	instcluster "github.com/openark/orchestrator/internal/inst/cluster"
	instmodel "github.com/openark/orchestrator/internal/inst/instance"
	instpool "github.com/openark/orchestrator/internal/inst/pool"
	instresolve "github.com/openark/orchestrator/internal/inst/resolve"
	"github.com/openark/orchestrator/internal/kv"
	modeldomain "github.com/openark/orchestrator/internal/models/domain"
	"github.com/openark/orchestrator/internal/recoverypolicy"
	"github.com/openark/orchestrator/internal/repository/metadata"
	"github.com/patrickmn/go-cache"
)

// ReadClusterAliasOverride reads and applies SuggestedClusterAlias based on cluster_alias_override.
func ReadClusterAliasOverride(instance *instmodel.Instance) (err error) {
	rows, err := metadata.ReadClusterAliasOverride(context.Background(), instance.ClusterName)
	if err != nil {
		return err
	}
	aliasOverride := ""
	if len(rows) > 0 {
		aliasOverride = rows[0]
	}
	if aliasOverride != "" {
		instance.SuggestedClusterAlias = aliasOverride
	}
	return err
}

func ReadReplicationGroupPrimary(instance *instmodel.Instance) (err error) {
	rows, err := metadata.ReadReplicationGroupPrimary(context.Background(), instance.ReplicationGroupName)
	if err != nil {
		return err
	}
	for _, row := range rows {
		resolvedGroupPrimary, err := instresolve.NewInstanceKey(row.Hostname, row.Port)
		if err != nil {
			return err
		}
		instance.ReplicationGroupPrimaryInstanceKey = *resolvedGroupPrimary
	}
	return nil
}

// ReadInstanceClusterAttributes will return the cluster name for a given instance by looking at its master
// and getting it from there.
// It is a non-recursive function and so-called-recursion is performed upon periodic reading of
// instances.
func ReadInstanceClusterAttributes(instance *instmodel.Instance) (err error) {
	var masterOrGroupPrimaryInstanceKey instmodel.InstanceKey
	var masterOrGroupPrimaryClusterName string
	var masterOrGroupPrimarySuggestedClusterAlias string
	var masterOrGroupPrimaryReplicationDepth uint
	var ancestryUUID string
	var masterOrGroupPrimaryExecutedGtidSet string
	masterOrGroupPrimaryDataFound := false

	// Read the cluster_name of the _master_ or _group_primary_ of our instance, derive it from there.
	// For instances that are part of a replication group, if the host is not the group's primary, we use the
	// information from the group primary. If it is the group primary, we use the information of its master
	// (if it has any). If it is not a group member, we use the information from the host's master.
	if instance.IsReplicationGroupSecondary() {
		masterOrGroupPrimaryInstanceKey = instance.ReplicationGroupPrimaryInstanceKey
	} else {
		masterOrGroupPrimaryInstanceKey = instance.MasterKey
	}
	rows, err := metadata.ReadInstanceClusterAttributes(
		context.Background(), masterOrGroupPrimaryInstanceKey.Hostname, masterOrGroupPrimaryInstanceKey.Port,
	)
	if err != nil {
		return log.Errore(err)
	}
	for _, row := range rows {
		masterOrGroupPrimaryClusterName = row.ClusterName
		masterOrGroupPrimarySuggestedClusterAlias = row.SuggestedClusterAlias
		masterOrGroupPrimaryReplicationDepth = row.ReplicationDepth
		masterOrGroupPrimaryInstanceKey.Hostname = row.MasterHost
		masterOrGroupPrimaryInstanceKey.Port = row.MasterPort
		ancestryUUID = row.AncestryUUID
		masterOrGroupPrimaryExecutedGtidSet = row.ExecutedGTIDSet
		masterOrGroupPrimaryDataFound = true
	}

	var replicationDepth uint = 0
	var clusterName string
	if masterOrGroupPrimaryDataFound {
		replicationDepth = masterOrGroupPrimaryReplicationDepth + 1
		clusterName = masterOrGroupPrimaryClusterName
	}
	clusterNameByInstanceKey := instance.Key.StringCode()
	if clusterName == "" {
		// Nothing from master; we set it to be named after the instance itself
		clusterName = clusterNameByInstanceKey
	}

	isCoMaster := false
	if masterOrGroupPrimaryInstanceKey.Equals(&instance.Key) {
		// co-master calls for special case, in fear of the infinite loop
		isCoMaster = true
		clusterNameByCoMasterKey := instance.MasterKey.StringCode()
		if clusterName != clusterNameByInstanceKey && clusterName != clusterNameByCoMasterKey {
			// Can be caused by a co-master topology failover
			log.Errorf("ReadInstanceClusterAttributes: in co-master topology %s is not in (%s, %s). Forcing it to become one of them", clusterName, clusterNameByInstanceKey, clusterNameByCoMasterKey)
			clusterName = math.TernaryString(instance.Key.SmallerThan(&instance.MasterKey), clusterNameByInstanceKey, clusterNameByCoMasterKey)
		}
		if clusterName == clusterNameByInstanceKey {
			// circular replication. Avoid infinite ++ on replicationDepth
			replicationDepth = 0
			ancestryUUID = ""
		} // While the other stays "1"
	}
	instance.ClusterName = clusterName
	instance.SuggestedClusterAlias = masterOrGroupPrimarySuggestedClusterAlias
	instance.ReplicationDepth = replicationDepth
	instance.IsCoMaster = isCoMaster
	instance.AncestryUUID = ancestryUUID
	instance.SetMasterExecutedGtidSet(masterOrGroupPrimaryExecutedGtidSet)
	return nil
}

type byNamePort []*instmodel.InstanceKey

func (entries byNamePort) Len() int      { return len(entries) }
func (entries byNamePort) Swap(i, j int) { entries[i], entries[j] = entries[j], entries[i] }
func (entries byNamePort) Less(i, j int) bool {
	return (entries[i].Hostname < entries[j].Hostname) ||
		(entries[i].Hostname == entries[j].Hostname && entries[i].Port < entries[j].Port)
}

// BulkReadInstance returns a list of all instances from the database.
func BulkReadInstance() ([]*instmodel.InstanceKey, error) {
	var instanceKeys []*instmodel.InstanceKey

	instances, err := readInstances(metadata.ReadAllInstanceRows)
	if err != nil {
		return nil, fmt.Errorf("BulkReadInstance: %+v", err)
	}

	// update counters if we picked anything up
	if len(instances) > 0 {
		readInstanceCounter.Add(context.Background(), int64(len(instances)))

		for _, instance := range instances {
			instanceKeys = append(instanceKeys, &instance.Key)
		}
		// sort on orchestrator and not the backend (should be redundant)
		sort.Sort(byNamePort(instanceKeys))
	}

	return instanceKeys, nil
}

func ReadInstancePromotionRule(instance *instmodel.Instance) (err error) {
	var promotionRule instmodel.CandidatePromotionRule = instmodel.NeutralPromoteRule
	rows, err := metadata.ReadInstancePromotionRule(context.Background(), instance.Key.Hostname, instance.Key.Port)
	if err == nil && len(rows) > 0 {
		promotionRule = instmodel.CandidatePromotionRule(rows[0])
	}
	instance.PromotionRule = promotionRule
	return log.Errore(err)
}

// readInstanceRow reads a single instance row from the orchestrator backend database.
func readInstanceRow(row modeldomain.BackendInstanceRecord) *instmodel.Instance {
	instance := instmodel.NewInstance()

	instance.Key.Hostname = row.Hostname
	instance.Key.Port = row.Port
	instance.Uptime = row.Uptime
	instance.ServerID = row.ServerID
	instance.ServerUUID = row.ServerUUID
	instance.Version = row.Version
	instance.VersionComment = row.VersionComment
	instance.ReadOnly = row.ReadOnly
	instance.Binlog_format = row.BinlogFormat
	instance.BinlogRowImage = row.BinlogRowImage
	instance.LogBinEnabled = row.LogBin
	instance.LogReplicationUpdatesEnabled = row.LogSlaveUpdates
	instance.MasterKey.Hostname = row.MasterHost
	instance.MasterKey.Port = row.MasterPort
	instance.IsDetachedMaster = instance.MasterKey.IsDetached()
	instance.ReplicationSQLThreadRuning = row.SlaveSQLRunning
	instance.ReplicationIOThreadRuning = row.SlaveIORunning
	instance.ReplicationSQLThreadState = instmodel.ReplicationThreadState(row.ReplicationSQLThreadState)
	instance.ReplicationIOThreadState = instmodel.ReplicationThreadState(row.ReplicationIOThreadState)
	instance.HasReplicationFilters = row.HasReplicationFilters
	instance.SupportsOracleGTID = row.SupportsOracleGTID
	instance.UsingOracleGTID = row.OracleGTID
	instance.MasterUUID = row.MasterUUID
	instance.AncestryUUID = row.AncestryUUID
	instance.ExecutedGtidSet = row.ExecutedGTIDSet
	instance.GTIDMode = row.GTIDMode
	instance.GtidPurged = row.GTIDPurged
	instance.GtidErrant = row.GTIDErrant
	instance.UsingMariaDBGTID = row.MariaDBGTID
	instance.UsingPseudoGTID = row.PseudoGTID
	instance.SelfBinlogCoordinates.LogFile = row.BinaryLogFile
	instance.SelfBinlogCoordinates.LogPos = row.BinaryLogPos
	instance.ReadBinlogCoordinates.LogFile = row.MasterLogFile
	instance.ReadBinlogCoordinates.LogPos = row.ReadMasterLogPos
	instance.ExecBinlogCoordinates.LogFile = row.RelayMasterLogFile
	instance.ExecBinlogCoordinates.LogPos = row.ExecMasterLogPos
	instance.IsDetached, _ = instance.ExecBinlogCoordinates.ExtractDetachedCoordinates()
	instance.RelaylogCoordinates.LogFile = row.RelayLogFile
	instance.RelaylogCoordinates.LogPos = row.RelayLogPos
	instance.RelaylogCoordinates.Type = instmodel.RelayLog
	instance.LastSQLError = row.LastSQLError
	instance.LastIOError = row.LastIOError
	instance.SecondsBehindMaster = modeldomain.NullInt64(row.SecondsBehindMaster)
	instance.ReplicationLagSeconds = modeldomain.NullInt64(row.SlaveLagSeconds)
	instance.SQLDelay = row.SQLDelay
	replicasJSON := row.SlaveHosts
	instance.ClusterName = row.ClusterName
	instance.SuggestedClusterAlias = row.SuggestedClusterAlias
	instance.DataCenter = row.DataCenter
	instance.Region = row.Region
	instance.PhysicalEnvironment = row.PhysicalEnvironment
	instance.SemiSyncPriority = row.SemiSyncEnforced
	instance.SemiSyncAvailable = row.SemiSyncAvailable
	instance.SemiSyncMasterEnabled = row.SemiSyncMasterEnabled
	instance.SemiSyncMasterTimeout = row.SemiSyncMasterTimeout
	instance.SemiSyncMasterWaitForReplicaCount = row.SemiSyncMasterWaitForReplicaCount
	instance.SemiSyncReplicaEnabled = row.SemiSyncReplicaEnabled
	instance.SemiSyncMasterStatus = row.SemiSyncMasterStatus
	instance.SemiSyncMasterClients = row.SemiSyncMasterClients
	instance.SemiSyncReplicaStatus = row.SemiSyncReplicaStatus
	instance.ReplicationDepth = row.ReplicationDepth
	instance.IsCoMaster = row.CoMaster
	instance.ReplicationCredentialsAvailable = row.ReplicationCredentialsAvailable
	instance.HasReplicationCredentials = row.HasReplicationCredentials
	secondsSinceLastChecked := modeldomain.NonNegativeUint(row.SecondsSinceLastChecked)
	instance.IsUpToDate = secondsSinceLastChecked <= config.Config.Topology.Discovery.PollSeconds
	instance.IsRecentlyChecked = secondsSinceLastChecked <= config.Config.Topology.Discovery.PollSeconds*5
	instance.LastSeenTimestamp = row.LastSeen.String
	instance.IsLastCheckValid = row.LastCheckValid
	instance.SecondsSinceLastSeen = modeldomain.NullInt64(row.SecondsSinceLastSeen)
	instance.IsCandidate = row.Candidate
	instance.PromotionRule = instmodel.CandidatePromotionRule(row.PromotionRule)
	instance.IsDowntimed = row.Downtimed
	instance.DowntimeReason = row.DowntimeReason
	instance.DowntimeOwner = row.DowntimeOwner
	instance.DowntimeEndTimestamp = row.DowntimeEndTimestamp
	instance.ElapsedDowntime = time.Second * time.Duration(row.ElapsedDowntimeSeconds)
	instance.UnresolvedHostname = row.UnresolvedHostname
	instance.AllowTLS = row.AllowTLS
	instance.InstanceAlias = row.InstanceAlias
	instance.LastDiscoveryLatency = time.Duration(row.LastDiscoveryLatency) * time.Nanosecond

	instance.Replicas.ReadJson(replicasJSON)
	instance.ApplyFlavorName()

	/* Read Group Replication variables below */
	instance.ReplicationGroupName = row.ReplicationGroupName
	instance.ReplicationGroupIsSinglePrimary = row.ReplicationGroupSinglePrimary
	instance.ReplicationGroupMemberState = row.ReplicationGroupMemberState
	instance.ReplicationGroupMemberRole = row.ReplicationGroupMemberRole
	instance.ReplicationGroupPrimaryInstanceKey = instmodel.InstanceKey{Hostname: row.ReplicationGroupPrimaryHost,
		Port: row.ReplicationGroupPrimaryPort}
	instance.ReplicationGroupMembers.ReadJson(row.ReplicationGroupMembers)
	//instance.ReplicationGroup = m.GetString("replication_group_")

	// problems
	if !instance.IsLastCheckValid {
		instance.Problems = append(instance.Problems, "last_check_invalid")
	} else if !instance.IsRecentlyChecked {
		instance.Problems = append(instance.Problems, "not_recently_checked")
	} else if instance.ReplicationThreadsExist() && !instance.ReplicaRunning() {
		instance.Problems = append(instance.Problems, "not_replicating")
	} else if instance.ReplicationLagSeconds.Valid && math.AbsInt64(instance.ReplicationLagSeconds.Int64-int64(instance.SQLDelay)) > int64(recoverypolicy.Current(instance.ClusterName).ReasonableReplicationLagSeconds) {
		instance.Problems = append(instance.Problems, "replication_lag")
	}
	if instance.GtidErrant != "" {
		instance.Problems = append(instance.Problems, "errant_gtid")
	}
	// Group replication problems
	if instance.ReplicationGroupName != "" && instance.ReplicationGroupMemberState != instmodel.GroupReplicationMemberStateOnline {
		instance.Problems = append(instance.Problems, "group_replication_member_not_online")
	}

	return instance
}

type instanceRowsReader func(context.Context) ([]modeldomain.BackendInstanceRecord, error)

func readInstances(readRows instanceRowsReader) ([]*instmodel.Instance, error) {
	return readInstancesContext(context.Background(), readRows)
}

func readInstancesContext(ctx context.Context, readRows instanceRowsReader) ([]*instmodel.Instance, error) {
	readFunc := func() ([]*instmodel.Instance, error) {
		instances := []*instmodel.Instance{}

		rows, err := readRows(ctx)
		for _, row := range rows {
			instance := readInstanceRow(row)
			instances = append(instances, instance)
		}
		if err != nil {
			return instances, log.Errore(err)
		}
		err = PopulateInstancesAgents(instances)
		if err != nil {
			return instances, log.Errore(err)
		}
		return instances, err
	}
	select {
	case instanceReadChan <- true:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	instances, err := readFunc()
	<-instanceReadChan
	return instances, err
}

func readInstancesByExactKeyContext(ctx context.Context, instanceKey *instmodel.InstanceKey) ([]*instmodel.Instance, error) {
	return readInstancesContext(ctx, func(ctx context.Context) ([]modeldomain.BackendInstanceRecord, error) {
		return metadata.ReadInstanceRowsByKey(ctx, instanceKey.Hostname, instanceKey.Port)
	})
}

// ReadInstance reads an instance from the orchestrator backend database
func ReadInstance(instanceKey *instmodel.InstanceKey) (*instmodel.Instance, bool, error) {
	return ReadInstanceContext(context.Background(), instanceKey)
}
func ReadInstanceContext(ctx context.Context, instanceKey *instmodel.InstanceKey) (*instmodel.Instance, bool, error) {
	instances, err := readInstancesByExactKeyContext(ctx, instanceKey)
	// We know there will be at most one (hostname & port are PK)
	// And we expect to find one
	readInstanceCounter.Add(context.Background(), 1)
	if len(instances) == 0 {
		return nil, false, err
	}
	if err != nil {
		return instances[0], false, err
	}
	return instances[0], true, nil
}

// ReadClusterInstances reads all instances of a given cluster
func ReadClusterInstances(clusterName string) ([]*instmodel.Instance, error) {
	if strings.Contains(clusterName, "'") {
		return []*instmodel.Instance{}, log.Errorf("Invalid cluster name: %s", clusterName)
	}
	return readInstances(func(ctx context.Context) ([]modeldomain.BackendInstanceRecord, error) {
		return metadata.ReadClusterInstanceRows(ctx, clusterName)
	})
}

// ReadClusterWriteableMaster returns the/a writeable master of this cluster
// Typically, the cluster name indicates the master of the cluster. However, in circular
// master-master replication one master can assume the name of the cluster, and it is
// not guaranteed that it is the writeable one.
func ReadClusterWriteableMaster(clusterName string) ([]*instmodel.Instance, error) {
	return readInstances(func(ctx context.Context) ([]modeldomain.BackendInstanceRecord, error) {
		return metadata.ReadWritableClusterMasterRows(ctx, clusterName)
	})
}

// ReadClusterMaster returns the master of this cluster.
// - if the cluster has co-masters, the/a writable one is returned
// - if the cluster has a single master, that master is retuened whether it is read-only or writable.
func ReadClusterMaster(clusterName string) ([]*instmodel.Instance, error) {
	return readInstances(func(ctx context.Context) ([]modeldomain.BackendInstanceRecord, error) {
		return metadata.ReadClusterMasterRows(ctx, clusterName)
	})
}

// ReadWriteableClustersMasters returns writeable masters of all clusters, but only one
// per cluster, in similar logic to ReadClusterWriteableMaster
func ReadWriteableClustersMasters() (instances []*instmodel.Instance, err error) {
	allMasters, err := readInstances(metadata.ReadWritableClusterMastersRows)
	if err != nil {
		return instances, err
	}
	visitedClusters := make(map[string]bool)
	for _, instance := range allMasters {
		if !visitedClusters[instance.ClusterName] {
			visitedClusters[instance.ClusterName] = true
			instances = append(instances, instance)
		}
	}
	return instances, err
}

// ReadReplicaInstances reads replicas of a given master
func ReadReplicaInstances(masterKey *instmodel.InstanceKey) ([]*instmodel.Instance, error) {
	return readInstances(func(ctx context.Context) ([]modeldomain.BackendInstanceRecord, error) {
		return metadata.ReadReplicaInstanceRows(ctx, masterKey.Hostname, masterKey.Port)
	})
}

// ReadReplicaInstancesIncludingBinlogServerSubReplicas returns a list of direct slves including any replicas
// of a binlog server replica
func ReadReplicaInstancesIncludingBinlogServerSubReplicas(masterKey *instmodel.InstanceKey) ([]*instmodel.Instance, error) {
	replicas, err := ReadReplicaInstances(masterKey)
	if err != nil {
		return replicas, err
	}
	for _, replica := range replicas {
		if replica.IsBinlogServer() {
			binlogServerReplicas, err := ReadReplicaInstancesIncludingBinlogServerSubReplicas(&replica.Key)
			if err != nil {
				return replicas, err
			}
			replicas = append(replicas, binlogServerReplicas...)
		}
	}
	return replicas, err
}

// ReadBinlogServerReplicaInstances reads direct replicas of a given master that are binlog servers
func ReadBinlogServerReplicaInstances(masterKey *instmodel.InstanceKey) ([]*instmodel.Instance, error) {
	return readInstances(func(ctx context.Context) ([]modeldomain.BackendInstanceRecord, error) {
		return metadata.ReadBinlogServerReplicaRows(ctx, masterKey.Hostname, masterKey.Port)
	})
}

// ReadUnseenInstances reads all instances which were not recently seen
func ReadUnseenInstances() ([]*instmodel.Instance, error) {
	return readInstances(metadata.ReadUnseenInstanceRows)
}

// ReadProblemInstances reads all instances with problems
func ReadProblemInstances(clusterName string) ([]*instmodel.Instance, error) {
	policy := recoverypolicy.Current(clusterName)
	instances, err := readInstances(func(ctx context.Context) ([]modeldomain.BackendInstanceRecord, error) {
		return metadata.ReadProblemInstanceRows(
			ctx,
			clusterName,
			config.Config.Topology.Discovery.PollSeconds*5,
			policy.ReasonableReplicationLagSeconds,
		)
	})
	if err != nil {
		return instances, err
	}
	var reportedInstances []*instmodel.Instance
	for _, instance := range instances {
		skip := false
		if instance.IsDowntimed {
			skip = true
		}
		if instmodel.FiltersMatchInstanceKey(&instance.Key, policy.ProblemIgnoreHostnameFilters) {
			skip = true
		}
		if !skip {
			reportedInstances = append(reportedInstances, instance)
		}
	}
	return reportedInstances, nil
}

// SearchInstances reads all instances qualifying for some searchString
func SearchInstances(searchString string) ([]*instmodel.Instance, error) {
	searchString = strings.TrimSpace(searchString)
	return readInstances(func(ctx context.Context) ([]modeldomain.BackendInstanceRecord, error) {
		return metadata.SearchInstanceRows(ctx, searchString)
	})
}

// FindInstances reads all instances whose name matches given pattern
func FindInstances(regexpPattern string) (result []*instmodel.Instance, err error) {
	result = []*instmodel.Instance{}
	r, err := regexp.Compile(regexpPattern)
	if err != nil {
		return result, err
	}
	unfiltered, err := readInstances(metadata.ReadAllInstanceRowsByTopology)
	if err != nil {
		return unfiltered, err
	}
	for _, instance := range unfiltered {
		if r.MatchString(instance.Key.DisplayString()) {
			result = append(result, instance)
		}
	}
	return result, nil
}

// findFuzzyInstances return instances whose names are like the one given (host & port substrings)
// For example, the given `mydb-3:3306` might find `myhosts-mydb301-production.mycompany.com:3306`
func findFuzzyInstances(fuzzyInstanceKey *instmodel.InstanceKey) ([]*instmodel.Instance, error) {
	return readInstances(func(ctx context.Context) ([]modeldomain.BackendInstanceRecord, error) {
		return metadata.ReadFuzzyInstanceRows(ctx, fuzzyInstanceKey.Hostname, fuzzyInstanceKey.Port)
	})
}

// ReadFuzzyInstanceKey accepts a fuzzy instance key and expects to return a single, fully qualified,
// known instance key.
func ReadFuzzyInstanceKey(fuzzyInstanceKey *instmodel.InstanceKey) *instmodel.InstanceKey {
	if fuzzyInstanceKey == nil {
		return nil
	}
	if fuzzyInstanceKey.IsIPv4() {
		// avoid fuzziness. When looking for 10.0.0.1 we don't want to match 10.0.0.15!
		return nil
	}
	if fuzzyInstanceKey.Hostname != "" {
		// Fuzzy instance search
		if fuzzyInstances, _ := findFuzzyInstances(fuzzyInstanceKey); len(fuzzyInstances) == 1 {
			return &(fuzzyInstances[0].Key)
		}
	}
	return nil
}

// ReadFuzzyInstanceKeyIfPossible accepts a fuzzy instance key and hopes to return a single, fully qualified,
// known instance key, or else the original given key
func ReadFuzzyInstanceKeyIfPossible(fuzzyInstanceKey *instmodel.InstanceKey) *instmodel.InstanceKey {
	if instanceKey := ReadFuzzyInstanceKey(fuzzyInstanceKey); instanceKey != nil {
		return instanceKey
	}
	return fuzzyInstanceKey
}

// ReadFuzzyInstance accepts a fuzzy instance key and expects to return a single instance.
// Multiple instances matching the fuzzy keys are not allowed.
func ReadFuzzyInstance(fuzzyInstanceKey *instmodel.InstanceKey) (*instmodel.Instance, error) {
	if fuzzyInstanceKey == nil {
		return nil, log.Errorf("ReadFuzzyInstance received nil input")
	}
	if fuzzyInstanceKey.IsIPv4() {
		// avoid fuzziness. When looking for 10.0.0.1 we don't want to match 10.0.0.15!
		instance, _, err := ReadInstance(fuzzyInstanceKey)
		return instance, err
	}
	if fuzzyInstanceKey.Hostname != "" {
		// Fuzzy instance search
		if fuzzyInstances, _ := findFuzzyInstances(fuzzyInstanceKey); len(fuzzyInstances) == 1 {
			return fuzzyInstances[0], nil
		}
	}
	return nil, log.Errorf("Cannot determine fuzzy instance %+v", *fuzzyInstanceKey)
}

// ReadLostInRecoveryInstances returns all instances (potentially filtered by cluster)
// which are currently indicated as downtimed due to being lost during a topology recovery.
func ReadLostInRecoveryInstances(clusterName string) ([]*instmodel.Instance, error) {
	return readInstances(func(ctx context.Context) ([]modeldomain.BackendInstanceRecord, error) {
		return metadata.ReadLostInRecoveryInstanceRows(ctx, instmodel.DowntimeLostInRecoveryMessage, clusterName)
	})
}

// ReadDowntimedInstances returns all instances currently downtimed, potentially filtered by cluster
func ReadDowntimedInstances(clusterName string) ([]*instmodel.Instance, error) {
	return readInstances(func(ctx context.Context) ([]modeldomain.BackendInstanceRecord, error) {
		return metadata.ReadDowntimedInstanceRows(ctx, clusterName)
	})
}

// ReadClusterCandidateInstances reads cluster instances which are also marked as candidates
func ReadClusterCandidateInstances(clusterName string) ([]*instmodel.Instance, error) {
	return readInstances(func(ctx context.Context) ([]modeldomain.BackendInstanceRecord, error) {
		return metadata.ReadClusterCandidateInstanceRows(ctx, clusterName)
	})
}

// ReadClusterNeutralPromotionRuleInstances reads cluster instances whose promotion-rule is marked as 'neutral'
func ReadClusterNeutralPromotionRuleInstances(clusterName string) (neutralInstances []*instmodel.Instance, err error) {
	instances, err := ReadClusterInstances(clusterName)
	if err != nil {
		return neutralInstances, err
	}
	for _, instance := range instances {
		if instance.PromotionRule == instmodel.NeutralPromoteRule {
			neutralInstances = append(neutralInstances, instance)
		}
	}
	return neutralInstances, nil
}

// filterOSCInstances will filter the given list such that only replicas fit for OSC control remain.
func filterOSCInstances(instances []*instmodel.Instance) []*instmodel.Instance {
	result := []*instmodel.Instance{}
	for _, instance := range instances {
		if instmodel.FiltersMatchInstanceKey(&instance.Key, config.Config.OSC.IgnoreHostnames) {
			continue
		}
		if instance.IsBinlogServer() {
			continue
		}
		if !instance.IsLastCheckValid {
			continue
		}
		result = append(result, instance)
	}
	return result
}

// Get two busiest instances per DC
func getTwoBusiestPerDC(all []*instmodel.Instance) []*instmodel.Instance {
	result := []*instmodel.Instance{}

	// sort by DC and replicas count
	sort.Sort(sort.Reverse(InstancesByDc(all)))

	currentDCInstances := 0
	var currentDC *string = nil

	for _, im := range all {
		if currentDC == nil || *currentDC != im.DataCenter {
			currentDCInstances = 0
			currentDC = &im.DataCenter
		}
		if currentDCInstances > 1 {
			continue
		}
		currentDCInstances++
		result = append(result, im)
	}
	return result
}

// GetClusterOSCReplicas returns a heuristic list of replicas which are fit as control replicas for an OSC operation.
// These would be intermediate masters
func GetClusterOSCReplicas(clusterName string) ([]*instmodel.Instance, error) {
	if strings.Contains(clusterName, "'") {
		return []*instmodel.Instance{}, log.Errorf("Invalid cluster name: %s", clusterName)
	}

	result := []*instmodel.Instance{}
	// Stage 1: 1st tier servers.
	// We get up to two 1st tier servers from each DC in the following order:
	// 1. Most busiest IMs
	// 2. Most lagging leaf nodes
	// Examples:
	// 1. If there are N > 1 IMs in the DC, we will use 2 busiest ones
	// (having the highest number of replicas)
	// 2. If there is only 1 IM in the DC, but there are some leaf nodes,
	// we will use IM + most lagging leaf node
	// 3. If there are no IMs in the DC, but there are leaf nodes, we will use
	// up to two most lagging leaf nodes
	//
	// So this stage will collect at most 2 servers per DC
	{
		firstTierServers, err := readInstances(func(ctx context.Context) ([]modeldomain.BackendInstanceRecord, error) {
			return metadata.ReadClusterInstancesAtDepthRows(ctx, clusterName, 1)
		})
		if err != nil {
			return result, err
		}

		firstTierServers = filterOSCInstances(firstTierServers)
		result = append(result, getTwoBusiestPerDC(firstTierServers)...)
	}

	// Stage 2: 2nd tier servers
	// Examine all selected 1st tier servers, and if they are IMs, get at most
	// two of their busiest replicas (2nd tier servers).
	// So this stage will collect at most 2 replicas per IM. If we collected 2 IMs
	// per DC in the 1st stage, here we will get 4 servers per DC
	{
		// Get at most 2 replicas of found IMs
		for _, im := range result {
			if len(im.Replicas) == 0 {
				// this is 1st tier leaf
				continue
			}
			replicas, err := ReadReplicaInstances(&im.Key)
			if err != nil {
				return result, err
			}
			sort.Sort(sort.Reverse(InstancesByCountReplicas(replicas)))
			replicas = filterOSCInstances(replicas)
			replicas = replicas[0:min(2, len(replicas))]
			result = append(result, replicas...)
		}
	}

	// Stage 3: 3rd tier servers
	// Get 2 busiest 3rd tier replicas per DC
	{
		replicas, err := readInstances(func(ctx context.Context) ([]modeldomain.BackendInstanceRecord, error) {
			return metadata.ReadClusterInstancesAtDepthRows(ctx, clusterName, 3)
		})
		if err != nil {
			return result, err
		}
		replicas = filterOSCInstances(replicas)
		result = append(result, getTwoBusiestPerDC(replicas)...)
	}

	return result, nil
}

// GetClusterGhostReplicas returns a list of replicas that can serve as the connected servers
// for a [gh-ost](https://github.com/github/gh-ost) operation. A gh-ost operation prefers to talk
// to a RBR replica that has no children.
func GetClusterGhostReplicas(clusterName string) (result []*instmodel.Instance, err error) {
	instances, err := readInstances(func(ctx context.Context) ([]modeldomain.BackendInstanceRecord, error) {
		return metadata.ReadClusterGhostInstanceRows(ctx, clusterName)
	})
	if err != nil {
		return result, err
	}

	for _, instance := range instances {
		skipThisHost := false
		if instance.IsBinlogServer() {
			skipThisHost = true
		}
		if !instance.IsLastCheckValid {
			skipThisHost = true
		}
		if !instance.LogBinEnabled {
			skipThisHost = true
		}
		if !instance.LogReplicationUpdatesEnabled {
			skipThisHost = true
		}
		if !skipThisHost {
			result = append(result, instance)
		}
	}

	return result, err
}

// GetInstancesMaxLag returns the maximum lag in a set of instances
func GetInstancesMaxLag(instances []*instmodel.Instance) (maxLag int64, err error) {
	if len(instances) == 0 {
		return 0, log.Errorf("No instances found in GetInstancesMaxLag")
	}
	for _, clusterInstance := range instances {
		if clusterInstance.ReplicationLagSeconds.Valid && clusterInstance.ReplicationLagSeconds.Int64 > maxLag {
			maxLag = clusterInstance.ReplicationLagSeconds.Int64
		}
	}
	return maxLag, nil
}

// GetClusterHeuristicLag returns a heuristic lag for a cluster, based on its OSC replicas
func GetClusterHeuristicLag(clusterName string) (int64, error) {
	instances, err := GetClusterOSCReplicas(clusterName)
	if err != nil {
		return 0, err
	}
	return GetInstancesMaxLag(instances)
}

// GetHeuristicClusterPoolInstances returns instances of a cluster which are also pooled. If `pool` argument
// is empty, all pools are considered, otherwise, only instances of given pool are considered.
func GetHeuristicClusterPoolInstances(clusterName string, pool string) (result []*instmodel.Instance, err error) {
	result = []*instmodel.Instance{}
	instances, err := ReadClusterInstances(clusterName)
	if err != nil {
		return result, err
	}

	pooledInstanceKeys := instmodel.NewInstanceKeyMap()
	clusterPoolInstances, err := instpool.ReadClusterPoolInstances(clusterName, pool)
	if err != nil {
		return result, err
	}
	for _, clusterPoolInstance := range clusterPoolInstances {
		pooledInstanceKeys.AddKey(instmodel.InstanceKey{Hostname: clusterPoolInstance.Hostname, Port: clusterPoolInstance.Port})
	}

	for _, instance := range instances {
		skipThisHost := false
		if instance.IsBinlogServer() {
			skipThisHost = true
		}
		if !instance.IsLastCheckValid {
			skipThisHost = true
		}
		if !pooledInstanceKeys.HasKey(instance.Key) {
			skipThisHost = true
		}
		if !skipThisHost {
			result = append(result, instance)
		}
	}

	return result, err
}

// GetHeuristicClusterPoolInstancesLag returns a heuristic lag for the instances participating
// in a cluster pool (or all the cluster's pools)
func GetHeuristicClusterPoolInstancesLag(clusterName string, pool string) (int64, error) {
	instances, err := GetHeuristicClusterPoolInstances(clusterName, pool)
	if err != nil {
		return 0, err
	}
	return GetInstancesMaxLag(instances)
}

// updateInstanceClusterName
func updateInstanceClusterName(instance *instmodel.Instance) error {
	writeFunc := func() error {
		err := metadata.UpdateInstanceClusterName(
			context.Background(), instance.Key.Hostname, instance.Key.Port, instance.ClusterName,
		)
		if err != nil {
			return log.Errore(err)
		}
		instaudit.AuditOperation("update-cluster-name", &instance.Key, fmt.Sprintf("set to %s", instance.ClusterName))
		return nil
	}
	return metadata.ExecuteWrite(context.Background(), writeFunc)
}

// ReplaceClusterName replaces all occurrences of oldClusterName with newClusterName
// It is called after a master failover
func ReplaceClusterName(oldClusterName string, newClusterName string) error {
	if oldClusterName == "" {
		return log.Errorf("replaceClusterName: skipping empty oldClusterName")
	}
	if newClusterName == "" {
		return log.Errorf("replaceClusterName: skipping empty newClusterName")
	}
	writeFunc := func() error {
		err := metadata.ReplaceInstanceClusterName(context.Background(), oldClusterName, newClusterName)
		if err != nil {
			return log.Errore(err)
		}
		instaudit.AuditOperation("replace-cluster-name", nil, fmt.Sprintf("replaxced %s with %s", oldClusterName, newClusterName))
		return nil
	}
	return metadata.ExecuteWrite(context.Background(), writeFunc)
}

// ReviewUnseenInstances reviews instances that have not been seen (supposedly dead) and updates some of their data
func ReviewUnseenInstances() error {
	instances, err := ReadUnseenInstances()
	if err != nil {
		return log.Errore(err)
	}
	operations := 0
	for _, instance := range instances {

		masterHostname, err := instresolve.ResolveHostname(instance.MasterKey.Hostname)
		if err != nil {
			log.Errore(err)
			continue
		}
		instance.MasterKey.Hostname = masterHostname
		savedClusterName := instance.ClusterName

		if err := ReadInstanceClusterAttributes(instance); err != nil {
			log.Errore(err)
		} else if instance.ClusterName != savedClusterName {
			updateInstanceClusterName(instance)
			operations++
		}
	}

	instaudit.AuditOperation("review-unseen-instances", nil, fmt.Sprintf("Operations: %d", operations))
	return err
}

// readUnseenMasterKeys will read list of masters that have never been seen, and yet whose replicas
// seem to be replicating.
func readUnseenMasterKeys() ([]instmodel.InstanceKey, error) {
	res := []instmodel.InstanceKey{}

	rows, err := metadata.ReadUnseenMasterKeys(context.Background())
	for _, row := range rows {
		instanceKey, _ := instresolve.NewInstanceKey(row.Hostname, row.Port)
		// we ignore the error. It can be expected that we are unable to resolve the hostname.
		// Maybe that's how we got here in the first place!
		res = append(res, *instanceKey)
	}
	if err != nil {
		return res, log.Errore(err)
	}

	return res, nil
}

// InjectSeed: intented to be used to inject an instance upon startup, assuming it's not already known to orchestrator.
func InjectSeed(instanceKey *instmodel.InstanceKey) error {
	if instanceKey == nil {
		return fmt.Errorf("InjectSeed: nil instanceKey")
	}
	clusterName := instanceKey.StringCode()
	// minimal details:
	instance := &instmodel.Instance{Key: *instanceKey, Version: "Unknown", ClusterName: clusterName}
	instance.SetSeed()
	err := WriteInstance(instance, false, nil)
	log.Debugf("InjectSeed: %+v, %+v", *instanceKey, err)
	instaudit.AuditOperation("inject-seed", instanceKey, "injected")
	return err
}

// InjectUnseenMasters will review masters of instances that are known to be replicating, yet which are not listed
// in database_instance. Since their replicas are listed as replicating, we can assume that such masters actually do
// exist: we shall therefore inject them with minimal details into the database_instance table.
func InjectUnseenMasters(probe func(*instmodel.InstanceKey) bool) error {

	unseenMasterKeys, err := readUnseenMasterKeys()
	if err != nil {
		return err
	}

	operations := 0
	for _, masterKey := range unseenMasterKeys {

		if instmodel.FiltersMatchInstanceKey(&masterKey, config.Config.Topology.Discovery.IgnoreMasterHostnames) {
			log.Debugf("InjectUnseenMasters: skipping discovery of %+v because it matches DiscoveryIgnoreMasterHostnameFilters", masterKey)
			continue
		}
		if instmodel.FiltersMatchInstanceKey(&masterKey, config.Config.Topology.Discovery.IgnoreHostnames) {
			log.Debugf("InjectUnseenMasters: skipping discovery of %+v because it matches DiscoveryIgnoreHostnameFilters", masterKey)
			continue
		}

		// We need to skip the master (intermediate replica) that uses replication user
		// that is filtered by DiscoveryIgnoreReplicationUsernameFilters.
		// Unfortunatly we don't know the replicaton user without examining the instance.
		// We need to do so first and then decide if we want to store it in
		// database_instance table (WriteInstance()) for regular discovery.
		// Note that we also can't store instance -> replication user mapping
		// somewhere and then use this information for filtering the host, as the user
		// can change, so if we know about this instance, we constantly need to check its
		// user and decide.
		// This is a corner case when during the discovery some replica was pointed out
		// and then we figured out its master (but not master's replication user).
		// In normal case, we point a chain master during the discovery, then we can
		// learn replica's usernames without examining actual replicas, so we can
		// skip them.
		skipped := probe(&masterKey)
		if skipped {
			if config.Config.Topology.Discovery.FilterLogsEnabled {
				log.Infof("InjectUnseenMasters: Skipping discovery of %+v because its replication user matches DiscoveryIgnoreReplicationUsernameFilters", masterKey)
			}
			continue
		}

		clusterName := masterKey.StringCode()
		// minimal details:
		instance := instmodel.Instance{Key: masterKey, Version: "Unknown", ClusterName: clusterName}
		if err := WriteInstance(&instance, false, nil); err == nil {
			operations++
		}
	}

	instaudit.AuditOperation("inject-unseen-masters", nil, fmt.Sprintf("Operations: %d", operations))
	return err
}

// ForgetUnseenInstancesDifferentlyResolved will purge instances which are invalid, and whose hostname
// appears on the hostname_resolved table; this means some time in the past their hostname was unresovled, and now
// resovled to a different value; the old hostname is never accessed anymore and the old entry should be removed.
func ForgetUnseenInstancesDifferentlyResolved() error {
	rowsAffected, err := metadata.ForgetUnseenDifferentlyResolvedInstances(context.Background())
	if err != nil {
		return log.Errore(err)
	}
	instaudit.AuditOperation("forget-unseen-differently-resolved", nil, fmt.Sprintf("Forgotten instances: %d", rowsAffected))
	return err
}

// readUnknownMasterHostnameResolves will figure out the resolved hostnames of master-hosts which cannot be found.
// It uses the hostname_resolve_history table to heuristically guess the correct hostname (based on "this was the
// last time we saw this hostname and it resolves into THAT")
func readUnknownMasterHostnameResolves() (map[string]string, error) {
	res := make(map[string]string)
	rows, err := metadata.ReadUnknownMasterHostnameResolves(context.Background())
	for _, row := range rows {
		res[row.Hostname] = row.ResolvedHostname
	}
	if err != nil {
		return res, log.Errore(err)
	}

	return res, nil
}

// ResolveUnknownMasterHostnameResolves fixes missing hostname resolves based on hostname_resolve_history
// The use case is replicas replicating from some unknown-hostname which cannot be otherwise found. This could
// happen due to an expire unresolve together with clearing up of hostname cache.
func ResolveUnknownMasterHostnameResolves() error {

	hostnameResolves, err := readUnknownMasterHostnameResolves()
	if err != nil {
		return err
	}
	for hostname, resolvedHostname := range hostnameResolves {
		instresolve.UpdateResolvedHostname(hostname, resolvedHostname)
	}

	instaudit.AuditOperation("resolve-unknown-masters", nil, fmt.Sprintf("Num resolved hostnames: %d", len(hostnameResolves)))
	return err
}

// ReadCountMySQLSnapshots is a utility method to return registered number of snapshots for a given list of hosts
func ReadCountMySQLSnapshots(hostnames []string) (map[string]int, error) {
	res := make(map[string]int)
	if !config.Config.Agents.ServeHTTP || len(hostnames) == 0 {
		return res, nil
	}
	rows, err := metadata.ReadHostSnapshotCounts(context.Background(), hostnames)
	for _, row := range rows {
		res[row.Hostname] = row.Count
	}

	if err != nil {
		log.Errore(err)
	}
	return res, err
}

// PopulateInstancesAgents will fill in extra data acquired from agents for given instances
// At current this is the number of snapshots.
// This isn't too pretty; it's a push-into-instance-data-that-belongs-to-agent thing.
// Originally the need was to visually present the number of snapshots per host on the web/cluster page, which
// indeed proves to be useful in our experience.
func PopulateInstancesAgents(instances []*instmodel.Instance) error {
	if len(instances) == 0 {
		return nil
	}
	hostnames := []string{}
	for _, instance := range instances {
		hostnames = append(hostnames, instance.Key.Hostname)
	}
	agentsCountMySQLSnapshots, err := ReadCountMySQLSnapshots(hostnames)
	if err != nil {
		return err
	}
	for _, instance := range instances {
		if count, ok := agentsCountMySQLSnapshots[instance.Key.Hostname]; ok {
			instance.CountMySQLSnapshots = count
		}
	}

	return nil
}

func GetClusterName(instanceKey *instmodel.InstanceKey) (clusterName string, err error) {
	if clusterName, found := instanceKeyInformativeClusterName.Get(instanceKey.StringCode()); found {
		return clusterName.(string), nil
	}
	rows, err := metadata.ReadInstanceClusterName(context.Background(), instanceKey.Hostname, instanceKey.Port)
	if err == nil && len(rows) > 0 {
		clusterName = rows[0]
		instanceKeyInformativeClusterName.Set(instanceKey.StringCode(), clusterName, cache.DefaultExpiration)
	}

	return clusterName, log.Errore(err)
}

// ReadClusters reads names of all known clusters
func ReadClusters() (clusterNames []string, err error) {
	clusters, err := ReadClustersInfo("")
	if err != nil {
		return clusterNames, err
	}
	for _, clusterInfo := range clusters {
		clusterNames = append(clusterNames, clusterInfo.ClusterName)
	}
	return clusterNames, nil
}

// ReadClusterInfo reads some info about a given cluster
func ReadClusterInfo(clusterName string) (*instcluster.ClusterInfo, error) {
	clusters, err := ReadClustersInfo(clusterName)
	if err != nil {
		return &instcluster.ClusterInfo{}, err
	}
	if len(clusters) != 1 {
		return &instcluster.ClusterInfo{}, fmt.Errorf("no cluster info found for %s", clusterName)
	}
	return &(clusters[0]), nil
}

// ReadClustersInfo reads names of all known clusters and some aggregated info
func ReadClustersInfo(clusterName string) ([]instcluster.ClusterInfo, error) {
	clusters := []instcluster.ClusterInfo{}

	rows, err := metadata.ReadClusterInfoRows(context.Background(), clusterName)
	for _, row := range rows {
		clusterInfo := instcluster.ClusterInfo{
			ClusterName:    row.ClusterName,
			CountInstances: row.CountInstances,
			ClusterAlias:   row.Alias,
			ClusterDomain:  row.DomainName,
		}
		clusterInfo.ApplyClusterAlias()
		clusterInfo.ReadRecoveryInfo()

		clusters = append(clusters, clusterInfo)
	}

	return clusters, err
}

// Get a listing of KVPair for clusters masters, for all clusters or for a specific cluster.
func GetMastersKVPairs(clusterName string) (kvPairs []*kv.KVPair, err error) {

	clusterAliasMap := make(map[string]string)
	if clustersInfo, err := ReadClustersInfo(clusterName); err != nil {
		return kvPairs, err
	} else {
		for _, clusterInfo := range clustersInfo {
			clusterAliasMap[clusterInfo.ClusterName] = clusterInfo.ClusterAlias
		}
	}

	masters, err := ReadWriteableClustersMasters()
	if err != nil {
		return kvPairs, err
	}
	for _, master := range masters {
		clusterPairs := instcluster.GetClusterMasterKVPairs(clusterAliasMap[master.ClusterName], &master.Key)
		kvPairs = append(kvPairs, clusterPairs...)
	}

	return kvPairs, err
}

// HeuristicallyApplyClusterDomainInstanceAttribute writes down the cluster-domain
// to master-hostname as a general attribute, by reading current topology and **trusting** it to be correct
func HeuristicallyApplyClusterDomainInstanceAttribute(clusterName string) (instanceKey *instmodel.InstanceKey, err error) {
	clusterInfo, err := ReadClusterInfo(clusterName)
	if err != nil {
		return nil, err
	}

	if clusterInfo.ClusterDomain == "" {
		return nil, fmt.Errorf("cannot find domain name for cluster %+v", clusterName)
	}

	masters, err := ReadClusterWriteableMaster(clusterName)
	if err != nil {
		return nil, err
	}
	if len(masters) != 1 {
		return nil, fmt.Errorf("found %+v potential master for cluster %+v", len(masters), clusterName)
	}
	instanceKey = &masters[0].Key
	return instanceKey, attributes.SetGeneralAttribute(clusterInfo.ClusterDomain, instanceKey.StringCode())
}

// GetHeuristicClusterDomainInstanceAttribute attempts detecting the cluster domain
// for the given cluster, and return the instance key associated as writer with that domain
func GetHeuristicClusterDomainInstanceAttribute(clusterName string) (instanceKey *instmodel.InstanceKey, err error) {
	clusterInfo, err := ReadClusterInfo(clusterName)
	if err != nil {
		return nil, err
	}

	if clusterInfo.ClusterDomain == "" {
		return nil, fmt.Errorf("cannot find domain name for cluster %+v", clusterName)
	}

	writerInstanceName, err := attributes.GetGeneralAttribute(clusterInfo.ClusterDomain)
	if err != nil {
		return nil, err
	}
	return instresolve.ParseRawInstanceKey(writerInstanceName)
}

// ReadAllInstanceKeys
func ReadAllInstanceKeys() ([]instmodel.InstanceKey, error) {
	res := []instmodel.InstanceKey{}
	rows, err := metadata.ReadAllInstanceKeys(context.Background())
	for _, row := range rows {
		instanceKey, merr := instresolve.NewInstanceKey(row.Hostname, row.Port)
		if merr != nil {
			log.Errore(merr)
		} else if !InstanceIsForgotten(instanceKey) {
			// only if not in "forget" cache
			res = append(res, *instanceKey)
		}
	}
	return res, log.Errore(err)
}

// ReadAllInstanceKeysMasterKeys
func ReadAllMinimalInstances() ([]instmodel.MinimalInstance, error) {
	res := []instmodel.MinimalInstance{}
	rows, err := metadata.ReadAllMinimalInstances(context.Background())
	for _, row := range rows {
		minimalInstance := instmodel.MinimalInstance{
			Key: instmodel.InstanceKey{
				Hostname: row.Hostname,
				Port:     row.Port,
			},
			MasterKey: instmodel.InstanceKey{
				Hostname: row.MasterHost,
				Port:     row.MasterPort,
			},
			ClusterName: row.ClusterName,
		}

		if !InstanceIsForgotten(&minimalInstance.Key) {
			// only if not in "forget" cache
			res = append(res, minimalInstance)
		}
	}
	return res, log.Errore(err)
}

// ReadOutdatedInstanceKeys reads and returns keys for all instances that are not up to date (i.e.
// pre-configured time has passed since they were last checked)
// But we also check for the case where an attempt at instance checking has been made, that hasn't
// resulted in an actual check! This can happen when TCP/IP connections are hung, in which case the "check"
// never returns. In such case we multiply interval by a factor, so as not to open too many connections on
// the instance.
func ReadOutdatedInstanceKeys() ([]instmodel.InstanceKey, error) {
	res := []instmodel.InstanceKey{}
	rows, err := metadata.ReadOutdatedInstanceKeys(context.Background(), config.Config.Topology.Discovery.PollSeconds)
	for _, row := range rows {
		instanceKey, merr := instresolve.NewInstanceKey(row.Hostname, row.Port)
		if merr != nil {
			log.Errore(merr)
		} else if !InstanceIsForgotten(instanceKey) {
			// only if not in "forget" cache
			res = append(res, *instanceKey)
		}
		// We don't stop on resolution errors because we want to keep filling the outdated instances list.
	}

	if err != nil {
		log.Errore(err)
	}
	return res, err

}
