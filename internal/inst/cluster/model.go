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

// Package cluster owns cluster identity, aliases, and cluster-level projections.
package cluster

import (
	"fmt"
	"regexp"

	"github.com/openark/orchestrator/internal/config"
	instmodel "github.com/openark/orchestrator/internal/inst/instance"
	instresolve "github.com/openark/orchestrator/internal/inst/resolve"
	"github.com/openark/orchestrator/internal/kv"
	"github.com/openark/orchestrator/internal/recoverypolicy"
)

func GetClusterMasterKVKey(clusterAlias string) string {
	return fmt.Sprintf("%s%s", config.Config.Consul.KV.ClusterMasterPrefix, clusterAlias)
}

func getClusterMasterKVPair(clusterAlias string, masterKey *instmodel.InstanceKey) *kv.KVPair {
	if clusterAlias == "" {
		return nil
	}
	if masterKey == nil {
		return nil
	}
	return kv.NewKVPair(GetClusterMasterKVKey(clusterAlias), masterKey.StringCode())
}

// GetClusterMasterKVPairs returns all KV pairs associated with a master. This includes the
// full identity of the master as well as a breakdown by hostname, port, ipv4, ipv6
func GetClusterMasterKVPairs(clusterAlias string, masterKey *instmodel.InstanceKey) (kvPairs []*kv.KVPair) {
	masterKVPair := getClusterMasterKVPair(clusterAlias, masterKey)
	if masterKVPair == nil {
		return kvPairs
	}
	kvPairs = append(kvPairs, masterKVPair)

	addPair := func(keySuffix, value string) {
		key := fmt.Sprintf("%s/%s", masterKVPair.Key, keySuffix)
		kvPairs = append(kvPairs, kv.NewKVPair(key, value))
	}

	addPair("hostname", masterKey.Hostname)
	addPair("port", fmt.Sprintf("%d", masterKey.Port))
	if ipv4, ipv6, err := instresolve.ReadHostnameIPs(masterKey.Hostname); err == nil {
		addPair("ipv4", ipv4)
		addPair("ipv6", ipv6)
	}
	return kvPairs
}

// MappedNameToAlias attempts to match a cluster with an alias based on
// configured ClusterNameToAlias map
func MappedNameToAlias(clusterName string) string {
	for pattern, alias := range config.Config.Topology.Classification.ClusterNameToAlias {
		if pattern == "" {
			// sanity
			continue
		}
		if matched, _ := regexp.MatchString(pattern, clusterName); matched {
			return alias
		}
	}
	return ""
}

// ClusterInfo makes for a cluster status/info summary
type ClusterInfo struct {
	ClusterName                            string
	ClusterAlias                           string // Human friendly alias
	ClusterDomain                          string // CNAME/VIP/A-record/whatever of the master of this cluster
	CountInstances                         uint
	HeuristicLag                           int64
	HasAutomatedMasterRecovery             bool
	HasAutomatedIntermediateMasterRecovery bool
}

// ReadRecoveryInfo
func (cluster *ClusterInfo) ReadRecoveryInfo() {
	policy := recoverypolicy.Current(cluster.ClusterName)
	cluster.HasAutomatedMasterRecovery = policy.AutoMasterRecovery
	cluster.HasAutomatedIntermediateMasterRecovery = policy.AutoIntermediateMasterRecovery
}

// ApplyClusterAlias updates the given clusterInfo's ClusterAlias property
func (cluster *ClusterInfo) ApplyClusterAlias() {
	if cluster.ClusterAlias != "" && cluster.ClusterAlias != cluster.ClusterName {
		// Already has an alias; abort
		return
	}
	if alias := MappedNameToAlias(cluster.ClusterName); alias != "" {
		cluster.ClusterAlias = alias
	}
}
