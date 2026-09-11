package instance

import (
	"fmt"
	"net"
	"net/http"

	"github.com/openark/orchestrator/internal/http/authz"
	"github.com/openark/orchestrator/internal/http/contract"
	"github.com/openark/orchestrator/internal/http/presenter"
	httpraft "github.com/openark/orchestrator/internal/http/raft"
	"github.com/openark/orchestrator/internal/http/request"
	"github.com/openark/orchestrator/internal/http/transport"
	instreplication "github.com/openark/orchestrator/internal/inst/change/replication"
	instdiscovery "github.com/openark/orchestrator/internal/inst/discovery"
	instinventory "github.com/openark/orchestrator/internal/inst/inventory"
	orcraft "github.com/openark/orchestrator/internal/raft"
)

// API contains instance discovery and lifecycle handlers.
type API struct{}

// Replicas lists all replicas of an instance.
func (api *API) Replicas(params transport.Params, r transport.Responder, req *http.Request) {
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	replicas, err := instinventory.ReadReplicaInstances(&instanceKey)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("Cannot read instance: %+v", instanceKey)})
		return
	}
	presenter.WriteJSON(r, http.StatusOK, replicas)
}

// Read returns an instance's details.
func (api *API) Read(params transport.Params, r transport.Responder, req *http.Request) {
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	instance, found, err := instinventory.ReadInstanceContext(req.Context(), &instanceKey)
	if !found || err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("Cannot read instance: %+v", instanceKey)})
		return
	}
	presenter.WriteJSON(r, http.StatusOK, instance)
}

// AsyncDiscover initiates an asynchronous instance discovery.
func (api *API) AsyncDiscover(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	go api.Discover(params, r, req, user)
	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("Asynchronous discovery initiated for Instance: %+v", instanceKey)})
}

// Discover synchronously reads and publishes an instance discovery.
func (api *API) Discover(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	instance, err := instdiscovery.ReadTopologyInstanceContext(req.Context(), &instanceKey)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	if _, err := orcraft.PublishCommand("discover", instanceKey); err != nil {
		httpraft.Respond(r, err, "", nil)
		return
	}
	if instance != nil {
		presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("Instance discovered: %+v", instance.Key), Details: instance})
		return
	}
	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: "No instances discovered", Details: nil})
}

// Refresh synchronously refreshes an instance.
func (api *API) Refresh(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	if _, err = instreplication.RefreshTopologyInstance(&instanceKey); err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("Instance refreshed: %+v", instanceKey), Details: instanceKey})
}

// Forget removes an instance entry from the backend database.
func (api *API) Forget(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveRawInstanceKey(params["host"], params["port"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	if _, err = orcraft.PublishCommand("forget", instanceKey); err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("Instance forgotten: %+v", instanceKey), Details: instanceKey})
}

// ForgetCluster removes all instances in a cluster.
func (api *API) ForgetCluster(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	clusterName, err := request.ClusterName(request.ClusterHint(params))
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	if _, err := orcraft.PublishCommand("forget-cluster", clusterName); err != nil {
		httpraft.Respond(r, err, "", nil)
		return
	}
	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("Cluster forgotten: %+v", clusterName)})
}

// Resolve resolves a hostname and checks whether its port is reachable.
func (api *API) Resolve(params transport.Params, r transport.Responder, req *http.Request) {
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	if conn, err := net.Dial("tcp", instanceKey.DisplayString()); err == nil {
		_ = conn.Close()
	} else {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: "Instance resolved", Details: instanceKey})
}
