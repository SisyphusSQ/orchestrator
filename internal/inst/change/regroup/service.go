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

package regroup

import (
	"fmt"
	"sort"
	"time"

	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/golib/log"
	instaudit "github.com/openark/orchestrator/internal/inst/audit"
	instrelocation "github.com/openark/orchestrator/internal/inst/change/relocation"
	instreplication "github.com/openark/orchestrator/internal/inst/change/replication"
	instmodel "github.com/openark/orchestrator/internal/inst/instance"
	instinventory "github.com/openark/orchestrator/internal/inst/inventory"
	modeldomain "github.com/openark/orchestrator/internal/models/domain"
	"github.com/openark/orchestrator/internal/recoverypolicy"
)

// sortInstances shuffles given list of instances according to some logic
func sortInstancesDataCenterHint(instances []*instmodel.Instance, dataCenterHint string) {
	sort.Sort(sort.Reverse(instmodel.NewInstancesSorterByExec(instances, dataCenterHint)))
}

// sortInstances shuffles given list of instances according to some logic
func sortInstances(instances []*instmodel.Instance) {
	sortInstancesDataCenterHint(instances, "")
}

// getReplicasForSorting returns a list of replicas of a given master potentially for candidate choosing
func getReplicasForSorting(masterKey *instmodel.InstanceKey, includeBinlogServerSubReplicas bool) (replicas []*instmodel.Instance, err error) {
	if includeBinlogServerSubReplicas {
		replicas, err = instinventory.ReadReplicaInstancesIncludingBinlogServerSubReplicas(masterKey)
	} else {
		replicas, err = instinventory.ReadReplicaInstances(masterKey)
	}
	return replicas, err
}

func sortedReplicas(replicas []*instmodel.Instance, stopReplicationMethod instreplication.StopReplicationMethod) []*instmodel.Instance {
	return sortedReplicasDataCenterHint(replicas, stopReplicationMethod, "")
}

// sortedReplicas returns the list of replicas of some master, sorted by exec coordinates
// (most up-to-date replica first).
// This function assumes given `replicas` argument is indeed a list of instances all replicating
// from the same master (the result of `getReplicasForSorting()` is appropriate)
func sortedReplicasDataCenterHint(replicas []*instmodel.Instance, stopReplicationMethod instreplication.StopReplicationMethod, dataCenterHint string) []*instmodel.Instance {
	if len(replicas) <= 1 {
		return replicas
	}
	replicas = instreplication.StopReplicas(replicas, stopReplicationMethod, time.Duration(config.Config.Topology.Operations.BulkWaitTimeoutSeconds)*time.Second)
	replicas = instmodel.RemoveNilInstances(replicas)

	sortInstancesDataCenterHint(replicas, dataCenterHint)
	for _, replica := range replicas {
		log.Debugf("- sorted replica: %+v %+v", replica.Key, replica.ExecBinlogCoordinates)
	}

	return replicas
}

// GetSortedReplicas reads list of replicas of a given master, and returns them sorted by exec coordinates
// (most up-to-date replica first).
func GetSortedReplicas(masterKey *instmodel.InstanceKey, stopReplicationMethod instreplication.StopReplicationMethod) (replicas []*instmodel.Instance, err error) {
	if replicas, err = getReplicasForSorting(masterKey, false); err != nil {
		return replicas, err
	}
	replicas = sortedReplicas(replicas, stopReplicationMethod)
	if len(replicas) == 0 {
		return replicas, fmt.Errorf("no replicas found for %+v", *masterKey)
	}
	return replicas, err
}

func isGenerallyValidAsBinlogSource(replica *instmodel.Instance) bool {
	if !replica.IsLastCheckValid {
		// something wrong with this replica right now. We shouldn't hope to be able to promote it
		return false
	}
	if !replica.LogBinEnabled {
		return false
	}
	if !replica.LogReplicationUpdatesEnabled {
		return false
	}

	return true
}

