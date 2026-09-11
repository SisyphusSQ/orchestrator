package cluster

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/openark/orchestrator/internal/http/authz"
	"github.com/openark/orchestrator/internal/http/contract"
	"github.com/openark/orchestrator/internal/http/presenter"
	"github.com/openark/orchestrator/internal/http/request"
	"github.com/openark/orchestrator/internal/http/transport"
	instaudit "github.com/openark/orchestrator/internal/inst/audit"
	instcandidate "github.com/openark/orchestrator/internal/inst/candidate"
	instcluster "github.com/openark/orchestrator/internal/inst/cluster"
	instmodel "github.com/openark/orchestrator/internal/inst/instance"
	instinventory "github.com/openark/orchestrator/internal/inst/inventory"
	instpool "github.com/openark/orchestrator/internal/inst/pool"
	instresolve "github.com/openark/orchestrator/internal/inst/resolve"
	insttag "github.com/openark/orchestrator/internal/inst/tag"
	insttopology "github.com/openark/orchestrator/internal/inst/topology"
	"github.com/openark/orchestrator/internal/logic/discovery"
	orcraft "github.com/openark/orchestrator/internal/raft"
)

// API contains cluster, inventory, tag, and pool handlers.
type API struct{}

func (api *API) asciiTopology(params transport.Params, r transport.Responder, req *http.Request, tabulated bool, printTags bool) {
	clusterName, err := request.ClusterName(request.ClusterHint(params))
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	asciiOutput, err := insttopology.ASCIITopology(clusterName, "", tabulated, printTags)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("Topology for cluster %s", clusterName), Details: asciiOutput})
}

// SnapshotTopologies triggers orchestrator to record a snapshot of host/master for all known hosts.
func (api *API) SnapshotTopologies(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	start := time.Now()
	if err := instinventory.SnapshotTopologies(); err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err)), Details: fmt.Sprintf("Took %v", time.Since(start))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: "Topology Snapshot completed", Details: fmt.Sprintf("Took %v", time.Since(start))})
}

// AsciiTopology returns an ascii graph of cluster's instances
func (api *API) AsciiTopology(params transport.Params, r transport.Responder, req *http.Request) {
	api.asciiTopology(params, r, req, false, false)
}

// AsciiTopology returns an ascii graph of cluster's instances
func (api *API) AsciiTopologyTabulated(params transport.Params, r transport.Responder, req *http.Request) {
	api.asciiTopology(params, r, req, true, false)
}

// AsciiTopologyTags returns an ascii graph of cluster's instances and instance tags
func (api *API) AsciiTopologyTags(params transport.Params, r transport.Responder, req *http.Request) {
	api.asciiTopology(params, r, req, false, true)
}

// Cluster provides list of instances in given cluster
func (api *API) Cluster(params transport.Params, r transport.Responder, req *http.Request) {
	clusterName, err := request.ClusterName(request.ClusterHint(params))
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	instances, err := instinventory.ReadClusterInstances(clusterName)

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.WriteJSON(r, http.StatusOK, instances)
}

// ClusterByAlias provides list of instances in given cluster
func (api *API) ClusterByAlias(params transport.Params, r transport.Responder, req *http.Request) {
	clusterName, err := instcluster.GetClusterByAlias(params["clusterAlias"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	params["clusterName"] = clusterName
	api.Cluster(params, r, req)
}

// ClusterByInstance provides list of instances in cluster an instance belongs to
func (api *API) ClusterByInstance(params transport.Params, r transport.Responder, req *http.Request) {
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	instance, found, err := instinventory.ReadInstanceContext(req.Context(), &instanceKey)
	if (!found) || (err != nil) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("Cannot read instance: %+v", instanceKey)})
		return
	}

	params["clusterName"] = instance.ClusterName
	api.Cluster(params, r, req)
}

// ClusterInfo provides details of a given cluster
func (api *API) ClusterInfo(params transport.Params, r transport.Responder, req *http.Request) {
	clusterName, err := request.ClusterName(request.ClusterHint(params))
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	clusterInfo, err := instinventory.ReadClusterInfo(clusterName)

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.WriteJSON(r, http.StatusOK, clusterInfo)
}

// Cluster provides list of instances in given cluster
func (api *API) ClusterInfoByAlias(params transport.Params, r transport.Responder, req *http.Request) {
	clusterName, err := instcluster.GetClusterByAlias(params["clusterAlias"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	params["clusterName"] = clusterName
	api.ClusterInfo(params, r, req)
}

// ClusterOSCReplicas returns heuristic list of OSC replicas
func (api *API) ClusterOSCReplicas(params transport.Params, r transport.Responder, req *http.Request) {
	clusterName, err := request.ClusterName(request.ClusterHint(params))
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	instances, err := instinventory.GetClusterOSCReplicas(clusterName)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.WriteJSON(r, http.StatusOK, instances)
}

// SetClusterAlias will change an alias for a given clustername
func (api *API) SetClusterAliasManualOverride(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	clusterName := params["clusterName"]
	alias := req.URL.Query().Get("alias")

	var err error

	_, err = orcraft.PublishCommand("set-cluster-alias-manual-override", []string{clusterName, alias})

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("Cluster %s now has alias '%s'", clusterName, alias)})
}

