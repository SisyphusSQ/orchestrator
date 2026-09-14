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

// Package recovery owns failure detection, recovery orchestration and history.
package recovery

import (
	"time"

	"github.com/openark/orchestrator/internal/inst/analysis"
	instrelocation "github.com/openark/orchestrator/internal/inst/change/relocation"
	instmodel "github.com/openark/orchestrator/internal/inst/instance"
	"github.com/openark/orchestrator/internal/util"
)

type RecoveryType string

const (
	MasterRecovery                 = "MasterRecovery"
	CoMasterRecovery               = "CoMasterRecovery"
	IntermediateMasterRecovery     = "IntermediateMasterRecovery"
	ReplicationGroupMemberRecovery = "ReplicationGroupMemberRecovery"
)

type RecoveryAcknowledgement struct {
	CreatedAt time.Time
	Owner     string
	Comment   string

	Key           instmodel.InstanceKey
	ClusterName   string
	Id            int64
	UID           string
	AllRecoveries bool
}

func NewRecoveryAcknowledgement(owner string, comment string) *RecoveryAcknowledgement {
	return &RecoveryAcknowledgement{
		CreatedAt: time.Now(),
		Owner:     owner,
		Comment:   comment,
	}
}

func NewInternalAcknowledgement() *RecoveryAcknowledgement {
	return &RecoveryAcknowledgement{
		CreatedAt: time.Now(),
		Owner:     "orchestrator",
		Comment:   "internal",
	}
}

// BlockedTopologyRecovery represents an entry in the blocked_topology_recovery table
type BlockedTopologyRecovery struct {
	FailedInstanceKey    instmodel.InstanceKey
	ClusterName          string
	Analysis             analysis.AnalysisCode
	LastBlockedTimestamp string
	BlockingRecoveryId   int64
}

// TopologyRecovery represents an entry in the topology_recovery table
type TopologyRecovery struct {
	instrelocation.PostponedFunctionsContainer

	Id                         int64
	UID                        string
	AnalysisEntry              analysis.ReplicationAnalysis
	SuccessorKey               *instmodel.InstanceKey
	SuccessorAlias             string
	SuccessorBinlogCoordinates *instmodel.BinlogCoordinates
	IsActive                   bool
	IsSuccessful               bool
	LostReplicas               instmodel.InstanceKeyMap
	ParticipatingInstanceKeys  instmodel.InstanceKeyMap
	AllErrors                  []string
	RecoveryStartTimestamp     string
	RecoveryEndTimestamp       string
	ProcessingNodeHostname     string
	ProcessingNodeToken        string
	PolicyRevision             int64
	HookAssignmentRevision     int64
	Acknowledged               bool
	AcknowledgedAt             string
	AcknowledgedBy             string
	AcknowledgedComment        string
	LastDetectionId            int64
	RelatedRecoveryId          int64
	Type                       RecoveryType
	RecoveryType               MasterRecoveryType
}

func NewTopologyRecovery(replicationAnalysis analysis.ReplicationAnalysis) *TopologyRecovery {
	topologyRecovery := &TopologyRecovery{}
	topologyRecovery.UID = util.PrettyUniqueToken()
	topologyRecovery.AnalysisEntry = replicationAnalysis
	topologyRecovery.SuccessorKey = nil
	topologyRecovery.SuccessorBinlogCoordinates = nil
	topologyRecovery.LostReplicas = *instmodel.NewInstanceKeyMap()
	topologyRecovery.ParticipatingInstanceKeys = *instmodel.NewInstanceKeyMap()
	topologyRecovery.AllErrors = []string{}
	topologyRecovery.RecoveryType = NotMasterRecovery
	return topologyRecovery
}

func (recovery *TopologyRecovery) AddError(err error) error {
	if err != nil {
		recovery.AllErrors = append(recovery.AllErrors, err.Error())
	}
	return err
}

func (recovery *TopologyRecovery) AddErrors(errs []error) {
	for _, err := range errs {
		recovery.AddError(err)
	}
}

type TopologyRecoveryStep struct {
	Id          int64
	RecoveryUID string
	AuditAt     string
	Message     string
}

func NewTopologyRecoveryStep(uid string, message string) *TopologyRecoveryStep {
	return &TopologyRecoveryStep{
		RecoveryUID: uid,
		Message:     message,
	}
}

type MasterRecoveryType string

const (
	NotMasterRecovery          = "NotMasterRecovery"
	MasterRecoveryGTID         = "MasterRecoveryGTID"
	MasterRecoveryPseudoGTID   = "MasterRecoveryPseudoGTID"
	MasterRecoveryBinlogServer = "MasterRecoveryBinlogServer"
)

// InstancesByCountReplicas sorts instances by umber of replicas, descending
type InstancesByCountReplicas []*instmodel.Instance

func (instances InstancesByCountReplicas) Len() int { return len(instances) }

func (instances InstancesByCountReplicas) Swap(i, j int) {
	instances[i], instances[j] = instances[j], instances[i]
}

func (instances InstancesByCountReplicas) Less(i, j int) bool {
	if len(instances[i].Replicas) == len(instances[j].Replicas) {
		// Secondary sorting: prefer more advanced replicas
		return !instances[i].ExecBinlogCoordinates.SmallerThan(&instances[j].ExecBinlogCoordinates)
	}
	return len(instances[i].Replicas) < len(instances[j].Replicas)
}