func isGenerallyValidAsCandidateReplica(replica *instmodel.Instance) bool {
	if !isGenerallyValidAsBinlogSource(replica) {
		// does not have binary logs
		return false
	}
	if replica.IsBinlogServer() {
		// Can't regroup under a binlog server because it does not support pseudo-gtid related queries such as SHOW BINLOG EVENTS
		return false
	}

	return true
}

// isValidAsCandidateMasterInBinlogServerTopology let's us know whether a given replica is generally
// valid to promote to be master.
func isValidAsCandidateMasterInBinlogServerTopology(replica *instmodel.Instance) bool {
	if !replica.IsLastCheckValid {
		// something wrong with this replica right now. We shouldn't hope to be able to promote it
		return false
	}
	if !replica.LogBinEnabled {
		return false
	}
	if replica.LogReplicationUpdatesEnabled {
		// That's right: we *disallow* log-replica-updates
		return false
	}
	if replica.IsBinlogServer() {
		return false
	}

	return true
}

func IsBannedFromBeingCandidateReplica(replica *instmodel.Instance) bool {
	return isBannedFromBeingCandidateReplica(replica, recoverypolicy.Current(replica.ClusterName).PromotionIgnoreHostnameFilters)
}

func isBannedFromBeingCandidateReplica(replica *instmodel.Instance, promotionIgnoreHostnameFilters []string) bool {
	if replica.PromotionRule == instmodel.MustNotPromoteRule {
		log.Debugf("instance %+v is banned because of promotion rule", replica.Key)
		return true
	}
	if instmodel.FiltersMatchInstanceKey(&replica.Key, promotionIgnoreHostnameFilters) {
		return true
	}
	return false
}

// getPriorityMajorVersionForCandidate returns the primary (most common) major version found
// among given instances. This will be used for choosing best candidate for promotion.
func getPriorityMajorVersionForCandidate(replicas []*instmodel.Instance) (priorityMajorVersion string, err error) {
	if len(replicas) == 0 {
		return "", log.Errorf("empty replicas list in getPriorityMajorVersionForCandidate")
	}
	majorVersionsCount := make(map[string]int)
	for _, replica := range replicas {
		majorVersionsCount[replica.MajorVersionString()] = majorVersionsCount[replica.MajorVersionString()] + 1
	}
	if len(majorVersionsCount) == 1 {
		// all same version, simple case
		return replicas[0].MajorVersionString(), nil
	}
	sorted := instmodel.NewMajorVersionsSortedByCount(majorVersionsCount)
	sort.Sort(sort.Reverse(sorted))
	return sorted.First(), nil
}

// getPriorityBinlogFormatForCandidate returns the primary (most common) binlog format found
// among given instances. This will be used for choosing best candidate for promotion.
func getPriorityBinlogFormatForCandidate(replicas []*instmodel.Instance) (priorityBinlogFormat string, err error) {
	if len(replicas) == 0 {
		return "", log.Errorf("empty replicas list in getPriorityBinlogFormatForCandidate")
	}
	binlogFormatsCount := make(map[string]int)
	for _, replica := range replicas {
		binlogFormatsCount[replica.Binlog_format] = binlogFormatsCount[replica.Binlog_format] + 1
	}
	if len(binlogFormatsCount) == 1 {
		// all same binlog format, simple case
		return replicas[0].Binlog_format, nil
	}
	sorted := instmodel.NewBinlogFormatSortedByCount(binlogFormatsCount)
	sort.Sort(sort.Reverse(sorted))
	return sorted.First(), nil
}