// Clusters provides list of known clusters
func (api *API) Clusters(params transport.Params, r transport.Responder, req *http.Request) {
	clusterNames, err := instinventory.ReadClusters()

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.WriteJSON(r, http.StatusOK, clusterNames)
}

// ClustersInfo provides list of known clusters, along with some added metadata per cluster
func (api *API) ClustersInfo(params transport.Params, r transport.Responder, req *http.Request) {
	clustersInfo, err := instinventory.ReadClustersInfo("")

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.WriteJSON(r, http.StatusOK, clustersInfo)
}

// Tags lists existing tags for a given instance
func (api *API) Tags(params transport.Params, r transport.Responder, req *http.Request) {
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	tags, err := insttag.ReadInstanceTags(&instanceKey)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	tagStrings := []string{}
	for _, tag := range tags {
		tagStrings = append(tagStrings, tag.String())
	}
	presenter.WriteJSON(r, http.StatusOK, tagStrings)
}

// TagValue returns a given tag's value for a specific instance
func (api *API) TagValue(params transport.Params, r transport.Responder, req *http.Request) {
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	tag, err := request.Tag(params, req)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	tagExists, err := insttag.ReadInstanceTag(&instanceKey, tag)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	if tagExists {
		presenter.WriteJSON(r, http.StatusOK, tag.TagValue)
	} else {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("tag %s not found for %+v", tag.TagName, instanceKey)})
	}
}

// Tagged return instance keys tagged by "tag" query param
func (api *API) Tagged(params transport.Params, r transport.Responder, req *http.Request) {
	tagsString := req.URL.Query().Get("tag")
	instanceKeyMap, err := insttag.GetInstanceKeysByTags(tagsString)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.WriteJSON(r, http.StatusOK, instanceKeyMap.GetInstanceKeys())
}

// Tags adds a tag to a given instance
func (api *API) Tag(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	tag, err := request.Tag(params, req)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	_, err = orcraft.PublishCommand("put-instance-tag", insttag.InstanceTag{Key: instanceKey, T: *tag})

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("%+v tagged with %s", instanceKey, tag.String()), Details: instanceKey})
}

// Untag removes a tag from an instance
func (api *API) Untag(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	tag, err := request.Tag(params, req)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	untagged, err := untagThroughRaft(&instanceKey, tag)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("%s removed from %+v instances", tag.TagName, len(*untagged)), Details: untagged.GetInstanceKeys()})
}

// UntagAll removes a tag from all matching instances
func (api *API) UntagAll(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	tag, err := request.Tag(params, req)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	untagged, err := untagThroughRaft(nil, tag)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("%s removed from %+v instances", tag.TagName, len(*untagged)), Details: untagged.GetInstanceKeys()})
}

// Write a cluster's master (or all clusters masters) to kv stores.
// This should generally only happen once in a lifetime of a cluster. Otherwise KV
// stores are updated via failovers.
func (api *API) SubmitMastersToKvStores(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	clusterName, err := request.ClusterNameIfExists(params)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	kvPairs, submittedCount, err := discovery.SubmitMastersToKvStores(clusterName, true)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("Submitted %d masters", submittedCount), Details: kvPairs})
}

// Clusters provides list of known masters
func (api *API) Masters(params transport.Params, r transport.Responder, req *http.Request) {
	instances, err := instinventory.ReadWriteableClustersMasters()

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.WriteJSON(r, http.StatusOK, instances)
}

// ClusterMaster returns the writable master of a given cluster
func (api *API) ClusterMaster(params transport.Params, r transport.Responder, req *http.Request) {
	clusterName, err := request.ClusterName(request.ClusterHint(params))
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	masters, err := instinventory.ReadClusterMaster(clusterName)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	if len(masters) == 0 {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("No masters found for %+v", clusterName)})
		return
	}

	presenter.WriteJSON(r, http.StatusOK, masters[0])
}

// Downtimed lists downtimed instances, potentially filtered by cluster
func (api *API) Downtimed(params transport.Params, r transport.Responder, req *http.Request) {
	clusterName, err := request.ClusterNameIfExists(params)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	instances, err := instinventory.ReadDowntimedInstances(clusterName)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.WriteJSON(r, http.StatusOK, instances)
}

// AllInstances lists all known instances
func (api *API) AllInstances(params transport.Params, r transport.Responder, req *http.Request) {
	instances, err := instinventory.SearchInstances("")

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.WriteJSON(r, http.StatusOK, instances)
}

// Search provides list of instances matching given search param via various criteria.
func (api *API) Search(params transport.Params, r transport.Responder, req *http.Request) {
	searchString := params["searchString"]
	if searchString == "" {
		searchString = req.URL.Query().Get("s")
	}
	instances, err := instinventory.SearchInstances(searchString)

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.WriteJSON(r, http.StatusOK, instances)
}

// Problems provides list of instances with known problems
func (api *API) Problems(params transport.Params, r transport.Responder, req *http.Request) {
	clusterName := params["clusterName"]
	instances, err := instinventory.ReadProblemInstances(clusterName)

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.WriteJSON(r, http.StatusOK, instances)
}

