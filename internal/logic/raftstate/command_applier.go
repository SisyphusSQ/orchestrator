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

// Package raftstate adapts application commands and snapshots to the Raft runtime.
package raftstate

import (
	"context"
	"encoding/json"

	"github.com/openark/orchestrator/internal/attributes"
	"github.com/openark/orchestrator/internal/inst/analysis"
	instcandidate "github.com/openark/orchestrator/internal/inst/candidate"
	instcluster "github.com/openark/orchestrator/internal/inst/cluster"
	instdiscovery "github.com/openark/orchestrator/internal/inst/discovery"
	instdowntime "github.com/openark/orchestrator/internal/inst/downtime"
	instmodel "github.com/openark/orchestrator/internal/inst/instance"
	instinventory "github.com/openark/orchestrator/internal/inst/inventory"
	instpool "github.com/openark/orchestrator/internal/inst/pool"
	instresolve "github.com/openark/orchestrator/internal/inst/resolve"
	insttag "github.com/openark/orchestrator/internal/inst/tag"
	"github.com/openark/orchestrator/internal/kv"
	"github.com/openark/orchestrator/internal/logic/discovery"
	"github.com/openark/orchestrator/internal/logic/recovery"
	"github.com/openark/orchestrator/internal/models/domain"
	"github.com/openark/orchestrator/internal/models/dto"
	"github.com/openark/orchestrator/internal/raft"
	"github.com/openark/orchestrator/internal/recoverypolicy"

	"github.com/openark/orchestrator/internal/golib/log"
)

// AsyncRequest represents an entry in the async_request table
type CommandApplier struct {
}

func NewCommandApplier() *CommandApplier {
	applier := &CommandApplier{}
	return applier
}

func (applier *CommandApplier) ApplyCommand(op string, value []byte) any {
	switch op {
	case "set-general-attribute":
		var attribute domain.HostAttributes
		if err := json.Unmarshal(value, &attribute); err != nil {
			return err
		}
		return attributes.SetGeneralAttribute(attribute.AttributeName, attribute.AttributeValue)
	case "heartbeat":
		return nil
	case "async-snapshot":
		return applier.asyncSnapshot(value)
	case "register-node":
		return applier.registerNode(value)
	case "discover":
		return applier.discover(value)
	case "injected-pseudo-gtid":
		return applier.injectedPseudoGTID(value)
	case "forget":
		return applier.forget(value)
	case "forget-cluster":
		return applier.forgetCluster(value)
	case "begin-downtime":
		return applier.beginDowntime(value)
	case "end-downtime":
		return applier.endDowntime(value)
	case "register-candidate":
		return applier.registerCandidate(value)
	case "ack-recovery":
		return applier.ackRecovery(value)
	case "register-hostname-unresolve":
		return applier.registerHostnameUnresolve(value)
	case "submit-pool-instances":
		return applier.submitPoolInstances(value)
	case "register-failure-detection":
		return applier.registerFailureDetection(value)
	case "write-recovery":
		return applier.writeRecovery(value)
	case "write-recovery-step":
		return applier.writeRecoveryStep(value)
	case "resolve-recovery":
		return applier.resolveRecovery(value)
	case "disable-global-recoveries":
		return applier.disableGlobalRecoveries(value)
	case "enable-global-recoveries":
		return applier.enableGlobalRecoveries(value)
	case "put-key-value":
		return applier.putKeyValue(value)
	case "put-instance-tag":
		return applier.putInstanceTag(value)
	case "delete-all-instance-tags":
		tag := insttag.Tag{}
		if err := json.Unmarshal(value, &tag); err != nil {
			return err
		}
		removed, err := insttag.Untag(nil, &tag)
		if err != nil {
			return err
		}
		return removed
	case "delete-instance-tag":
		return applier.deleteInstanceTag(value)
	case "leader-uri":
		return applier.leaderURI(value)
	case "set-cluster-alias-manual-override":
		return applier.setClusterAliasManualOverride(value)
	case "save-recovery-policy":
		var command dto.SaveRecoveryPolicyCommand
		if err := json.Unmarshal(value, &command); err != nil {
			return err
		}
		return recoverypolicy.SavePolicy(context.Background(), command)
	case "save-recovery-hook-profile":
		var command dto.SaveRecoveryHookProfileCommand
		if err := json.Unmarshal(value, &command); err != nil {
			return err
		}
		return recoverypolicy.SaveHookProfile(context.Background(), command)
	case "save-recovery-hook-assignment":
		var command dto.SaveRecoveryHookAssignmentCommand
		if err := json.Unmarshal(value, &command); err != nil {
			return err
		}
		return recoverypolicy.SaveHookAssignment(context.Background(), command)
	}
	return log.Errorf("Unknown command op: %s", op)
}