// chooseCandidateReplica
func chooseCandidateReplica(replicas []*instmodel.Instance) (candidateReplica *instmodel.Instance, aheadReplicas, equalReplicas, laterReplicas, cannotReplicateReplicas []*instmodel.Instance, err error) {
	if len(replicas) == 0 {
		return candidateReplica, aheadReplicas, equalReplicas, laterReplicas, cannotReplicateReplicas, fmt.Errorf("no replicas found given in chooseCandidateReplica")
	}
	priorityMajorVersion, _ := getPriorityMajorVersionForCandidate(replicas)
	priorityBinlogFormat, _ := getPriorityBinlogFormatForCandidate(replicas)

	for _, replica := range replicas {
		if isGenerallyValidAsCandidateReplica(replica) &&
			!IsBannedFromBeingCandidateReplica(replica) &&
			!instmodel.IsSmallerMajorVersion(priorityMajorVersion, replica.MajorVersionString()) &&
			!instmodel.IsSmallerBinlogFormat(priorityBinlogFormat, replica.Binlog_format) {
			// this is the one
			candidateReplica = replica
			break
		}
	}
	if candidateReplica == nil {
		// Unable to find a candidate that will master others.
		// Instead, pick a (single) replica which is not banned.
		for _, replica := range replicas {
			if !IsBannedFromBeingCandidateReplica(replica) {
				// this is the one
				candidateReplica = replica
				break
			}
		}
		if candidateReplica != nil {
			replicas = instmodel.RemoveInstance(replicas, &candidateReplica.Key)
		}
		return candidateReplica, replicas, equalReplicas, laterReplicas, cannotReplicateReplicas, fmt.Errorf("chooseCandidateReplica: no candidate replica found")
	}
	replicas = instmodel.RemoveInstance(replicas, &candidateReplica.Key)
	for _, replica := range replicas {
		if canReplicate, err := replica.CanReplicateFromEx(candidateReplica, "chooseCandidateReplica()"); !canReplicate {
			// lost due to inability to replicate
			cannotReplicateReplicas = append(cannotReplicateReplicas, replica)
			if err != nil {
				log.Errorf("chooseCandidateReplica(): error checking CanReplicateFrom(). replica: %v; error: %v", replica.Key, err)
			}
		} else if replica.ExecBinlogCoordinates.SmallerThan(&candidateReplica.ExecBinlogCoordinates) {
			laterReplicas = append(laterReplicas, replica)
		} else if replica.ExecBinlogCoordinates.Equals(&candidateReplica.ExecBinlogCoordinates) {
			equalReplicas = append(equalReplicas, replica)
		} else {
			// lost due to being more advanced/ahead of chosen replica.
			aheadReplicas = append(aheadReplicas, replica)
		}
	}
	return candidateReplica, aheadReplicas, equalReplicas, laterReplicas, cannotReplicateReplicas, err
}

// GetCandidateReplica chooses the best replica to promote given a (possibly dead) master
func GetCandidateReplica(masterKey *instmodel.InstanceKey, forRematchPurposes bool) (*instmodel.Instance, []*instmodel.Instance, []*instmodel.Instance, []*instmodel.Instance, []*instmodel.Instance, error) {
	var candidateReplica *instmodel.Instance
	aheadReplicas := []*instmodel.Instance{}
	equalReplicas := []*instmodel.Instance{}
	laterReplicas := []*instmodel.Instance{}
	cannotReplicateReplicas := []*instmodel.Instance{}

	dataCenterHint := ""
	if master, _, _ := instinventory.ReadInstance(masterKey); master != nil {
		dataCenterHint = master.DataCenter
	}
	replicas, err := getReplicasForSorting(masterKey, false)
	if err != nil {
		return candidateReplica, aheadReplicas, equalReplicas, laterReplicas, cannotReplicateReplicas, err
	}
	var stopReplicationMethod instreplication.StopReplicationMethod = instreplication.NoStopReplication
	if forRematchPurposes {
		stopReplicationMethod = instreplication.StopReplicationNice
	}
	replicas = sortedReplicasDataCenterHint(replicas, stopReplicationMethod, dataCenterHint)
	if len(replicas) == 0 {
		return candidateReplica, aheadReplicas, equalReplicas, laterReplicas, cannotReplicateReplicas, fmt.Errorf("no replicas found for %+v", *masterKey)
	}
	candidateReplica, aheadReplicas, equalReplicas, laterReplicas, cannotReplicateReplicas, err = chooseCandidateReplica(replicas)
	if err != nil {
		return candidateReplica, aheadReplicas, equalReplicas, laterReplicas, cannotReplicateReplicas, err
	}
	if candidateReplica != nil {
		mostUpToDateReplica := replicas[0]
		if candidateReplica.ExecBinlogCoordinates.SmallerThan(&mostUpToDateReplica.ExecBinlogCoordinates) {
			log.Warningf("GetCandidateReplica: chosen replica: %+v is behind most-up-to-date replica: %+v", candidateReplica.Key, mostUpToDateReplica.Key)
		}
	}
	log.Debugf("GetCandidateReplica: candidate: %+v, ahead: %d, equal: %d, late: %d, break: %d", candidateReplica.Key, len(aheadReplicas), len(equalReplicas), len(laterReplicas), len(cannotReplicateReplicas))
	return candidateReplica, aheadReplicas, equalReplicas, laterReplicas, cannotReplicateReplicas, nil
}

