/*
   Copyright 2017 Shlomi Noach, GitHub Inc.

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

package raftstate

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"

	instdiscovery "github.com/openark/orchestrator/internal/inst/discovery"
	instmodel "github.com/openark/orchestrator/internal/inst/instance"
	instinventory "github.com/openark/orchestrator/internal/inst/inventory"
	"github.com/openark/orchestrator/internal/logic/discovery"
	"github.com/openark/orchestrator/internal/logic/recovery"
	modeldomain "github.com/openark/orchestrator/internal/models/domain"
	"github.com/openark/orchestrator/internal/recoverypolicy"
	"github.com/openark/orchestrator/internal/repository/metadata"

	"github.com/openark/orchestrator/internal/golib/log"
	orcraft "github.com/openark/orchestrator/internal/raft"
)

type SnapshotData struct {
	Keys             []instmodel.InstanceKey // Kept for backwards comapatibility
	MinimalInstances []instmodel.MinimalInstance
	RecoveryDisabled bool

	ClusterAlias,
	ClusterAliasOverride,
	ClusterDomainName,
	HostAttributes,
	InstanceTags,
	AccessToken,
	PoolInstances,
	InjectedPseudoGTIDClusters,
	HostnameResolves,
	HostnameUnresolves,
	DowntimedInstances,
	Candidates,
	Detections,
	KVStore,
	Recovery,
	RecoverySteps,
	RecoveryPolicy,
	RecoveryHookProfiles,
	RecoveryHookAssignments modeldomain.NamedResultData

	LeaderURI string
}

func NewSnapshotData() *SnapshotData {
	return &SnapshotData{}
}

func readTableData(tableName string, data *modeldomain.NamedResultData) error {
	var err error
	*data, err = metadata.ReadSnapshotTable(context.Background(), tableName)
	return log.Errore(err)
}

func writeTableData(tableName string, data *modeldomain.NamedResultData) error {
	err := metadata.WriteSnapshotTable(context.Background(), tableName, *data)
	return log.Errore(err)
}

// CreateSnapshotData 读取完整快照；任何表读取失败都不输出部分快照。
func CreateSnapshotData() (*SnapshotData, error) {
	snapshotData := NewSnapshotData()

	snapshotData.LeaderURI = orcraft.LeaderURI.Get()
	// keys
	var err error
	snapshotData.Keys, err = instinventory.ReadAllInstanceKeys()
	if err != nil {
		return nil, err
	}
	snapshotData.MinimalInstances, err = instinventory.ReadAllMinimalInstances()
	if err != nil {
		return nil, err
	}
	snapshotData.RecoveryDisabled, err = recovery.IsRecoveryDisabled()
	if err != nil {
		return nil, err
	}

	if err := readTableData("cluster_alias", &snapshotData.ClusterAlias); err != nil {
		return nil, err
	}
	if err := readTableData("cluster_alias_override", &snapshotData.ClusterAliasOverride); err != nil {
		return nil, err
	}
	if err := readTableData("cluster_domain_name", &snapshotData.ClusterDomainName); err != nil {
		return nil, err
	}
	if err := readTableData("access_token", &snapshotData.AccessToken); err != nil {
		return nil, err
	}
	if err := readTableData("host_attributes", &snapshotData.HostAttributes); err != nil {
		return nil, err
	}
	if err := readTableData("database_instance_tags", &snapshotData.InstanceTags); err != nil {
		return nil, err
	}
	if err := readTableData("database_instance_pool", &snapshotData.PoolInstances); err != nil {
		return nil, err
	}
	if err := readTableData("hostname_resolve", &snapshotData.HostnameResolves); err != nil {
		return nil, err
	}
	if err := readTableData("hostname_unresolve", &snapshotData.HostnameUnresolves); err != nil {
		return nil, err
	}
	if err := readTableData("database_instance_downtime", &snapshotData.DowntimedInstances); err != nil {
		return nil, err
	}
	if err := readTableData("candidate_database_instance", &snapshotData.Candidates); err != nil {
		return nil, err
	}
	if err := readTableData("topology_failure_detection", &snapshotData.Detections); err != nil {
		return nil, err
	}
	if err := readTableData("kv_store", &snapshotData.KVStore); err != nil {
		return nil, err
	}
	if err := readTableData("topology_recovery", &snapshotData.Recovery); err != nil {
		return nil, err
	}
	if err := readTableData("topology_recovery_steps", &snapshotData.RecoverySteps); err != nil {
		return nil, err
	}
	if err := readTableData("recovery_policy", &snapshotData.RecoveryPolicy); err != nil {
		return nil, err
	}
	if err := readTableData("recovery_hook_profile", &snapshotData.RecoveryHookProfiles); err != nil {
		return nil, err
	}
	if err := readTableData("recovery_hook_assignment", &snapshotData.RecoveryHookAssignments); err != nil {
		return nil, err
	}
	if err := readTableData("cluster_injected_pseudo_gtid", &snapshotData.InjectedPseudoGTIDClusters); err != nil {
		return nil, err
	}

	log.Debugf("raft snapshot data created")
	return snapshotData, nil
}

type SnapshotDataCreatorApplier struct {
}

func NewSnapshotDataCreatorApplier() *SnapshotDataCreatorApplier {
	generator := &SnapshotDataCreatorApplier{}
	return generator
}

func (snapshot *SnapshotDataCreatorApplier) GetData() (data []byte, err error) {
	snapshotData, err := CreateSnapshotData()
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(snapshotData)
	if err != nil {
		return b, err
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(b); err != nil {
		return b, err
	}
	if err := zw.Close(); err != nil {
		return b, err
	}
	return buf.Bytes(), nil
}

func (snapshot *SnapshotDataCreatorApplier) Restore(rc io.ReadCloser) error {
	snapshotData := NewSnapshotData()
	zr, err := gzip.NewReader(rc)
	if err != nil {
		return err
	}
	defer zr.Close()
	if err := json.NewDecoder(zr).Decode(&snapshotData); err != nil {
		return err
	}

	orcraft.LeaderURI.Set(snapshotData.LeaderURI)
	// keys
	{
		snapshotInstanceKeyMap := instmodel.NewInstanceKeyMap()
		snapshotInstanceKeyMap.AddKeys(snapshotData.Keys)
		for _, minimalInstance := range snapshotData.MinimalInstances {
			snapshotInstanceKeyMap.AddKey(minimalInstance.Key)
		}

		discardedKeys := 0
		// Forget instances that were not in snapshot
		existingKeys, err := instinventory.ReadAllInstanceKeys()
		if err != nil {
			return err
		}
		for _, existingKey := range existingKeys {
			if !snapshotInstanceKeyMap.HasKey(existingKey) {
				if err := instinventory.ForgetInstance(&existingKey, instdiscovery.DeadInstancesFilter.UnregisterInstance); err != nil {
					return err
				}
				discardedKeys++
			}
		}
		log.Debugf("raft snapshot restore: discarded %+v keys", discardedKeys)
		existingKeysMap := instmodel.NewInstanceKeyMap()
		existingKeysMap.AddKeys(existingKeys)

		// Discover instances that are in snapshot and not in our own database.
		// Instances that _are_ in our own database will self-discover. No need
		// to explicitly discover them.
		discoveredKeys := 0
		// v2: read keys + master keys
		for _, minimalInstance := range snapshotData.MinimalInstances {
			if !existingKeysMap.HasKey(minimalInstance.Key) {
				if err := instinventory.WriteInstance(minimalInstance.ToInstance(), false, nil); err == nil {
					discoveredKeys++
				} else {
					return err
				}
			}
		}
		if len(snapshotData.MinimalInstances) == 0 {
			// v1: read keys (backwards support)
			for _, snapshotKey := range snapshotData.Keys {
				if !existingKeysMap.HasKey(snapshotKey) {
					snapshotKey := snapshotKey
					go func() {
						discovery.QueueSnapshotKey(snapshotKey)
					}()
					discoveredKeys++
				}
			}
		}
		log.Debugf("raft snapshot restore: discovered %+v keys", discoveredKeys)
	}
	if err := writeTableData("cluster_alias", &snapshotData.ClusterAlias); err != nil {
		return err
	}
	if err := writeTableData("cluster_alias_override", &snapshotData.ClusterAliasOverride); err != nil {
		return err
	}
	if err := writeTableData("cluster_domain_name", &snapshotData.ClusterDomainName); err != nil {
		return err
	}
	if err := writeTableData("access_token", &snapshotData.AccessToken); err != nil {
		return err
	}
	if err := writeTableData("host_attributes", &snapshotData.HostAttributes); err != nil {
		return err
	}
	if err := writeTableData("database_instance_tags", &snapshotData.InstanceTags); err != nil {
		return err
	}
	if err := writeTableData("database_instance_pool", &snapshotData.PoolInstances); err != nil {
		return err
	}
	if err := writeTableData("hostname_resolve", &snapshotData.HostnameResolves); err != nil {
		return err
	}
	if err := writeTableData("hostname_unresolve", &snapshotData.HostnameUnresolves); err != nil {
		return err
	}
	if err := writeTableData("database_instance_downtime", &snapshotData.DowntimedInstances); err != nil {
		return err
	}
	if err := writeTableData("candidate_database_instance", &snapshotData.Candidates); err != nil {
		return err
	}
	if err := writeTableData("kv_store", &snapshotData.KVStore); err != nil {
		return err
	}
	if err := writeTableData("topology_recovery", &snapshotData.Recovery); err != nil {
		return err
	}
	if err := writeTableData("topology_failure_detection", &snapshotData.Detections); err != nil {
		return err
	}
	if err := writeTableData("topology_recovery_steps", &snapshotData.RecoverySteps); err != nil {
		return err
	}
	if err := writeTableData("recovery_policy", &snapshotData.RecoveryPolicy); err != nil {
		return err
	}
	if err := writeTableData("recovery_hook_profile", &snapshotData.RecoveryHookProfiles); err != nil {
		return err
	}
	if err := writeTableData("recovery_hook_assignment", &snapshotData.RecoveryHookAssignments); err != nil {
		return err
	}
	recoverypolicy.Invalidate()
	if err := writeTableData("cluster_injected_pseudo_gtid", &snapshotData.InjectedPseudoGTIDClusters); err != nil {
		return err
	}

	// recovery disable
	{
		if err := recovery.SetRecoveryDisabled(snapshotData.RecoveryDisabled); err != nil {
			return err
		}
	}
	log.Debugf("raft snapshot restore applied")
	return nil
}