func (applier *CommandApplier) asyncSnapshot(value []byte) any {
	err := orcraft.AsyncSnapshot()
	return err
}

func (applier *CommandApplier) registerNode(value []byte) any {
	return nil
}

func (applier *CommandApplier) discover(value []byte) any {
	instanceKey := instmodel.InstanceKey{}
	if err := json.Unmarshal(value, &instanceKey); err != nil {
		return log.Errore(err)
	}
	discovery.DiscoverInstance(instanceKey)
	return nil
}

func (applier *CommandApplier) injectedPseudoGTID(value []byte) any {
	var clusterName string
	if err := json.Unmarshal(value, &clusterName); err != nil {
		return log.Errore(err)
	}
	instinventory.RegisterInjectedPseudoGTID(clusterName)
	return nil
}

func (applier *CommandApplier) forget(value []byte) any {
	instanceKey := instmodel.InstanceKey{}
	if err := json.Unmarshal(value, &instanceKey); err != nil {
		return log.Errore(err)
	}
	err := instinventory.ForgetInstance(&instanceKey, instdiscovery.DeadInstancesFilter.UnregisterInstance)
	return err
}

func (applier *CommandApplier) forgetCluster(value []byte) any {
	var clusterName string
	if err := json.Unmarshal(value, &clusterName); err != nil {
		return log.Errore(err)
	}
	err := instinventory.ForgetCluster(clusterName, instdiscovery.DeadInstancesFilter.UnregisterInstance)
	return err
}

func (applier *CommandApplier) beginDowntime(value []byte) any {
	downtime := instdowntime.Downtime{}
	if err := json.Unmarshal(value, &downtime); err != nil {
		return log.Errore(err)
	}
	err := instdowntime.BeginDowntime(&downtime)
	return err
}

func (applier *CommandApplier) endDowntime(value []byte) any {
	instanceKey := instmodel.InstanceKey{}
	if err := json.Unmarshal(value, &instanceKey); err != nil {
		return log.Errore(err)
	}
	_, err := instdowntime.EndDowntime(&instanceKey)
	return err
}

func (applier *CommandApplier) registerCandidate(value []byte) any {
	candidate := instcandidate.CandidateDatabaseInstance{}
	if err := json.Unmarshal(value, &candidate); err != nil {
		return log.Errore(err)
	}
	err := instcandidate.RegisterCandidateInstance(&candidate)
	return err
}

func (applier *CommandApplier) ackRecovery(value []byte) any {
	ack := recovery.RecoveryAcknowledgement{}
	err := json.Unmarshal(value, &ack)
	if err != nil {
		return log.Errore(err)
	}
	if ack.AllRecoveries {
		_, err = recovery.AcknowledgeAllRecoveries(ack.Owner, ack.Comment)
	}
	if ack.ClusterName != "" {
		_, err = recovery.AcknowledgeClusterRecoveries(ack.ClusterName, ack.Owner, ack.Comment)
	}
	if ack.Key.IsValid() {
		_, err = recovery.AcknowledgeInstanceRecoveries(&ack.Key, ack.Owner, ack.Comment)
	}
	if ack.Id > 0 {
		_, err = recovery.AcknowledgeRecovery(ack.Id, ack.Owner, ack.Comment)
	}
	if ack.UID != "" {
		_, err = recovery.AcknowledgeRecoveryByUID(ack.UID, ack.Owner, ack.Comment)
	}
	return err
}