// GetCandidateReplicaOfBinlogServerTopology chooses the best replica to promote given a (possibly dead) master
func GetCandidateReplicaOfBinlogServerTopology(masterKey *instmodel.InstanceKey) (candidateReplica *instmodel.Instance, err error) {
	replicas, err := getReplicasForSorting(masterKey, true)
	if err != nil {
		return candidateReplica, err
	}
	replicas = sortedReplicas(replicas, instreplication.NoStopReplication)
	if len(replicas) == 0 {
		return candidateReplica, fmt.Errorf("no replicas found for %+v", *masterKey)
	}
	for _, replica := range replicas {
		if candidateReplica != nil {
			break
		}
		if isValidAsCandidateMasterInBinlogServerTopology(replica) && !IsBannedFromBeingCandidateReplica(replica) {
			// this is the one
			candidateReplica = replica
		}
	}
	if candidateReplica != nil {
		log.Debugf("GetCandidateReplicaOfBinlogServerTopology: returning %+v as candidate replica for %+v", candidateReplica.Key, *masterKey)
	} else {
		log.Debugf("GetCandidateReplicaOfBinlogServerTopology: no candidate replica found for %+v", *masterKey)
	}
	return candidateReplica, err
}

// RegroupReplicasPseudoGTID will choose a candidate replica of a given instance, and take its siblings using pseudo-gtid
func RegroupReplicasPseudoGTID(
	masterKey *instmodel.InstanceKey,
	returnReplicaEvenOnFailureToRegroup bool,
	onCandidateReplicaChosen func(*instmodel.Instance),
	postponedFunctionsContainer *instrelocation.PostponedFunctionsContainer,
	postponeAllMatchOperations func(*instmodel.Instance, bool) bool,
) (
	aheadReplicas []*instmodel.Instance,
	equalReplicas []*instmodel.Instance,
	laterReplicas []*instmodel.Instance,
	cannotReplicateReplicas []*instmodel.Instance,
	candidateReplica *instmodel.Instance,
	err error,
) {
	candidateReplica, aheadReplicas, equalReplicas, laterReplicas, cannotReplicateReplicas, err = GetCandidateReplica(masterKey, true)
	if err != nil {
		if !returnReplicaEvenOnFailureToRegroup {
			candidateReplica = nil
		}
		return aheadReplicas, equalReplicas, laterReplicas, cannotReplicateReplicas, candidateReplica, err
	}

	if config.Config.PseudoGTID.Pattern == "" {
		return aheadReplicas, equalReplicas, laterReplicas, cannotReplicateReplicas, candidateReplica, fmt.Errorf("PseudoGTIDPattern not configured; cannot use Pseudo-GTID")
	}

	if onCandidateReplicaChosen != nil {
		onCandidateReplicaChosen(candidateReplica)
	}

	allMatchingFunc := func() error {
		log.Debugf("RegroupReplicas: working on %d equals replicas", len(equalReplicas))
		barrier := make(chan *instmodel.InstanceKey)
		for _, replica := range equalReplicas {
			// This replica has the exact same executing coordinates as the candidate replica. This replica
			// is *extremely* easy to attach below the candidate replica!
			go func() {
				defer func() { barrier <- &candidateReplica.Key }()
				instreplication.ExecuteOnTopology(func() {
					instreplication.ChangeMasterTo(&replica.Key, &candidateReplica.Key, &candidateReplica.SelfBinlogCoordinates, false, modeldomain.GTIDHintDeny)
				})
			}()
		}
		for range equalReplicas {
			<-barrier
		}

		log.Debugf("RegroupReplicas: multi matching %d later replicas", len(laterReplicas))
		// As for the laterReplicas, we'll have to apply pseudo GTID
		laterReplicas, candidateReplica, _, err = instrelocation.MultiMatchBelow(laterReplicas, &candidateReplica.Key, postponedFunctionsContainer)

		operatedReplicas := append(equalReplicas, candidateReplica)
		operatedReplicas = append(operatedReplicas, laterReplicas...)
		log.Debugf("RegroupReplicas: starting %d replicas", len(operatedReplicas))
		barrier = make(chan *instmodel.InstanceKey)
		for _, replica := range operatedReplicas {
			go func() {
				defer func() { barrier <- &candidateReplica.Key }()
				instreplication.ExecuteOnTopology(func() {
					instreplication.StartReplication(&replica.Key)
				})
			}()
		}
		for range operatedReplicas {
			<-barrier
		}
		instaudit.AuditOperation("regroup-replicas", masterKey, fmt.Sprintf("regrouped %+v replicas below %+v", len(operatedReplicas), *masterKey))
		return err
	}
	if postponedFunctionsContainer != nil && postponeAllMatchOperations != nil && postponeAllMatchOperations(candidateReplica, false) {
		postponedFunctionsContainer.AddPostponedFunction(allMatchingFunc, fmt.Sprintf("regroup-replicas-pseudo-gtid %+v", candidateReplica.Key))
	} else {
		err = allMatchingFunc()
	}
	log.Debugf("RegroupReplicas: done")
	// aheadReplicas are lost (they were ahead in replication as compared to promoted replica)
	return aheadReplicas, equalReplicas, laterReplicas, cannotReplicateReplicas, candidateReplica, err
}

