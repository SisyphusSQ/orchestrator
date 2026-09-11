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

package http

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/openark/orchestrator/internal/config"
	httpagent "github.com/openark/orchestrator/internal/http/agent"
	clusterapi "github.com/openark/orchestrator/internal/http/api/cluster"
	instanceapi "github.com/openark/orchestrator/internal/http/api/instance"
	maintenanceapi "github.com/openark/orchestrator/internal/http/api/maintenance"
	recoveryapi "github.com/openark/orchestrator/internal/http/api/recovery"
	systemapi "github.com/openark/orchestrator/internal/http/api/system"
	topologyapi "github.com/openark/orchestrator/internal/http/api/topology"
	httpcli "github.com/openark/orchestrator/internal/http/cli"
	"github.com/openark/orchestrator/internal/http/presenter"
	httpraft "github.com/openark/orchestrator/internal/http/raft"
	"github.com/openark/orchestrator/internal/http/transport"
)

var apiSynonyms = map[string]string{
	"relocate-slaves":            "relocate-replicas",
	"regroup-slaves":             "regroup-replicas",
	"move-up-slaves":             "move-up-replicas",
	"repoint-slaves":             "repoint-replicas",
	"enslave-siblings":           "take-siblings",
	"enslave-master":             "take-master",
	"regroup-slaves-bls":         "regroup-replicas-bls",
	"move-slaves-gtid":           "move-replicas-gtid",
	"regroup-slaves-gtid":        "regroup-replicas-gtid",
	"match-slaves":               "match-replicas",
	"match-up-slaves":            "match-up-replicas",
	"regroup-slaves-pgtid":       "regroup-replicas-pgtid",
	"detach-slave":               "detach-replica",
	"reattach-slave":             "reattach-replica",
	"detach-slave-master-host":   "detach-replica-master-host",
	"reattach-slave-master-host": "reattach-replica-master-host",
	"cluster-osc-slaves":         "cluster-osc-replicas",
	"start-slave":                "start-replica",
	"restart-slave":              "restart-replica",
	"stop-slave":                 "stop-replica",
	"stop-slave-nice":            "stop-replica-nice",
	"reset-slave":                "reset-replica",
	"restart-slave-statements":   "restart-replica-statements",
	"enable-semi-sync-master":    "enable-semi-sync-source",
	"disable-semi-sync-master":   "disable-semi-sync-source",
}

var registeredPaths []string

// Routes composes capability handlers into the stable HTTP route surface.
type Routes struct {
	URLPrefix string
}

func (api *Routes) getSynonymPath(path string) (synonymPath string) {
	pathBase, _, _ := strings.Cut(path, "/")
	if synonym, ok := apiSynonyms[pathBase]; ok {
		synonymPath = fmt.Sprintf("%s%s", synonym, path[len(pathBase):])
	}
	return synonymPath
}

func (api *Routes) registerSingleAPIRequest(m *transport.Router, path string, handler transport.Handler, allowProxy bool) {
	registeredPaths = append(registeredPaths, path)
	fullPath := fmt.Sprintf("%s/api/%s", api.URLPrefix, path)

	if allowProxy {
		m.Get(fullPath, httpraft.ReverseProxy, handler)
	} else {
		m.Get(fullPath, handler)
	}
	if isWebAction(path) {
		handlers := []transport.Handler{guardWebAction, handler}
		if allowProxy {
			handlers = []transport.Handler{guardWebAction, httpraft.ReverseProxy, handler}
		}
		m.Post(fullPath, handlers...)
	}
}

func (api *Routes) registerAPIRequestInternal(m *transport.Router, path string, handler transport.Handler, allowProxy bool) {
	api.registerSingleAPIRequest(m, path, handler, allowProxy)

	if synonym := api.getSynonymPath(path); synonym != "" {
		api.registerSingleAPIRequest(m, synonym, handler, allowProxy)
	}
}

func (api *Routes) registerAPIRequest(m *transport.Router, path string, handler transport.Handler) {
	api.registerAPIRequestInternal(m, path, handler, true)
}