func (applier *CommandApplier) registerHostnameUnresolve(value []byte) any {
	registration := instresolve.HostnameRegistration{}
	if err := json.Unmarshal(value, &registration); err != nil {
		return log.Errore(err)
	}
	err := instresolve.RegisterHostnameUnresolve(&registration)
	return err
}

func (applier *CommandApplier) submitPoolInstances(value []byte) any {
	submission := instpool.PoolInstancesSubmission{}
	if err := json.Unmarshal(value, &submission); err != nil {
		return log.Errore(err)
	}
	err := instpool.ApplyPoolInstances(&submission)
	return err
}

func (applier *CommandApplier) registerFailureDetection(value []byte) any {
	analysisEntry := analysis.ReplicationAnalysis{}
	if err := json.Unmarshal(value, &analysisEntry); err != nil {
		return log.Errore(err)
	}
	_, err := recovery.AttemptFailureDetectionRegistration(&analysisEntry)
	return err
}

func (applier *CommandApplier) writeRecovery(value []byte) any {
	topologyRecovery := recovery.TopologyRecovery{}
	if err := json.Unmarshal(value, &topologyRecovery); err != nil {
		return log.Errore(err)
	}
	if err := recovery.ApplyTopologyRecovery(&topologyRecovery); err != nil {
		return err
	}
	return nil
}

func (applier *CommandApplier) writeRecoveryStep(value []byte) any {
	topologyRecoveryStep := recovery.TopologyRecoveryStep{}
	if err := json.Unmarshal(value, &topologyRecoveryStep); err != nil {
		return log.Errore(err)
	}
	err := recovery.ApplyTopologyRecoveryStep(&topologyRecoveryStep)
	return err
}

func (applier *CommandApplier) resolveRecovery(value []byte) any {
	topologyRecovery := recovery.TopologyRecovery{}
	if err := json.Unmarshal(value, &topologyRecovery); err != nil {
		return log.Errore(err)
	}
	if err := recovery.ApplyResolvedTopologyRecovery(&topologyRecovery); err != nil {
		return log.Errore(err)
	}
	return nil
}

func (applier *CommandApplier) disableGlobalRecoveries(value []byte) any {
	err := recovery.DisableRecovery()
	return err
}

func (applier *CommandApplier) enableGlobalRecoveries(value []byte) any {
	err := recovery.EnableRecovery()
	return err
}

func (applier *CommandApplier) putKeyValue(value []byte) any {
	kvPair := &kv.KVPair{}
	if err := json.Unmarshal(value, kvPair); err != nil {
		return log.Errore(err)
	}
	err := kv.PutKVPairs([]*kv.KVPair{kvPair})
	return err
}

func (applier *CommandApplier) putInstanceTag(value []byte) any {
	instanceTag := insttag.InstanceTag{}
	if err := json.Unmarshal(value, &instanceTag); err != nil {
		return log.Errore(err)
	}
	err := insttag.PutInstanceTag(&instanceTag.Key, &instanceTag.T)
	return err
}

func (applier *CommandApplier) deleteInstanceTag(value []byte) any {
	instanceTag := insttag.InstanceTag{}
	if err := json.Unmarshal(value, &instanceTag); err != nil {
		return log.Errore(err)
	}
	removed, err := insttag.Untag(&instanceTag.Key, &instanceTag.T)
	if err != nil {
		return err
	}
	return removed
}

func (applier *CommandApplier) leaderURI(value []byte) any {
	var uri string
	if err := json.Unmarshal(value, &uri); err != nil {
		return log.Errore(err)
	}
	orcraft.LeaderURI.Set(uri)
	return nil
}

func (applier *CommandApplier) setClusterAliasManualOverride(value []byte) any {
	var params [2]string
	if err := json.Unmarshal(value, &params); err != nil {
		return log.Errore(err)
	}
	clusterName, alias := params[0], params[1]
	err := instcluster.SetClusterAliasManualOverride(clusterName, alias)
	return err
}