func getMostUpToDateActiveBinlogServer(masterKey *instmodel.InstanceKey) (mostAdvancedBinlogServer *instmodel.Instance, binlogServerReplicas []*instmodel.Instance, err error) {
	if binlogServerReplicas, err = instinventory.ReadBinlogServerReplicaInstances(masterKey); err == nil && len(binlogServerReplicas) > 0 {
		// Pick the most advanced binlog sever that is good to go
		for _, binlogServer := range binlogServerReplicas {
			if binlogServer.IsLastCheckValid {
				if mostAdvancedBinlogServer == nil {
					mostAdvancedBinlogServer = binlogServer
				}
				if mostAdvancedBinlogServer.ExecBinlogCoordinates.SmallerThan(&binlogServer.ExecBinlogCoordinates) {
					mostAdvancedBinlogServer = binlogServer
				}
			}
		}
	}
	return mostAdvancedBinlogServer, binlogServerReplicas, err
}

// RegroupReplicasPseudoGTIDIncludingSubReplicasOfBinlogServers uses Pseugo-GTID to regroup replicas
// of given instance. The function also drill in to replicas of binlog servers that are replicating from given instance,
// and other recursive binlog servers, as long as they're in the same binlog-server-family.
func RegroupReplicasPseudoGTIDIncludingSubReplicasOfBinlogServers(
	masterKey *instmodel.InstanceKey,
	returnReplicaEvenOnFailureToRegroup bool,
	onCandidateReplicaChosen func(*instmodel.Instance),
	postponedFunctionsContainer *instrelocation.PostponedFunctionsContainer,
	postponeAllMatchOperations func(*instmodel.Instance, bool) bool,
) (
	aheadReplicas []*instmodel.Instance,
	equalReplicas []*instmodel.Instance,
	laterReplicas []*instmodel.Instance,
	cannotReplicateReplicas []*instmodel.Instance,
	candidateReplica *instmodel.Instance,
	err error,
) {
	// First, handle binlog server issues:
	func() error {
		log.Debugf("RegroupReplicasIncludingSubReplicasOfBinlogServers: starting on replicas of %+v", *masterKey)
		// Find the most up to date binlog server:
		mostUpToDateBinlogServer, binlogServerReplicas, err := getMostUpToDateActiveBinlogServer(masterKey)
		if err != nil {
			return log.Errore(err)
		}
		if mostUpToDateBinlogServer == nil {
			log.Debugf("RegroupReplicasIncludingSubReplicasOfBinlogServers: no binlog server replicates from %+v", *masterKey)
			// No binlog server; proceed as normal
			return nil
		}
		log.Debugf("RegroupReplicasIncludingSubReplicasOfBinlogServers: most up to date binlog server of %+v: %+v", *masterKey, mostUpToDateBinlogServer.Key)

		// Find the most up to date candidate replica:
		candidateReplica, _, _, _, _, err := GetCandidateReplica(masterKey, true)
		if err != nil {
			return log.Errore(err)
		}
		if candidateReplica == nil {
			log.Debugf("RegroupReplicasIncludingSubReplicasOfBinlogServers: no candidate replica for %+v", *masterKey)
			// Let the followup code handle that
			return nil
		}
		log.Debugf("RegroupReplicasIncludingSubReplicasOfBinlogServers: candidate replica of %+v: %+v", *masterKey, candidateReplica.Key)

		if candidateReplica.ExecBinlogCoordinates.SmallerThan(&mostUpToDateBinlogServer.ExecBinlogCoordinates) {
			log.Debugf("RegroupReplicasIncludingSubReplicasOfBinlogServers: candidate replica %+v coordinates smaller than binlog server %+v", candidateReplica.Key, mostUpToDateBinlogServer.Key)
			// Need to align under binlog server...
			candidateReplica, err = instrelocation.Repoint(&candidateReplica.Key, &mostUpToDateBinlogServer.Key, modeldomain.GTIDHintDeny)
			if err != nil {
				return log.Errore(err)
			}
			log.Debugf("RegroupReplicasIncludingSubReplicasOfBinlogServers: repointed candidate replica %+v under binlog server %+v", candidateReplica.Key, mostUpToDateBinlogServer.Key)
			candidateReplica, err = instreplication.StartReplicationUntilMasterCoordinates(&candidateReplica.Key, &mostUpToDateBinlogServer.ExecBinlogCoordinates)
			if err != nil {
				return log.Errore(err)
			}
			log.Debugf("RegroupReplicasIncludingSubReplicasOfBinlogServers: aligned candidate replica %+v under binlog server %+v", candidateReplica.Key, mostUpToDateBinlogServer.Key)
			// and move back
			candidateReplica, err = instrelocation.Repoint(&candidateReplica.Key, masterKey, modeldomain.GTIDHintDeny)
			if err != nil {
				return log.Errore(err)
			}
			log.Debugf("RegroupReplicasIncludingSubReplicasOfBinlogServers: repointed candidate replica %+v under master %+v", candidateReplica.Key, *masterKey)
			return nil
		}
		// Either because it _was_ like that, or we _made_ it so,
		// candidate replica is as/more up to date than all binlog servers
		for _, binlogServer := range binlogServerReplicas {
			log.Debugf("RegroupReplicasIncludingSubReplicasOfBinlogServers: matching replicas of binlog server %+v below %+v", binlogServer.Key, candidateReplica.Key)
			// Right now sequentially.
			// At this point just do what you can, don't return an error
			instrelocation.MultiMatchReplicas(&binlogServer.Key, &candidateReplica.Key, "")
			log.Debugf("RegroupReplicasIncludingSubReplicasOfBinlogServers: done matching replicas of binlog server %+v below %+v", binlogServer.Key, candidateReplica.Key)
		}
		log.Debugf("RegroupReplicasIncludingSubReplicasOfBinlogServers: done handling binlog regrouping for %+v; will proceed with normal RegroupReplicas", *masterKey)
		instaudit.AuditOperation("regroup-replicas-including-bls", masterKey, fmt.Sprintf("matched replicas of binlog server replicas of %+v under %+v", *masterKey, candidateReplica.Key))
		return nil
	}()
	// Proceed to normal regroup:
	return RegroupReplicasPseudoGTID(masterKey, returnReplicaEvenOnFailureToRegroup, onCandidateReplicaChosen, postponedFunctionsContainer, postponeAllMatchOperations)
}