// Audit provides list of audit entries by given page number
func (api *API) Audit(params transport.Params, r transport.Responder, req *http.Request) {
	page, err := strconv.Atoi(params["page"])
	if err != nil || page < 0 {
		page = 0
	}
	var auditedInstanceKey *instmodel.InstanceKey
	if instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"]); err == nil {
		auditedInstanceKey = &instanceKey
	}

	audits, err := instaudit.ReadRecentAudit(auditedInstanceKey, page)

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.WriteJSON(r, http.StatusOK, audits)
}

// HostnameResolveCache shows content of in-memory hostname cache
func (api *API) HostnameResolveCache(params transport.Params, r transport.Responder, req *http.Request) {
	content, err := instresolve.HostnameResolveCache()

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: "Cache retrieved", Details: content})
}

// ResetHostnameResolveCache clears in-memory hostname resovle cache
func (api *API) ResetHostnameResolveCache(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	err := instresolve.ResetHostnameResolveCache()

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: "Hostname cache cleared"})
}

// DeregisterHostnameUnresolve deregisters the unresolve name used previously
func (api *API) DeregisterHostnameUnresolve(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}

	var instanceKey *instmodel.InstanceKey
	if instKey, err := request.ResolveInstanceKey(params["host"], params["port"]); err == nil {
		instanceKey = &instKey
	}

	var err error
	registration := instresolve.NewHostnameDeregistration(instanceKey)

	_, err = orcraft.PublishCommand("register-hostname-unresolve", registration)

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: "Hostname deregister unresolve completed", Details: instanceKey})
}

// RegisterHostnameUnresolve registers the unresolve name to use
func (api *API) RegisterHostnameUnresolve(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}

	var instanceKey *instmodel.InstanceKey
	if instKey, err := request.ResolveInstanceKey(params["host"], params["port"]); err == nil {
		instanceKey = &instKey
	}

	hostname := params["virtualname"]
	var err error
	registration := instresolve.NewHostnameRegistration(instanceKey, hostname)

	_, err = orcraft.PublishCommand("register-hostname-unresolve", registration)

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: "Hostname register unresolve completed", Details: instanceKey})
}

// SubmitPoolInstances (re-)applies the list of hostnames for a given pool
func (api *API) SubmitPoolInstances(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	pool := params["pool"]
	instances := req.URL.Query().Get("instances")

	var err error
	submission := instpool.NewPoolInstancesSubmission(pool, instances)

	_, err = orcraft.PublishCommand("submit-pool-instances", submission)

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("Applied %s pool instances", pool), Details: pool})
}

// SubmitPoolHostnames (re-)applies the list of hostnames for a given pool
func (api *API) ReadClusterPoolInstancesMap(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	clusterName := params["clusterName"]
	pool := params["pool"]

	poolInstancesMap, err := instpool.ReadClusterPoolInstancesMap(clusterName, pool)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("Read pool instances for cluster %s", clusterName), Details: poolInstancesMap})
}

// GetHeuristicClusterPoolInstances returns instances belonging to a cluster's pool
func (api *API) GetHeuristicClusterPoolInstances(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	clusterName, err := request.ClusterName(request.ClusterHint(params))
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	pool := params["pool"]

	instances, err := instinventory.GetHeuristicClusterPoolInstances(clusterName, pool)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("Heuristic pool instances for cluster %s", clusterName), Details: instances})
}

// GetHeuristicClusterPoolInstances returns instances belonging to a cluster's pool
func (api *API) GetHeuristicClusterPoolInstancesLag(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	clusterName, err := instcluster.ReadClusterNameByAlias(params["clusterName"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	pool := params["pool"]

	lag, err := instinventory.GetHeuristicClusterPoolInstancesLag(clusterName, pool)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("Heuristic pool lag for cluster %s", clusterName), Details: lag})
}

// ReloadClusterAlias clears in-memory hostname resovle cache
func (api *API) ReloadClusterAlias(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "This API call has been retired"})
}

// BulkPromotionRules returns a list of the known promotion rules for each instance
func (api *API) BulkPromotionRules(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}

	promotionRules, err := instcandidate.BulkReadCandidateDatabaseInstance()
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.WriteJSON(r, http.StatusOK, promotionRules)
}

// BulkInstances returns a list of all known instances
func (api *API) BulkInstances(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}

	instances, err := instinventory.BulkReadInstance()
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.WriteJSON(r, http.StatusOK, instances)
}

func untagThroughRaft(key *instmodel.InstanceKey, tag *insttag.Tag) (*instmodel.InstanceKeyMap, error) {
	var value any
	var err error
	if key == nil {
		value, err = orcraft.PublishCommand("delete-all-instance-tags", tag)
	} else {
		value, err = orcraft.PublishCommand("delete-instance-tag", insttag.InstanceTag{Key: *key, T: *tag})
	}
	if err != nil {
		return nil, err
	}
	result, ok := value.(*instmodel.InstanceKeyMap)
	if !ok || result == nil {
		return nil, fmt.Errorf("unexpected tag removal result")
	}
	return result, nil
}
