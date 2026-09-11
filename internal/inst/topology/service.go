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

// Package topology renders and inspects replication topology.
package topology

import (
	"fmt"
	"strings"

	"github.com/openark/orchestrator/internal/golib/log"
	"github.com/openark/orchestrator/internal/golib/util"
	instdiscovery "github.com/openark/orchestrator/internal/inst/discovery"
	instmodel "github.com/openark/orchestrator/internal/inst/instance"
	instinventory "github.com/openark/orchestrator/internal/inst/inventory"
	insttag "github.com/openark/orchestrator/internal/inst/tag"
)

var asciiFillerCharacter = " "
var tabulatorScharacter = "|"

// getASCIITopologyEntry will get an ascii topology tree rooted at given instance. Ir recursively
// draws the tree
func getASCIITopologyEntry(depth int, instance *instmodel.Instance, replicationMap map[*instmodel.Instance]([]*instmodel.Instance), extendedOutput bool, fillerCharacter string, tabulated bool, printTags bool) []string {
	if instance == nil {
		return []string{}
	}
	if instance.IsCoMaster && depth > 1 {
		return []string{}
	}
	prefix := ""
	if depth > 0 {
		prefix = strings.Repeat(fillerCharacter, (depth-1)*2)
		if instance.IsReplicationGroupSecondary() {
			prefix += "‡" + fillerCharacter
		} else {
			if instance.ReplicaRunning() && instance.IsLastCheckValid && instance.IsRecentlyChecked {
				prefix += "+" + fillerCharacter
			} else {
				prefix += "-" + fillerCharacter
			}
		}
	}
	entryAlias := ""
	if instance.InstanceAlias != "" {
		entryAlias = fmt.Sprintf(" (%s)", instance.InstanceAlias)
	}
	entry := fmt.Sprintf("%s%s%s", prefix, instance.Key.DisplayString(), entryAlias)
	if extendedOutput {
		if tabulated {
			entry = fmt.Sprintf("%s%s%s", entry, tabulatorScharacter, instance.TabulatedDescription(tabulatorScharacter))
		} else {
			entry = fmt.Sprintf("%s%s%s", entry, fillerCharacter, instance.HumanReadableDescription())
		}
		if printTags {
			tags, _ := insttag.ReadInstanceTags(&instance.Key)
			tagsString := make([]string, len(tags))
			for idx, tag := range tags {
				tagsString[idx] = tag.Display()
			}
			entry = fmt.Sprintf("%s [%s]", entry, strings.Join(tagsString, ","))
		}
	}
	result := []string{entry}
	for _, replica := range replicationMap[instance] {
		replicasResult := getASCIITopologyEntry(depth+1, replica, replicationMap, extendedOutput, fillerCharacter, tabulated, printTags)
		result = append(result, replicasResult...)
	}
	return result
}

// ASCIITopology returns a string representation of the topology of given cluster.
func ASCIITopology(clusterName string, historyTimestampPattern string, tabulated bool, printTags bool) (result string, err error) {
	fillerCharacter := asciiFillerCharacter
	var instances []*instmodel.Instance
	if historyTimestampPattern == "" {
		instances, err = instinventory.ReadClusterInstances(clusterName)
	} else {
		instances, err = instinventory.ReadHistoryClusterInstances(clusterName, historyTimestampPattern)
	}
	if err != nil {
		return "", err
	}

	instancesMap := make(map[instmodel.InstanceKey]*instmodel.Instance)
	for _, instance := range instances {
		log.Debugf("instanceKey: %+v", instance.Key)
		instancesMap[instance.Key] = instance
	}

	replicationMap := make(map[*instmodel.Instance]([]*instmodel.Instance))
	var masterInstance *instmodel.Instance
	// Investigate replicas:
	for _, instance := range instances {
		var masterOrGroupPrimary *instmodel.Instance
		var ok bool
		// If the current instance is a a group member, get the group's primary instead of the classical replication
		// source.
		if instance.IsReplicationGroupMember() && instance.IsReplicationGroupSecondary() {
			masterOrGroupPrimary, ok = instancesMap[instance.ReplicationGroupPrimaryInstanceKey]
		} else {
			masterOrGroupPrimary, ok = instancesMap[instance.MasterKey]
		}
		if ok {
			if _, ok := replicationMap[masterOrGroupPrimary]; !ok {
				replicationMap[masterOrGroupPrimary] = []*instmodel.Instance{}
			}
			if !instance.IsReplicationGroupPrimary() || (instance.IsReplicationGroupPrimary() && instance.IsReplica()) {
				replicationMap[masterOrGroupPrimary] = append(replicationMap[masterOrGroupPrimary], instance)
			}
		} else {
			masterInstance = instance
		}
	}
	// Get entries:
	var entries []string
	if masterInstance != nil {
		// Single master
		entries = getASCIITopologyEntry(0, masterInstance, replicationMap, historyTimestampPattern == "", fillerCharacter, tabulated, printTags)
	} else {
		// Co-masters? For visualization we put each in its own branch while ignoring its other co-masters.
		for _, instance := range instances {
			if instance.IsCoMaster {
				entries = append(entries, getASCIITopologyEntry(1, instance, replicationMap, historyTimestampPattern == "", fillerCharacter, tabulated, printTags)...)
			}
		}
	}
	// Beautify: make sure the "[...]" part is nicely aligned for all instances.
	if tabulated {
		entries = util.Tabulate(entries, "|", "|", util.TabulateLeft, util.TabulateRight)
	} else {
		indentationCharacter := "["
		maxIndent := 0
		for _, entry := range entries {
			maxIndent = max(maxIndent, strings.Index(entry, indentationCharacter))
		}
		for i, entry := range entries {
			entryIndent := strings.Index(entry, indentationCharacter)
			if maxIndent > entryIndent {
				tokens := strings.SplitN(entry, indentationCharacter, 2)
				newEntry := fmt.Sprintf("%s%s%s%s", tokens[0], strings.Repeat(fillerCharacter, maxIndent-entryIndent), indentationCharacter, tokens[1])
				entries[i] = newEntry
			}
		}
	}
	// Turn into string
	result = strings.Join(entries, "\n")
	return result, nil
}

// GetInstanceMaster synchronously reaches into the replication topology
// and retrieves master's data
func GetInstanceMaster(instance *instmodel.Instance) (*instmodel.Instance, error) {
	master, err := instdiscovery.ReadTopologyInstance(&instance.MasterKey)
	return master, err
}

// InstancesAreSiblings checks whether both instances are replicating from same master
func InstancesAreSiblings(instance0, instance1 *instmodel.Instance) bool {
	if !instance0.IsReplica() {
		return false
	}
	if !instance1.IsReplica() {
		return false
	}
	if instance0.Key.Equals(&instance1.Key) {
		// same instance...
		return false
	}
	return instance0.MasterKey.Equals(&instance1.MasterKey)
}

// InstanceIsMasterOf checks whether an instance is the master of another
func InstanceIsMasterOf(allegedMaster, allegedReplica *instmodel.Instance) bool {
	if !allegedReplica.IsReplica() {
		return false
	}
	if allegedMaster.Key.Equals(&allegedReplica.Key) {
		// same instance...
		return false
	}
	return allegedMaster.Key.Equals(&allegedReplica.MasterKey)
}