// RegroupReplicasGTID will choose a candidate replica of a given instance, and take its siblings using GTID
func RegroupReplicasGTID(
	masterKey *instmodel.InstanceKey,
	returnReplicaEvenOnFailureToRegroup bool,
	startReplicationOnCandidate bool,
	onCandidateReplicaChosen func(*instmodel.Instance),
	postponedFunctionsContainer *instrelocation.PostponedFunctionsContainer,
	postponeAllMatchOperations func(*instmodel.Instance, bool) bool,
) (
	lostReplicas []*instmodel.Instance,
	movedReplicas []*instmodel.Instance,
	cannotReplicateReplicas []*instmodel.Instance,
	candidateReplica *instmodel.Instance,
	err error,
) {
	var emptyReplicas []*instmodel.Instance
	var unmovedReplicas []*instmodel.Instance
	candidateReplica, aheadReplicas, equalReplicas, laterReplicas, cannotReplicateReplicas, err := GetCandidateReplica(masterKey, true)
	if err != nil {
		if !returnReplicaEvenOnFailureToRegroup {
			candidateReplica = nil
		}
		return emptyReplicas, emptyReplicas, emptyReplicas, candidateReplica, err
	}

	if onCandidateReplicaChosen != nil {
		onCandidateReplicaChosen(candidateReplica)
	}
	replicasToMove := append(equalReplicas, laterReplicas...)
	hasBestPromotionRule := true
	if candidateReplica != nil {
		for _, replica := range replicasToMove {
			if replica.PromotionRule.BetterThan(candidateReplica.PromotionRule) {
				hasBestPromotionRule = false
			}
		}
	}
	moveGTIDFunc := func() error {
		log.Debugf("RegroupReplicasGTID: working on %d replicas", len(replicasToMove))

		movedReplicas, unmovedReplicas, _, err = instrelocation.MoveReplicasViaGTID(replicasToMove, candidateReplica, postponedFunctionsContainer)
		unmovedReplicas = append(unmovedReplicas, aheadReplicas...)
		return log.Errore(err)
	}
	if postponedFunctionsContainer != nil && postponeAllMatchOperations != nil && postponeAllMatchOperations(candidateReplica, hasBestPromotionRule) {
		postponedFunctionsContainer.AddPostponedFunction(moveGTIDFunc, fmt.Sprintf("regroup-replicas-gtid %+v", candidateReplica.Key))
	} else {
		err = moveGTIDFunc()
	}

	if startReplicationOnCandidate {
		instreplication.StartReplication(&candidateReplica.Key)
	}

	log.Debugf("RegroupReplicasGTID: done")
	instaudit.AuditOperation("regroup-replicas-gtid", masterKey, fmt.Sprintf("regrouped replicas of %+v via GTID; promoted %+v", *masterKey, candidateReplica.Key))
	return unmovedReplicas, movedReplicas, cannotReplicateReplicas, candidateReplica, err
}