func (api *Routes) registerAPIRequestNoProxy(m *transport.Router, path string, handler transport.Handler) {
	api.registerAPIRequestInternal(m, path, handler, false)
}

func (api *Routes) registerAPIMethod(m *transport.Router, method, path string, handler transport.Handler, allowProxy bool) {
	registeredPaths = append(registeredPaths, path)
	fullPath := fmt.Sprintf("%s/api/%s", api.URLPrefix, path)
	handlers := []transport.Handler{handler}
	if method != http.MethodGet {
		handlers = []transport.Handler{guardWebAction, handler}
	}
	if allowProxy {
		handlers = append(handlers[:len(handlers)-1], httpraft.ReverseProxy, handler)
	}
	switch method {
	case http.MethodGet:
		m.Get(fullPath, handlers...)
	case http.MethodPost:
		m.Post(fullPath, handlers...)
	case http.MethodDelete:
		m.Delete(fullPath, handlers...)
	default:
		panic(fmt.Sprintf("unsupported API method %s", method))
	}
}

// RegisterRequests makes for the de-facto list of known API calls
func (api *Routes) RegisterRequests(m *transport.Router) {
	agentAPI := httpagent.ManagementAPI{}
	clusterAPI := clusterapi.API{}
	instanceAPI := instanceapi.API{}
	maintenanceAPI := maintenanceapi.API{}
	raftAPI := httpraft.API{}
	recoveryAPI := recoveryapi.API{}
	systemAPI := systemapi.API{}
	topologyAPI := topologyapi.API{}
	httpcli.Register(func(path string, handler transport.Handler) {
		api.registerAPIRequest(m, path, handler)
	})
	// Smart relocation:
	api.registerAPIRequest(m, "relocate/:host/:port/:belowHost/:belowPort", topologyAPI.RelocateBelow)
	api.registerAPIRequest(m, "relocate-below/:host/:port/:belowHost/:belowPort", topologyAPI.RelocateBelow)
	api.registerAPIRequest(m, "relocate-slaves/:host/:port/:belowHost/:belowPort", topologyAPI.RelocateReplicas)
	api.registerAPIRequest(m, "regroup-slaves/:host/:port", topologyAPI.RegroupReplicas)

	// Classic file:pos relocation:
	api.registerAPIRequest(m, "move-up/:host/:port", topologyAPI.MoveUp)
	api.registerAPIRequest(m, "move-up-slaves/:host/:port", topologyAPI.MoveUpReplicas)
	api.registerAPIRequest(m, "move-below/:host/:port/:siblingHost/:siblingPort", topologyAPI.MoveBelow)
	api.registerAPIRequest(m, "move-equivalent/:host/:port/:belowHost/:belowPort", topologyAPI.MoveEquivalent)
	api.registerAPIRequest(m, "repoint/:host/:port", topologyAPI.Repoint)
	api.registerAPIRequest(m, "repoint/:host/:port/:belowHost/:belowPort", topologyAPI.Repoint)
	api.registerAPIRequest(m, "repoint-slaves/:host/:port", topologyAPI.RepointReplicas)
	api.registerAPIRequest(m, "make-co-master/:host/:port", topologyAPI.MakeCoMaster)
	api.registerAPIRequest(m, "enslave-siblings/:host/:port", topologyAPI.TakeSiblings)
	api.registerAPIRequest(m, "enslave-master/:host/:port", topologyAPI.TakeMaster)
	api.registerAPIRequest(m, "master-equivalent/:host/:port/:logFile/:logPos", topologyAPI.MasterEquivalent)

	// Binlog server relocation:
	api.registerAPIRequest(m, "regroup-slaves-bls/:host/:port", topologyAPI.RegroupReplicasBinlogServers)

	// GTID relocation:
	api.registerAPIRequest(m, "move-below-gtid/:host/:port/:belowHost/:belowPort", topologyAPI.MoveBelowGTID)
	api.registerAPIRequest(m, "move-slaves-gtid/:host/:port/:belowHost/:belowPort", topologyAPI.MoveReplicasGTID)
	api.registerAPIRequest(m, "regroup-slaves-gtid/:host/:port", topologyAPI.RegroupReplicasGTID)

	// Pseudo-GTID relocation:
	api.registerAPIRequest(m, "match/:host/:port/:belowHost/:belowPort", topologyAPI.MatchBelow)
	api.registerAPIRequest(m, "match-below/:host/:port/:belowHost/:belowPort", topologyAPI.MatchBelow)
	api.registerAPIRequest(m, "match-up/:host/:port", topologyAPI.MatchUp)
	api.registerAPIRequest(m, "match-slaves/:host/:port/:belowHost/:belowPort", topologyAPI.MultiMatchReplicas)
	api.registerAPIRequest(m, "match-up-slaves/:host/:port", topologyAPI.MatchUpReplicas)
	api.registerAPIRequest(m, "regroup-slaves-pgtid/:host/:port", topologyAPI.RegroupReplicasPseudoGTID)
	// Legacy, need to revisit:
	api.registerAPIRequest(m, "make-master/:host/:port", topologyAPI.MakeMaster)
	api.registerAPIRequest(m, "make-local-master/:host/:port", topologyAPI.MakeLocalMaster)

	// Replication, general:
	api.registerAPIRequest(m, "enable-gtid/:host/:port", topologyAPI.EnableGTID)
	api.registerAPIRequest(m, "disable-gtid/:host/:port", topologyAPI.DisableGTID)
	api.registerAPIRequest(m, "locate-gtid-errant/:host/:port", topologyAPI.LocateErrantGTID)
	api.registerAPIRequest(m, "gtid-errant-reset-master/:host/:port", topologyAPI.ErrantGTIDResetMaster)
	api.registerAPIRequest(m, "gtid-errant-inject-empty/:host/:port", topologyAPI.ErrantGTIDInjectEmpty)
	api.registerAPIRequest(m, "skip-query/:host/:port", topologyAPI.SkipQuery)
	api.registerAPIRequest(m, "start-slave/:host/:port", topologyAPI.StartReplication)
	api.registerAPIRequest(m, "restart-slave/:host/:port", topologyAPI.RestartReplication)
	api.registerAPIRequest(m, "stop-slave/:host/:port", topologyAPI.StopReplication)
	api.registerAPIRequest(m, "stop-slave-nice/:host/:port", topologyAPI.StopReplicationNicely)
	api.registerAPIRequest(m, "reset-slave/:host/:port", topologyAPI.ResetReplication)
	api.registerAPIRequest(m, "change-master-credentials/:host/:port", topologyAPI.ChangeMasterCredentials)
	api.registerAPIRequest(m, "detach-slave/:host/:port", topologyAPI.DetachReplicaMasterHost)
	api.registerAPIRequest(m, "reattach-slave/:host/:port", topologyAPI.ReattachReplicaMasterHost)
	api.registerAPIRequest(m, "detach-slave-master-host/:host/:port", topologyAPI.DetachReplicaMasterHost)
	api.registerAPIRequest(m, "reattach-slave-master-host/:host/:port", topologyAPI.ReattachReplicaMasterHost)
	api.registerAPIRequest(m, "flush-binary-logs/:host/:port", topologyAPI.FlushBinaryLogs)
	api.registerAPIRequest(m, "purge-binary-logs/:host/:port/:logFile", topologyAPI.PurgeBinaryLogs)
	api.registerAPIRequest(m, "restart-slave-statements/:host/:port", topologyAPI.RestartReplicationStatements)
	api.registerAPIRequest(m, "enable-semi-sync-master/:host/:port", topologyAPI.EnableSemiSyncMaster)
	api.registerAPIRequest(m, "disable-semi-sync-master/:host/:port", topologyAPI.DisableSemiSyncMaster)
	api.registerAPIRequest(m, "enable-semi-sync-replica/:host/:port", topologyAPI.EnableSemiSyncReplica)
	api.registerAPIRequest(m, "disable-semi-sync-replica/:host/:port", topologyAPI.DisableSemiSyncReplica)
	api.registerAPIRequest(m, "delay-replication/:host/:port/:seconds", topologyAPI.DelayReplication)

	// Replication information:
	api.registerAPIRequest(m, "can-replicate-from/:host/:port/:belowHost/:belowPort", topologyAPI.CanReplicateFrom)
	api.registerAPIRequest(m, "can-replicate-from-gtid/:host/:port/:belowHost/:belowPort", topologyAPI.CanReplicateFromGTID)

	// Instance:
	api.registerAPIRequest(m, "set-read-only/:host/:port", topologyAPI.SetReadOnly)
	api.registerAPIRequest(m, "set-writeable/:host/:port", topologyAPI.SetWriteable)
	api.registerAPIRequest(m, "kill-query/:host/:port/:process", topologyAPI.KillQuery)

	// Binary logs:
	api.registerAPIRequest(m, "last-pseudo-gtid/:host/:port", topologyAPI.LastPseudoGTID)

	// Pools:
	api.registerAPIRequest(m, "submit-pool-instances/:pool", clusterAPI.SubmitPoolInstances)
	api.registerAPIRequest(m, "cluster-pool-instances/:clusterName", clusterAPI.ReadClusterPoolInstancesMap)
	api.registerAPIRequest(m, "cluster-pool-instances/:clusterName/:pool", clusterAPI.ReadClusterPoolInstancesMap)
	api.registerAPIRequest(m, "heuristic-cluster-pool-instances/:clusterName", clusterAPI.GetHeuristicClusterPoolInstances)
	api.registerAPIRequest(m, "heuristic-cluster-pool-instances/:clusterName/:pool", clusterAPI.GetHeuristicClusterPoolInstances)
	api.registerAPIRequest(m, "heuristic-cluster-pool-lag/:clusterName", clusterAPI.GetHeuristicClusterPoolInstancesLag)
	api.registerAPIRequest(m, "heuristic-cluster-pool-lag/:clusterName/:pool", clusterAPI.GetHeuristicClusterPoolInstancesLag)

	// Information:
	api.registerAPIRequest(m, "search/:searchString", clusterAPI.Search)
	api.registerAPIRequest(m, "search", clusterAPI.Search)

	// Cluster
	api.registerAPIRequest(m, "cluster/:clusterHint", clusterAPI.Cluster)
	api.registerAPIRequest(m, "cluster/alias/:clusterAlias", clusterAPI.ClusterByAlias)
	api.registerAPIRequest(m, "cluster/instance/:host/:port", clusterAPI.ClusterByInstance)
	api.registerAPIRequest(m, "cluster-info/:clusterHint", clusterAPI.ClusterInfo)
	api.registerAPIRequest(m, "cluster-info/alias/:clusterAlias", clusterAPI.ClusterInfoByAlias)
	api.registerAPIRequest(m, "cluster-osc-slaves/:clusterHint", clusterAPI.ClusterOSCReplicas)
	api.registerAPIRequest(m, "set-cluster-alias/:clusterName", clusterAPI.SetClusterAliasManualOverride)
	api.registerAPIRequest(m, "clusters", clusterAPI.Clusters)
	api.registerAPIRequest(m, "clusters-info", clusterAPI.ClustersInfo)

	api.registerAPIRequest(m, "masters", clusterAPI.Masters)
	api.registerAPIRequest(m, "master/:clusterHint", clusterAPI.ClusterMaster)
	api.registerAPIRequest(m, "instance-replicas/:host/:port", instanceAPI.Replicas)
	api.registerAPIRequest(m, "all-instances", clusterAPI.AllInstances)
	api.registerAPIRequest(m, "downtimed", clusterAPI.Downtimed)
	api.registerAPIRequest(m, "downtimed/:clusterHint", clusterAPI.Downtimed)
	api.registerAPIRequest(m, "topology/:clusterHint", clusterAPI.AsciiTopology)
	api.registerAPIRequest(m, "topology/:host/:port", clusterAPI.AsciiTopology)
	api.registerAPIRequest(m, "topology-tabulated/:clusterHint", clusterAPI.AsciiTopologyTabulated)
	api.registerAPIRequest(m, "topology-tabulated/:host/:port", clusterAPI.AsciiTopologyTabulated)
	api.registerAPIRequest(m, "topology-tags/:clusterHint", clusterAPI.AsciiTopologyTags)
	api.registerAPIRequest(m, "topology-tags/:host/:port", clusterAPI.AsciiTopologyTags)
	api.registerAPIRequest(m, "snapshot-topologies", clusterAPI.SnapshotTopologies)

	// Key-value:
	api.registerAPIRequest(m, "submit-masters-to-kv-stores", clusterAPI.SubmitMastersToKvStores)
	api.registerAPIRequest(m, "submit-masters-to-kv-stores/:clusterHint", clusterAPI.SubmitMastersToKvStores)

	// Tags:
	api.registerAPIRequest(m, "tagged", clusterAPI.Tagged)
	api.registerAPIRequest(m, "tags/:host/:port", clusterAPI.Tags)
	api.registerAPIRequest(m, "tag-value/:host/:port", clusterAPI.TagValue)
	api.registerAPIRequest(m, "tag-value/:host/:port/:tagName", clusterAPI.TagValue)
	api.registerAPIRequest(m, "tag/:host/:port", clusterAPI.Tag)
	api.registerAPIRequest(m, "tag/:host/:port/:tagName/:tagValue", clusterAPI.Tag)
	api.registerAPIRequest(m, "untag/:host/:port", clusterAPI.Untag)
	api.registerAPIRequest(m, "untag/:host/:port/:tagName", clusterAPI.Untag)
	api.registerAPIRequest(m, "untag-all", clusterAPI.UntagAll)
	api.registerAPIRequest(m, "untag-all/:tagName/:tagValue", clusterAPI.UntagAll)

	// Instance management:
	api.registerAPIRequest(m, "instance/:host/:port", instanceAPI.Read)
	api.registerAPIRequest(m, "discover/:host/:port", instanceAPI.Discover)
	api.registerAPIRequest(m, "async-discover/:host/:port", instanceAPI.AsyncDiscover)
	api.registerAPIRequest(m, "refresh/:host/:port", instanceAPI.Refresh)
	api.registerAPIRequest(m, "forget/:host/:port", instanceAPI.Forget)
	api.registerAPIRequest(m, "forget-cluster/:clusterHint", instanceAPI.ForgetCluster)
	api.registerAPIRequest(m, "begin-maintenance/:host/:port/:owner/:reason", maintenanceAPI.Begin)
	api.registerAPIRequest(m, "end-maintenance/:host/:port", maintenanceAPI.EndByInstance)
	api.registerAPIRequest(m, "in-maintenance/:host/:port", maintenanceAPI.InMaintenance)
	api.registerAPIRequest(m, "end-maintenance/:maintenanceKey", maintenanceAPI.End)
	api.registerAPIRequest(m, "maintenance", maintenanceAPI.List)
	api.registerAPIRequest(m, "begin-downtime/:host/:port/:owner/:reason", maintenanceAPI.BeginDowntime)
	api.registerAPIRequest(m, "begin-downtime/:host/:port/:owner/:reason/:duration", maintenanceAPI.BeginDowntime)
	api.registerAPIRequest(m, "end-downtime/:host/:port", maintenanceAPI.EndDowntime)

	// Recovery:
	api.registerAPIMethod(m, http.MethodGet, "recovery-policy/:scopeType/:scopeKey", recoveryAPI.RecoveryPolicy, true)
	api.registerAPIMethod(m, http.MethodPost, "recovery-policy", recoveryAPI.SaveRecoveryPolicy, true)
	api.registerAPIMethod(m, http.MethodGet, "recovery-hook-profiles", recoveryAPI.RecoveryHookProfiles, true)
	api.registerAPIMethod(m, http.MethodPost, "recovery-hook-profiles", recoveryAPI.SaveRecoveryHookProfile, true)
	api.registerAPIMethod(m, http.MethodPost, "recovery-hook-test", recoveryAPI.TestRecoveryHookProfile, true)
	api.registerAPIMethod(m, http.MethodGet, "recovery-hook-assignments/:scopeType/:scopeKey", recoveryAPI.RecoveryHookAssignments, true)
	api.registerAPIMethod(m, http.MethodPost, "recovery-hook-assignments", recoveryAPI.SaveRecoveryHookAssignment, true)
	api.registerAPIRequest(m, "replication-analysis", recoveryAPI.ReplicationAnalysis)
	api.registerAPIRequest(m, "replication-analysis/:clusterName", recoveryAPI.ReplicationAnalysisForCluster)
	api.registerAPIRequest(m, "replication-analysis/instance/:host/:port", recoveryAPI.ReplicationAnalysisForKey)
	api.registerAPIRequest(m, "recover/:host/:port", recoveryAPI.Recover)
	api.registerAPIRequest(m, "recover/:host/:port/:candidateHost/:candidatePort", recoveryAPI.Recover)
	api.registerAPIRequest(m, "recover-lite/:host/:port", recoveryAPI.RecoverLite)
	api.registerAPIRequest(m, "recover-lite/:host/:port/:candidateHost/:candidatePort", recoveryAPI.RecoverLite)
	api.registerAPIRequest(m, "graceful-master-takeover/:host/:port", recoveryAPI.GracefulMasterTakeover)
	api.registerAPIRequest(m, "graceful-master-takeover/:host/:port/:designatedHost/:designatedPort", recoveryAPI.GracefulMasterTakeover)
	api.registerAPIRequest(m, "graceful-master-takeover/:clusterHint", recoveryAPI.GracefulMasterTakeover)
	api.registerAPIRequest(m, "graceful-master-takeover/:clusterHint/:designatedHost/:designatedPort", recoveryAPI.GracefulMasterTakeover)
	api.registerAPIRequest(m, "graceful-master-takeover-auto/:host/:port", recoveryAPI.GracefulMasterTakeoverAuto)
	api.registerAPIRequest(m, "graceful-master-takeover-auto/:host/:port/:designatedHost/:designatedPort", recoveryAPI.GracefulMasterTakeoverAuto)
	api.registerAPIRequest(m, "graceful-master-takeover-auto/:clusterHint", recoveryAPI.GracefulMasterTakeoverAuto)
	api.registerAPIRequest(m, "graceful-master-takeover-auto/:clusterHint/:designatedHost/:designatedPort", recoveryAPI.GracefulMasterTakeoverAuto)
	api.registerAPIRequest(m, "force-master-failover/:host/:port", recoveryAPI.ForceMasterFailover)
	api.registerAPIRequest(m, "force-master-failover/:clusterHint", recoveryAPI.ForceMasterFailover)
	api.registerAPIRequest(m, "force-master-takeover/:clusterHint/:designatedHost/:designatedPort", recoveryAPI.ForceMasterTakeover)
	api.registerAPIRequest(m, "force-master-takeover/:host/:port/:designatedHost/:designatedPort", recoveryAPI.ForceMasterTakeover)
	api.registerAPIRequest(m, "register-candidate/:host/:port/:promotionRule", recoveryAPI.RegisterCandidate)
	api.registerAPIRequest(m, "automated-recovery-filters", recoveryAPI.AutomatedRecoveryFilters)
	api.registerAPIRequest(m, "audit-failure-detection", recoveryAPI.AuditFailureDetection)
	api.registerAPIRequest(m, "audit-failure-detection/:page", recoveryAPI.AuditFailureDetection)
	api.registerAPIRequest(m, "audit-failure-detection/id/:id", recoveryAPI.AuditFailureDetection)
	api.registerAPIRequest(m, "audit-failure-detection/alias/:clusterAlias", recoveryAPI.AuditFailureDetection)
	api.registerAPIRequest(m, "audit-failure-detection/alias/:clusterAlias/:page", recoveryAPI.AuditFailureDetection)
	api.registerAPIRequest(m, "replication-analysis-changelog", recoveryAPI.ReadReplicationAnalysisChangelog)
	api.registerAPIRequest(m, "audit-recovery", recoveryAPI.AuditRecovery)
	api.registerAPIRequest(m, "audit-recovery/:page", recoveryAPI.AuditRecovery)
	api.registerAPIRequest(m, "audit-recovery/id/:id", recoveryAPI.AuditRecovery)
	api.registerAPIRequest(m, "audit-recovery/uid/:uid", recoveryAPI.AuditRecovery)
	api.registerAPIRequest(m, "audit-recovery/cluster/:clusterName", recoveryAPI.AuditRecovery)
	api.registerAPIRequest(m, "audit-recovery/cluster/:clusterName/:page", recoveryAPI.AuditRecovery)
	api.registerAPIRequest(m, "audit-recovery/alias/:clusterAlias", recoveryAPI.AuditRecovery)
	api.registerAPIRequest(m, "audit-recovery/alias/:clusterAlias/:page", recoveryAPI.AuditRecovery)
	api.registerAPIRequest(m, "audit-recovery-steps/:uid", recoveryAPI.AuditRecoverySteps)
	api.registerAPIRequest(m, "active-cluster-recovery/:clusterName", recoveryAPI.ActiveClusterRecovery)
	api.registerAPIRequest(m, "recently-active-cluster-recovery/:clusterName", recoveryAPI.RecentlyActiveClusterRecovery)
	api.registerAPIRequest(m, "recently-active-instance-recovery/:host/:port", recoveryAPI.RecentlyActiveInstanceRecovery)
	api.registerAPIRequest(m, "ack-recovery/cluster/:clusterHint", recoveryAPI.AcknowledgeClusterRecoveries)
	api.registerAPIRequest(m, "ack-recovery/cluster/alias/:clusterAlias", recoveryAPI.AcknowledgeClusterRecoveries)
	api.registerAPIRequest(m, "ack-recovery/instance/:host/:port", recoveryAPI.AcknowledgeInstanceRecoveries)
	api.registerAPIRequest(m, "ack-recovery/:recoveryId", recoveryAPI.AcknowledgeRecovery)
	api.registerAPIRequest(m, "ack-recovery/uid/:uid", recoveryAPI.AcknowledgeRecovery)
	api.registerAPIRequest(m, "ack-all-recoveries", recoveryAPI.AcknowledgeAllRecoveries)
	api.registerAPIRequest(m, "blocked-recoveries", recoveryAPI.BlockedRecoveries)
	api.registerAPIRequest(m, "blocked-recoveries/cluster/:clusterName", recoveryAPI.BlockedRecoveries)
	api.registerAPIRequest(m, "disable-global-recoveries", recoveryAPI.DisableGlobalRecoveries)
	api.registerAPIRequest(m, "enable-global-recoveries", recoveryAPI.EnableGlobalRecoveries)
	api.registerAPIRequest(m, "check-global-recoveries", recoveryAPI.CheckGlobalRecoveries)

	// General
	api.registerAPIRequest(m, "problems", clusterAPI.Problems)
	api.registerAPIRequest(m, "problems/:clusterName", clusterAPI.Problems)
	api.registerAPIRequest(m, "audit", clusterAPI.Audit)
	api.registerAPIRequest(m, "audit/:page", clusterAPI.Audit)
	api.registerAPIRequest(m, "audit/instance/:host/:port", clusterAPI.Audit)
	api.registerAPIRequest(m, "audit/instance/:host/:port/:page", clusterAPI.Audit)
	api.registerAPIRequest(m, "resolve/:host/:port", instanceAPI.Resolve)

	// Meta, no proxy
	api.registerAPIRequestNoProxy(m, "headers", systemAPI.Headers)
	api.registerAPIRequestNoProxy(m, "health", systemAPI.Health)
	api.registerAPIRequestNoProxy(m, "lb-check", systemAPI.LBCheck)
	api.registerAPIRequestNoProxy(m, "_ping", systemAPI.LBCheck)
	api.registerAPIRequestNoProxy(m, "leader-check", systemAPI.LeaderCheck)
	api.registerAPIRequestNoProxy(m, "leader-check/:errorStatusCode", systemAPI.LeaderCheck)
	api.registerAPIRequestNoProxy(m, "raft/configuration", raftAPI.Configuration)
	api.registerAPIMethod(m, http.MethodPost, "raft/bootstrap", raftAPI.Bootstrap, false)
	api.registerAPIMethod(m, http.MethodPost, "raft/members", raftAPI.AddMember, true)
	api.registerAPIMethod(m, http.MethodDelete, "raft/members/:id", raftAPI.RemoveMember, true)
	api.registerAPIMethod(m, http.MethodPost, "raft/leadership/transfer", raftAPI.LeadershipTransfer, true)
	api.registerAPIMethod(m, http.MethodPost, "raft/snapshot", raftAPI.Snapshot, false)
	api.registerAPIRequestNoProxy(m, "raft-state", raftAPI.State)
	api.registerAPIRequestNoProxy(m, "raft-leader", raftAPI.Leader)
	api.registerAPIRequestNoProxy(m, "raft-health", raftAPI.Health)
	api.registerAPIRequestNoProxy(m, "raft-status", raftAPI.Status)
	api.registerAPIRequestNoProxy(m, "reload-configuration", systemAPI.ReloadConfiguration)
	api.registerAPIRequestNoProxy(m, "hostname-resolve-cache", clusterAPI.HostnameResolveCache)
	api.registerAPIRequestNoProxy(m, "reset-hostname-resolve-cache", clusterAPI.ResetHostnameResolveCache)
	// Meta
	api.registerAPIRequest(m, "routed-leader-check", systemAPI.LeaderCheck)
	api.registerAPIRequest(m, "reload-cluster-alias", clusterAPI.ReloadClusterAlias)
	api.registerAPIRequest(m, "deregister-hostname-unresolve/:host/:port", clusterAPI.DeregisterHostnameUnresolve)
	api.registerAPIRequest(m, "register-hostname-unresolve/:host/:port/:virtualname", clusterAPI.RegisterHostnameUnresolve)

	// Bulk access to information
	api.registerAPIRequest(m, "bulk-instances", clusterAPI.BulkInstances)
	api.registerAPIRequest(m, "bulk-promotion-rules", clusterAPI.BulkPromotionRules)

	// Monitoring

	// Agents
	api.registerAPIRequest(m, "agents", agentAPI.Agents)
	api.registerAPIRequest(m, "agent/:host", agentAPI.Agent)
	api.registerAPIRequest(m, "agent-umount/:host", agentAPI.Unmount)
	api.registerAPIRequest(m, "agent-mount/:host", agentAPI.MountLV)
	api.registerAPIRequest(m, "agent-create-snapshot/:host", agentAPI.CreateSnapshot)
	api.registerAPIRequest(m, "agent-removelv/:host", agentAPI.RemoveLV)
	api.registerAPIRequest(m, "agent-mysql-stop/:host", agentAPI.MySQLStop)
	api.registerAPIRequest(m, "agent-mysql-start/:host", agentAPI.MySQLStart)
	api.registerAPIRequest(m, "agent-seed/:targetHost/:sourceHost", agentAPI.Seed)
	api.registerAPIRequest(m, "agent-active-seeds/:host", agentAPI.ActiveSeeds)
	api.registerAPIRequest(m, "agent-recent-seeds/:host", agentAPI.RecentSeeds)
	api.registerAPIRequest(m, "agent-seed-details/:seedId", agentAPI.SeedDetails)
	api.registerAPIRequest(m, "agent-seed-states/:seedId", agentAPI.SeedStates)
	api.registerAPIRequest(m, "agent-abort-seed/:seedId", agentAPI.AbortSeed)
	api.registerAPIRequest(m, "agent-custom-command/:host/:command", agentAPI.CustomCommand)
	api.registerAPIRequest(m, "seeds", agentAPI.Seeds)

	// Configurable status check endpoint
	if config.Config.Server.Status.Endpoint == config.DefaultStatusAPIEndpoint {
		api.registerAPIRequestNoProxy(m, "status", systemAPI.StatusCheck)
	} else {
		m.Get(config.Config.Server.Status.Endpoint, systemAPI.StatusCheck)
	}

	presenter.SetupMessagePrefix()
}
