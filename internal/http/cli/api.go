package cli

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/openark/orchestrator/internal/http/authz"
	"github.com/openark/orchestrator/internal/http/contract"
	"github.com/openark/orchestrator/internal/http/presenter"
	httpraft "github.com/openark/orchestrator/internal/http/raft"
	"github.com/openark/orchestrator/internal/http/request"
	"github.com/openark/orchestrator/internal/http/transport"
	instaudit "github.com/openark/orchestrator/internal/inst/audit"
	instbinlog "github.com/openark/orchestrator/internal/inst/binlog"
	instregroup "github.com/openark/orchestrator/internal/inst/change/regroup"
	instrelocation "github.com/openark/orchestrator/internal/inst/change/relocation"
	instreplication "github.com/openark/orchestrator/internal/inst/change/replication"
	instdiscovery "github.com/openark/orchestrator/internal/inst/discovery"
	instmodel "github.com/openark/orchestrator/internal/inst/instance"
	instinventory "github.com/openark/orchestrator/internal/inst/inventory"
	instpool "github.com/openark/orchestrator/internal/inst/pool"
	instresolve "github.com/openark/orchestrator/internal/inst/resolve"
	"github.com/openark/orchestrator/internal/models/domain"
	"github.com/openark/orchestrator/internal/process"
	orcraft "github.com/openark/orchestrator/internal/raft"
)

// Register 补齐旧本地 CLI 的诊断能力。每条路由仍由根路由组装层添加鉴权和 leader 代理。
func Register(register func(string, transport.Handler)) {
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
		register("cli/"+path, handler(name))
	}
}

func handler(name string) transport.Handler {
	return func(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
		if !authz.ForAction(req, user) {
			presenter.RespondStatus(r, http.StatusForbidden, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
			return
		}
		result, err := diagnostic(name, params, req)
		if err != nil {
			if orcraft.ClassOf(err) == orcraft.ClassIndeterminate {
				httpraft.Respond(r, err, name, result)
				return
			}
			presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error()})
			return
		}
		presenter.Respond(r, &contract.Response{Code: contract.OK, Message: name, Details: result})
	}
}
func diagnostic(name string, params transport.Params, req *http.Request) (any, error) {
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	var key instmodel.InstanceKey
	var err error
	if params["host"] != "" {
		key, err = request.ResolveInstanceKey(params["host"], params["port"])
		if err != nil {
			return nil, err
		}
	}
	cluster := ""
	if params["clusterHint"] != "" {
		cluster, err = request.ClusterName(params["clusterHint"])
		if err != nil {
			return nil, err
		}
	}
	switch name {
	case "cluster-pool-instances":
		instances, err := instpool.ReadAllClusterPoolInstances()
		if err != nil {
			return nil, err
		}
		result := make([]*instpool.ClusterPoolInstance, 0, len(instances))
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
		return instinventory.FindInstances(pattern)
	case "active-nodes":
		return process.ReadAvailableNodes(false)
	case "show-resolve-hosts":
		return instresolve.ReadAllHostnameResolves()
	case "show-unresolve-hosts":
		return instresolve.ReadAllHostnameUnresolves()
	case "which-lost-in-recovery":
		return instinventory.ReadLostInRecoveryInstances("")
	case "which-cluster-gh-ost-replicas":
		return instinventory.GetClusterGhostReplicas(cluster)
	case "get-cluster-heuristic-lag":
		return instinventory.GetClusterHeuristicLag(cluster)
	case "which-heuristic-domain-instance":
		return instinventory.GetHeuristicClusterDomainInstanceAttribute(cluster)
	case "set-heuristic-domain-instance":
		// 先在 leader 确定值，再复制确定的属性，不能在各 FSM 上重新推测主库。
		info, err := instinventory.ReadClusterInfo(cluster)
		if err != nil {
			return nil, err
		}
		if info.ClusterDomain == "" {
			return nil, fmt.Errorf("cluster has no domain")
		}
		masters, err := instinventory.ReadClusterWriteableMaster(cluster)
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
		return masters[0].Key, instaudit.AuditOperation(name, &masters[0].Key, cluster)
	case "get-candidate-replica":
		candidate, _, _, _, _, err := instregroup.GetCandidateReplica(&key, false)
		return candidate, err
	case "rematch":
		instance, _, err := instrelocation.RematchReplica(&key, true)
		return instance, err
	case "instance-status":
		instance, found, err := instinventory.ReadInstance(&key)
		if err != nil {
			return nil, err
		}
		if !found || instance == nil {
			return nil, fmt.Errorf("instance not found")
		}
		return instance.HumanReadableDescription(), nil
	}
	// 以下命令只查询拓扑/日志，不修改全局 CLI 参数。
	instance, err := instdiscovery.ReadTopologyInstance(&key)
	if err != nil {
		return nil, err
	}
	if instance == nil {
		return nil, fmt.Errorf("instance not found")
	}
	var coordinates *instmodel.BinlogCoordinates
	if raw := req.URL.Query().Get("binlog"); raw != "" {
		coordinates, err = instmodel.ParseBinlogCoordinates(raw)
		if err != nil {
			return nil, err
		}
	}
	switch name {
	case "master-pos-wait":
		if coordinates == nil {
			return nil, fmt.Errorf("binlog file:pos is required")
		}
		return instreplication.MasterPosWait(&key, coordinates)
	case "find-binlog-entry":
		pattern := req.URL.Query().Get("pattern")
		if pattern == "" {
			return nil, fmt.Errorf("pattern is required")
		}
		return instbinlog.SearchEntryInInstanceBinlogs(instance, pattern, false, nil)
	case "last-executed-relay-entry":
		minimum, err := instinventory.GetPreviousKnownRelayLogCoordinatesForInstance(instance)
		if err != nil {
			return nil, err
		}
		return instbinlog.GetLastExecutedEntryInRelayLogs(instance, minimum, instance.RelaylogCoordinates)
	case "correlate-binlog-pos", "correlate-relaylog-pos":
		destination, err := request.ResolveInstanceKey(params["belowHost"], params["belowPort"])
		if err != nil {
			return nil, err
		}
		other, err := instdiscovery.ReadTopologyInstance(&destination)
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
			result, _, err := instrelocation.CorrelateBinlogCoordinates(instance, coordinates, other)
			return result, err
		}
		source, correlated, next, found, err := instrelocation.CorrelateRelaylogCoordinates(instance, coordinates, other)
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

type relayCorrelation struct{ Source, Correlated, Next *instmodel.BinlogCoordinates }