// RegroupReplicasBinlogServers works on a binlog-servers topology. It picks the most up-to-date BLS and repoints all other
// BLS below it
func RegroupReplicasBinlogServers(masterKey *instmodel.InstanceKey, returnReplicaEvenOnFailureToRegroup bool) (repointedBinlogServers []*instmodel.Instance, promotedBinlogServer *instmodel.Instance, err error) {
	var binlogServerReplicas []*instmodel.Instance
	promotedBinlogServer, binlogServerReplicas, err = getMostUpToDateActiveBinlogServer(masterKey)

	resultOnError := func(err error) ([]*instmodel.Instance, *instmodel.Instance, error) {
		if !returnReplicaEvenOnFailureToRegroup {
			promotedBinlogServer = nil
		}
		return repointedBinlogServers, promotedBinlogServer, err
	}

	if err != nil {
		return resultOnError(err)
	}

	repointedBinlogServers, _, err = instrelocation.RepointTo(binlogServerReplicas, &promotedBinlogServer.Key)

	if err != nil {
		return resultOnError(err)
	}
	instaudit.AuditOperation("regroup-replicas-bls", masterKey, fmt.Sprintf("regrouped binlog server replicas of %+v; promoted %+v", *masterKey, promotedBinlogServer.Key))
	return repointedBinlogServers, promotedBinlogServer, nil
}

