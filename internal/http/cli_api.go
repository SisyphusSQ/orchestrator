package http

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/openark/orchestrator/internal/inst"
	"github.com/openark/orchestrator/internal/models/domain"
	"github.com/openark/orchestrator/internal/process"
	orcraft "github.com/openark/orchestrator/internal/raft"
)

// registerCLIRequests 补齐旧本地 CLI 的诊断能力。每条路由仍走服务端鉴权和 leader 代理。
func (api *HttpAPI) registerCLIRequests(router *Router) {
	paths := []string{
		"get-candidate-replica/:host/:port", "rematch/:host/:port", "master-pos-wait/:host/:port",
		"last-executed-relay-entry/:host/:port", "correlate-relaylog-pos/:host/:port/:belowHost/:belowPort",
		"find-binlog-entry/:host/:port", "correlate-binlog-pos/:host/:port/:belowHost/:belowPort", "find",
		"which-heuristic-domain-instance/:clusterHint", "which-cluster-gh-ost-replicas/:clusterHint",
		"which-lost-in-recovery", "instance-status/:host/:port", "get-cluster-heuristic-lag/:clusterHint",
		"cluster-pool-instances", "set-heuristic-domain-instance/:clusterHint", "active-nodes", "show-resolve-hosts", "show-unresolve-hosts",
	}
	for _, path := range paths {
		name, _, _ := strings.Cut(path, "/")
		api.registerAPIRequest(router, "cli/"+path, api.cliHandler(name))
	}
}
func (api *HttpAPI) cliHandler(name string) Handler {
	return func(params Params, r Responder, req *http.Request, user Principal) {
		if !isAuthorizedForAction(req, user) {
			RespondStatus(r, http.StatusForbidden, &APIResponse{Code: ERROR, Message: "Unauthorized"})
			return
		}
		result, err := api.cliDiagnostic(name, params, req)
		if err != nil {
			if orcraft.ClassOf(err) == orcraft.ClassIndeterminate {
				respondRaft(r, err, name, result)
				return
			}
			Respond(r, &APIResponse{Code: ERROR, Message: err.Error()})
			return
		}
		Respond(r, &APIResponse{Code: OK, Message: name, Details: result})
	}
}
func (api *HttpAPI) cliDiagnostic(name string, params Params, req *http.Request) (any, error) {
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	var key inst.InstanceKey
	var err error
	if params["host"] != "" {
		key, err = api.getInstanceKey(params["host"], params["port"])
		if err != nil {
			return nil, err
		}
	}
	cluster := ""
	if params["clusterHint"] != "" {
		cluster, err = figureClusterName(params["clusterHint"])
		if err != nil {
			return nil, err
		}
	}
	switch name {
	case "cluster-pool-instances":
		instances, err := inst.ReadAllClusterPoolInstances()
		if err != nil {
			return nil, err
		}
		result := make([]*inst.ClusterPoolInstance, 0, len(instances))
		for _, instance := range instances {
			if cluster := req.URL.Query().Get("cluster"); cluster != "" && instance.ClusterName != cluster {
				continue
			}
			if pool := req.URL.Query().Get("pool"); pool != "" && instance.Pool != pool {
				continue
			}
			result = append(result, instance)
		}
		return result, nil
	case "find":
		pattern := req.URL.Query().Get("pattern")
		if pattern == "" {
			return nil, fmt.Errorf("pattern is required")
		}
		if _, err := regexp.Compile(pattern); err != nil {
			return nil, fmt.Errorf("invalid pattern: %w", err)
		}
		return inst.FindInstances(pattern)
	case "active-nodes":
		return process.ReadAvailableNodes(false)
	case "show-resolve-hosts":
		return inst.ReadAllHostnameResolves()
	case "show-unresolve-hosts":
		return inst.ReadAllHostnameUnresolves()
	case "which-lost-in-recovery":
		return inst.ReadLostInRecoveryInstances("")
	case "which-cluster-gh-ost-replicas":
		return inst.GetClusterGhostReplicas(cluster)
	case "get-cluster-heuristic-lag":
		return inst.GetClusterHeuristicLag(cluster)
	case "which-heuristic-domain-instance":
		return inst.GetHeuristicClusterDomainInstanceAttribute(cluster)
	case "set-heuristic-domain-instance":
		// 先在 leader 确定值，再复制确定的属性，不能在各 FSM 上重新推测主库。
		info, err := inst.ReadClusterInfo(cluster)
		if err != nil {
			return nil, err
		}
		if info.ClusterDomain == "" {
			return nil, fmt.Errorf("cluster has no domain")
		}
		masters, err := inst.ReadClusterWriteableMaster(cluster)
		if err != nil {
			return nil, err
		}
		if len(masters) != 1 {
			return nil, fmt.Errorf("expected one writable master, got %d", len(masters))
		}
		value := domain.HostAttributes{Hostname: "*", AttributeName: info.ClusterDomain, AttributeValue: masters[0].Key.StringCode()}

		_, err = orcraft.PublishCommand("set-general-attribute", value)

		if err != nil {
			return nil, err
		}
		return masters[0].Key, inst.AuditOperation(name, &masters[0].Key, cluster)
	case "get-candidate-replica":
		candidate, _, _, _, _, err := inst.GetCandidateReplica(&key, false)
		return candidate, err
	case "rematch":
		instance, _, err := inst.RematchReplica(&key, true)
		return instance, err
	case "instance-status":
		instance, found, err := inst.ReadInstance(&key)
		if err != nil {
			return nil, err
		}
		if !found || instance == nil {
			return nil, fmt.Errorf("instance not found")
		}
		return instance.HumanReadableDescription(), nil
	}
	// 以下命令只查询拓扑/日志，不修改全局 CLI 参数。
	instance, err := inst.ReadTopologyInstance(&key)
	if err != nil {
		return nil, err
	}
	if instance == nil {
		return nil, fmt.Errorf("instance not found")
	}
	var coordinates *inst.BinlogCoordinates
	if raw := req.URL.Query().Get("binlog"); raw != "" {
		coordinates, err = inst.ParseBinlogCoordinates(raw)
		if err != nil {
			return nil, err
		}
	}
	switch name {
	case "master-pos-wait":
		if coordinates == nil {
			return nil, fmt.Errorf("binlog file:pos is required")
		}
		return inst.MasterPosWait(&key, coordinates)
	case "find-binlog-entry":
		pattern := req.URL.Query().Get("pattern")
		if pattern == "" {
			return nil, fmt.Errorf("pattern is required")
		}
		return inst.SearchEntryInInstanceBinlogs(instance, pattern, false, nil)
	case "last-executed-relay-entry":
		minimum, err := inst.GetPreviousKnownRelayLogCoordinatesForInstance(instance)
		if err != nil {
			return nil, err
		}
		return inst.GetLastExecutedEntryInRelayLogs(instance, minimum, instance.RelaylogCoordinates)
	case "correlate-binlog-pos", "correlate-relaylog-pos":
		destination, err := api.getInstanceKey(params["belowHost"], params["belowPort"])
		if err != nil {
			return nil, err
		}
		other, err := inst.ReadTopologyInstance(&destination)
		if err != nil {
			return nil, err
		}
		if other == nil {
			return nil, fmt.Errorf("destination not found")
		}
		if name == "correlate-binlog-pos" {
			if !instance.LogBinEnabled {
				return nil, fmt.Errorf("instance has no binary logs")
			}
			if coordinates == nil {
				coordinates = &instance.SelfBinlogCoordinates
			}
			result, _, err := inst.CorrelateBinlogCoordinates(instance, coordinates, other)
			return result, err
		}
		source, correlated, next, found, err := inst.CorrelateRelaylogCoordinates(instance, coordinates, other)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, fmt.Errorf("relay log coordinates not found")
		}
		return relayCorrelation{source, correlated, next}, nil
	}
	return nil, fmt.Errorf("unknown diagnostic operation")
}

type relayCorrelation struct{ Source, Correlated, Next *inst.BinlogCoordinates }

// untagThroughRaft keeps tag removal consistent across backends and returns the applied result.
func untagThroughRaft(key *inst.InstanceKey, tag *inst.Tag) (*inst.InstanceKeyMap, error) {

	var value any
	var err error
	if key == nil {
		value, err = orcraft.PublishCommand("delete-all-instance-tags", tag)
	} else {
		value, err = orcraft.PublishCommand("delete-instance-tag", inst.InstanceTag{Key: *key, T: *tag})
	}
	if err != nil {
		return nil, err
	}
	result, ok := value.(*inst.InstanceKeyMap)
	if !ok || result == nil {
		return nil, fmt.Errorf("unexpected tag removal result")
	}
	return result, nil
}