// RegroupReplicas is a "smart" method of promoting one replica over the others ("promoting" it on top of its siblings)
// This method decides which strategy to use: GTID, Pseudo-GTID, Binlog Servers.
func RegroupReplicas(masterKey *instmodel.InstanceKey, returnReplicaEvenOnFailureToRegroup bool,
	onCandidateReplicaChosen func(*instmodel.Instance),
	postponedFunctionsContainer *instrelocation.PostponedFunctionsContainer) (

	aheadReplicas []*instmodel.Instance,
	equalReplicas []*instmodel.Instance,
	laterReplicas []*instmodel.Instance,
	cannotReplicateReplicas []*instmodel.Instance,
	instance *instmodel.Instance,
	err error,
) {
	//
	var emptyReplicas []*instmodel.Instance

	replicas, err := instinventory.ReadReplicaInstances(masterKey)
	if err != nil {
		return emptyReplicas, emptyReplicas, emptyReplicas, emptyReplicas, instance, err
	}
	if len(replicas) == 0 {
		return emptyReplicas, emptyReplicas, emptyReplicas, emptyReplicas, instance, err
	}
	if len(replicas) == 1 {
		return emptyReplicas, emptyReplicas, emptyReplicas, emptyReplicas, replicas[0], err
	}
	allGTID := true
	allBinlogServers := true
	allPseudoGTID := true
	for _, replica := range replicas {
		if !replica.UsingGTID() {
			allGTID = false
		}
		if !replica.IsBinlogServer() {
			allBinlogServers = false
		}
		if !replica.UsingPseudoGTID {
			allPseudoGTID = false
		}
	}
	if allGTID {
		log.Debugf("RegroupReplicas: using GTID to regroup replicas of %+v", *masterKey)
		unmovedReplicas, movedReplicas, cannotReplicateReplicas, candidateReplica, err := RegroupReplicasGTID(masterKey, returnReplicaEvenOnFailureToRegroup, true, onCandidateReplicaChosen, nil, nil)
		return unmovedReplicas, emptyReplicas, movedReplicas, cannotReplicateReplicas, candidateReplica, err
	}
	if allBinlogServers {
		log.Debugf("RegroupReplicas: using binlog servers to regroup replicas of %+v", *masterKey)
		movedReplicas, candidateReplica, err := RegroupReplicasBinlogServers(masterKey, returnReplicaEvenOnFailureToRegroup)
		return emptyReplicas, emptyReplicas, movedReplicas, cannotReplicateReplicas, candidateReplica, err
	}
	if allPseudoGTID {
		log.Debugf("RegroupReplicas: using Pseudo-GTID to regroup replicas of %+v", *masterKey)
		return RegroupReplicasPseudoGTID(masterKey, returnReplicaEvenOnFailureToRegroup, onCandidateReplicaChosen, postponedFunctionsContainer, nil)
	}
	// And, as last resort, we do PseudoGTID & binlog servers
	log.Warningf("RegroupReplicas: unsure what method to invoke for %+v; trying Pseudo-GTID+Binlog Servers", *masterKey)
	return RegroupReplicasPseudoGTIDIncludingSubReplicasOfBinlogServers(masterKey, returnReplicaEvenOnFailureToRegroup, onCandidateReplicaChosen, postponedFunctionsContainer, nil)
}
