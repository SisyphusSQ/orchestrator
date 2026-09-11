package topology

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/openark/orchestrator/internal/http/authz"
	"github.com/openark/orchestrator/internal/http/contract"
	"github.com/openark/orchestrator/internal/http/presenter"
	"github.com/openark/orchestrator/internal/http/request"
	"github.com/openark/orchestrator/internal/http/transport"
	instregroup "github.com/openark/orchestrator/internal/inst/change/regroup"
	instrelocation "github.com/openark/orchestrator/internal/inst/change/relocation"
	instreplication "github.com/openark/orchestrator/internal/inst/change/replication"
	instequivalence "github.com/openark/orchestrator/internal/inst/equivalence"
	instmodel "github.com/openark/orchestrator/internal/inst/instance"
	instinventory "github.com/openark/orchestrator/internal/inst/inventory"
	instresolve "github.com/openark/orchestrator/internal/inst/resolve"
	"github.com/openark/orchestrator/internal/models/domain"
	orcraft "github.com/openark/orchestrator/internal/raft"
)

// API contains topology mutation and replication handlers.
type API struct{}

// MoveUp attempts to move an instance up the topology
func (api *API) MoveUp(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	instance, err := instrelocation.MoveUp(&instanceKey)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("Instance %+v moved up", instanceKey), Details: instance})
}

// MoveUpReplicas attempts to move up all replicas of an instance
func (api *API) MoveUpReplicas(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	replicas, newMaster, errs, err := instrelocation.MoveUpReplicas(&instanceKey, req.URL.Query().Get("pattern"))
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("Moved up %d replicas of %+v below %+v; %d errors: %+v", len(replicas), instanceKey, newMaster.Key, len(errs), errs), Details: replicas})
}

// Repoint positiones a replica under another (or same) master with exact same coordinates.
// Useful for binlog servers
func (api *API) Repoint(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	var belowKey *instmodel.InstanceKey
	if params["belowHost"] != "" {
		key, err := request.ResolveInstanceKey(params["belowHost"], params["belowPort"])
		if err != nil {
			presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
			return
		}
		belowKey = &key
	}

	instance, err := instrelocation.Repoint(&instanceKey, belowKey, domain.GTIDHintNeutral)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("Instance %+v repointed below %+v", instanceKey, belowKey), Details: instance})
}

// MoveUpReplicas attempts to move up all replicas of an instance
func (api *API) RepointReplicas(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	var destination *instmodel.InstanceKey
	if raw := req.URL.Query().Get("destination"); raw != "" {
		destination, err = instresolve.ParseInstanceKey(raw)
		if err != nil {
			presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
			return
		}
	}
	replicas, _, err := instrelocation.RepointReplicasTo(&instanceKey, req.URL.Query().Get("pattern"), destination)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("Repointed %d replicas of %+v", len(replicas), instanceKey), Details: replicas})
}

// MakeCoMaster attempts to make an instance co-master with its own master
func (api *API) MakeCoMaster(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	instance, err := instrelocation.MakeCoMaster(&instanceKey)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("Instance made co-master: %+v", instance.Key), Details: instance})
}

// ResetReplication makes a replica forget about its master, effectively breaking the replication
func (api *API) ResetReplication(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	instance, err := instrelocation.ResetReplicationOperation(&instanceKey)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("Replica reset on %+v", instance.Key), Details: instance})
}

// ChangeMasterCredentials re-applies replication user/password (and SSL material supplied via
// ReplicationCredentialsQuery) on an instance while preserving its existing SOURCE_SSL/TLS
// configuration. Useful for credential rotation and for exercising the TLS-preservation path.
func (api *API) ChangeMasterCredentials(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	creds, err := instreplication.ReadReplicationCredentials(&instanceKey)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	instance, err := instreplication.ChangeMasterCredentials(&instanceKey, creds)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("Replication credentials re-applied on %+v", instance.Key), Details: instance})
}

// DetachReplicaMasterHost detaches a replica from its master by setting an invalid
// (yet revertible) host name
func (api *API) DetachReplicaMasterHost(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	instance, err := instrelocation.DetachReplicaMasterHost(&instanceKey)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("Replica detached: %+v", instance.Key), Details: instance})
}

// ReattachReplicaMasterHost reverts a detachReplicaMasterHost command
// by resoting the original master hostname in CHANGE MASTER TO
func (api *API) ReattachReplicaMasterHost(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	instance, err := instrelocation.ReattachReplicaMasterHost(&instanceKey)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("Replica reattached: %+v", instance.Key), Details: instance})
}

// EnableGTID attempts to enable GTID on a replica
func (api *API) EnableGTID(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	instance, err := instrelocation.EnableGTID(&instanceKey)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("Enabled GTID on %+v", instance.Key), Details: instance})
}

// DisableGTID attempts to disable GTID on a replica, and revert to binlog file:pos
func (api *API) DisableGTID(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	instance, err := instrelocation.DisableGTID(&instanceKey)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("Disabled GTID on %+v", instance.Key), Details: instance})
}

// LocateErrantGTID identifies the binlog positions for errant GTIDs on an instance
func (api *API) LocateErrantGTID(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	errantBinlogs, err := instrelocation.LocateErrantGTID(&instanceKey)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: "located errant GTID", Details: errantBinlogs})
}

// ErrantGTIDResetMaster removes errant transactions on a server by way of RESET MASTER
func (api *API) ErrantGTIDResetMaster(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	instance, err := instrelocation.ErrantGTIDResetMaster(&instanceKey)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("Removed errant GTID on %+v and issued a RESET MASTER", instance.Key), Details: instance})
}

// ErrantGTIDInjectEmpty removes errant transactions by injecting and empty transaction on the cluster's master
func (api *API) ErrantGTIDInjectEmpty(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	instance, clusterMaster, countInjectedTransactions, err := instrelocation.ErrantGTIDInjectEmpty(&instanceKey)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("Have injected %+v transactions on cluster master %+v", countInjectedTransactions, clusterMaster.Key), Details: instance})
}

// MoveBelow attempts to move an instance below its supposed sibling
func (api *API) MoveBelow(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	siblingKey, err := request.ResolveInstanceKey(params["siblingHost"], params["siblingPort"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	instance, err := instrelocation.MoveBelow(&instanceKey, &siblingKey)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("Instance %+v moved below %+v", instanceKey, siblingKey), Details: instance})
}

// MoveBelowGTID attempts to move an instance below another, via GTID
func (api *API) MoveBelowGTID(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	belowKey, err := request.ResolveInstanceKey(params["belowHost"], params["belowPort"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	instance, err := instrelocation.MoveBelowGTID(&instanceKey, &belowKey)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("Instance %+v moved below %+v via GTID", instanceKey, belowKey), Details: instance})
}

// MoveReplicasGTID attempts to move an instance below another, via GTID
func (api *API) MoveReplicasGTID(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	belowKey, err := request.ResolveInstanceKey(params["belowHost"], params["belowPort"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	movedReplicas, _, errs, err := instrelocation.MoveReplicasGTID(&instanceKey, &belowKey, req.URL.Query().Get("pattern"))
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("Moved %d replicas of %+v below %+v via GTID; %d errors: %+v", len(movedReplicas), instanceKey, belowKey, len(errs), errs), Details: belowKey})
}

// TakeSiblings
func (api *API) TakeSiblings(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	instance, count, err := instrelocation.TakeSiblings(&instanceKey)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("Took %d siblings of %+v", count, instanceKey), Details: instance})
}

// TakeMaster
func (api *API) TakeMaster(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	instance, err := instrelocation.TakeMaster(&instanceKey, false)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("%+v took its master", instanceKey), Details: instance})
}

// RelocateBelow attempts to move an instance below another, orchestrator choosing the best (potentially multi-step)
// relocation method
func (api *API) RelocateBelow(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	belowKey, err := request.ResolveInstanceKey(params["belowHost"], params["belowPort"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	instance, err := instrelocation.RelocateBelow(&instanceKey, &belowKey)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("Instance %+v relocated below %+v", instanceKey, belowKey), Details: instance})
}

// Relocates attempts to smartly relocate replicas of a given instance below another
func (api *API) RelocateReplicas(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	belowKey, err := request.ResolveInstanceKey(params["belowHost"], params["belowPort"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	replicas, _, errs, err := instrelocation.RelocateReplicas(&instanceKey, &belowKey, req.URL.Query().Get("pattern"))
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("Relocated %d replicas of %+v below %+v; %d errors: %+v", len(replicas), instanceKey, belowKey, len(errs), errs), Details: replicas})
}

// MoveEquivalent attempts to move an instance below another, baseed on known equivalence master coordinates
func (api *API) MoveEquivalent(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	belowKey, err := request.ResolveInstanceKey(params["belowHost"], params["belowPort"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	instance, err := instrelocation.MoveEquivalent(&instanceKey, &belowKey)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("Instance %+v relocated via equivalence coordinates below %+v", instanceKey, belowKey), Details: instance})
}

// LastPseudoGTID attempts to find the last pseugo-gtid entry in an instance
func (api *API) LastPseudoGTID(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	instance, found, err := instinventory.ReadInstanceContext(req.Context(), &instanceKey)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	if instance == nil || !found {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("Instance not found: %+v", instanceKey)})
		return
	}
	coordinates, text, err := instrelocation.FindLastPseudoGTIDEntry(instance, instance.RelaylogCoordinates, nil, req.URL.Query().Get("strict") == "true", nil)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("%+v", *coordinates), Details: text})
}

// MatchBelow attempts to move an instance below another via pseudo GTID matching of binlog entries
func (api *API) MatchBelow(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	belowKey, err := request.ResolveInstanceKey(params["belowHost"], params["belowPort"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	instance, matchedCoordinates, err := instrelocation.MatchBelow(&instanceKey, &belowKey, true)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("Instance %+v matched below %+v at %+v", instanceKey, belowKey, *matchedCoordinates), Details: instance})
}

// MatchBelow attempts to move an instance below another via pseudo GTID matching of binlog entries
func (api *API) MatchUp(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	instance, matchedCoordinates, err := instrelocation.MatchUp(&instanceKey, true)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("Instance %+v matched up at %+v", instanceKey, *matchedCoordinates), Details: instance})
}

// MultiMatchReplicas attempts to match all replicas of a given instance below another, efficiently
func (api *API) MultiMatchReplicas(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	belowKey, err := request.ResolveInstanceKey(params["belowHost"], params["belowPort"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	replicas, newMaster, errs, err := instrelocation.MultiMatchReplicas(&instanceKey, &belowKey, req.URL.Query().Get("pattern"))
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("Matched %d replicas of %+v below %+v; %d errors: %+v", len(replicas), instanceKey, newMaster.Key, len(errs), errs), Details: newMaster.Key})
}

// MatchUpReplicas attempts to match up all replicas of an instance
func (api *API) MatchUpReplicas(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	replicas, newMaster, errs, err := instrelocation.MatchUpReplicas(&instanceKey, req.URL.Query().Get("pattern"))
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("Matched up %d replicas of %+v below %+v; %d errors: %+v", len(replicas), instanceKey, newMaster.Key, len(errs), errs), Details: newMaster.Key})
}

// RegroupReplicas attempts to pick a replica of a given instance and make it take its siblings, using any
// method possible (GTID, Pseudo-GTID, binlog servers)
func (api *API) RegroupReplicas(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	lostReplicas, equalReplicas, aheadReplicas, cannotReplicateReplicas, promotedReplica, err := instregroup.RegroupReplicas(&instanceKey, false, nil, nil)
	lostReplicas = append(lostReplicas, cannotReplicateReplicas...)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("promoted replica: %s, lost: %d, trivial: %d, pseudo-gtid: %d",
		promotedReplica.Key.DisplayString(), len(lostReplicas), len(equalReplicas), len(aheadReplicas)), Details: promotedReplica.Key})
}

// RegroupReplicas attempts to pick a replica of a given instance and make it take its siblings, efficiently,
// using pseudo-gtid if necessary
func (api *API) RegroupReplicasPseudoGTID(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	lostReplicas, equalReplicas, aheadReplicas, cannotReplicateReplicas, promotedReplica, err := instregroup.RegroupReplicasPseudoGTID(&instanceKey, false, nil, nil, nil)
	lostReplicas = append(lostReplicas, cannotReplicateReplicas...)

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("promoted replica: %s, lost: %d, trivial: %d, pseudo-gtid: %d",
		promotedReplica.Key.DisplayString(), len(lostReplicas), len(equalReplicas), len(aheadReplicas)), Details: promotedReplica.Key})
}

// RegroupReplicasGTID attempts to pick a replica of a given instance and make it take its siblings, efficiently, using GTID
func (api *API) RegroupReplicasGTID(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	lostReplicas, movedReplicas, cannotReplicateReplicas, promotedReplica, err := instregroup.RegroupReplicasGTID(&instanceKey, false, true, nil, nil, nil)
	lostReplicas = append(lostReplicas, cannotReplicateReplicas...)

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("promoted replica: %s, lost: %d, moved: %d",
		promotedReplica.Key.DisplayString(), len(lostReplicas), len(movedReplicas)), Details: promotedReplica.Key})
}

// RegroupReplicasBinlogServers attempts to pick a replica of a given instance and make it take its siblings, efficiently, using GTID
func (api *API) RegroupReplicasBinlogServers(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	_, promotedBinlogServer, err := instregroup.RegroupReplicasBinlogServers(&instanceKey, false)

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("promoted binlog server: %s",
		promotedBinlogServer.Key.DisplayString()), Details: promotedBinlogServer.Key})
}

// MakeMaster attempts to make the given instance a master, and match its siblings to be its replicas
func (api *API) MakeMaster(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	instance, err := instrelocation.MakeMaster(&instanceKey)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("Instance %+v now made master", instanceKey), Details: instance})
}

// MakeLocalMaster attempts to make the given instance a local master: take over its master by
// enslaving its siblings and replicating from its grandparent.
func (api *API) MakeLocalMaster(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	instance, err := instrelocation.MakeLocalMaster(&instanceKey)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("Instance %+v now made local master", instanceKey), Details: instance})
}

// SkipQuery skips a single query on a failed replication instance
func (api *API) SkipQuery(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	instance, err := instreplication.SkipQuery(&instanceKey)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("Query skipped on %+v", instance.Key), Details: instance})
}

// StartReplication starts replication on given instance
func (api *API) StartReplication(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	instance, err := instreplication.StartReplication(&instanceKey)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("Replica started: %+v", instance.Key), Details: instance})
}

// RestartReplication stops & starts replication on given instance
func (api *API) RestartReplication(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	instance, err := instreplication.RestartReplication(&instanceKey)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("Replica restarted: %+v", instance.Key), Details: instance})
}

// StopReplication stops replication on given instance
func (api *API) StopReplication(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	instance, err := instreplication.StopReplication(&instanceKey)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("Replica stopped: %+v", instance.Key), Details: instance})
}

// StopReplicationNicely stops replication on given instance, such that sql thead is aligned with IO thread
func (api *API) StopReplicationNicely(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	instance, err := instreplication.StopReplicationNicely(&instanceKey, 0)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("Replica stopped nicely: %+v", instance.Key), Details: instance})
}

// FlushBinaryLogs runs a single FLUSH BINARY LOGS
func (api *API) FlushBinaryLogs(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	var instance *instmodel.Instance
	if file := req.URL.Query().Get("binlog"); file != "" {
		instance, err = instreplication.FlushBinaryLogsTo(&instanceKey, file)
	} else {
		instance, err = instreplication.FlushBinaryLogs(&instanceKey, 1)
	}
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("Binary logs flushed on: %+v", instance.Key), Details: instance})
}

// PurgeBinaryLogs purges binary logs up to given binlog file
func (api *API) PurgeBinaryLogs(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	logFile := params["logFile"]
	if logFile == "" {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "purge-binary-logs: expected log file name or 'latest'"})
		return
	}
	force := (req.URL.Query().Get("force") == "true") || (params["force"] == "true")
	var instance *instmodel.Instance
	if logFile == "latest" {
		instance, err = instrelocation.PurgeBinaryLogsToLatest(&instanceKey, force)
	} else {
		instance, err = instrelocation.PurgeBinaryLogsTo(&instanceKey, logFile, force)
	}
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("Binary logs flushed on: %+v", instance.Key), Details: instance})
}

// RestartReplicationStatements receives a query to execute that requires a replication restart to apply.
// As an example, this may be `set global rpl_semi_sync_slave_enabled=1`. orchestrator will check
// replication status on given host and will wrap with appropriate stop/start statements, if need be.
func (api *API) RestartReplicationStatements(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	query := req.URL.Query().Get("q")
	statements, err := instreplication.GetReplicationRestartPreserveStatements(&instanceKey, query)

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("statements for: %+v", instanceKey), Details: statements})
}

// MasterEquivalent provides (possibly empty) list of master coordinates equivalent to the given ones
func (api *API) MasterEquivalent(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	coordinates, err := request.BinlogCoordinates(params["logFile"], params["logPos"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	instanceCoordinates := &instequivalence.InstanceBinlogCoordinates{Key: instanceKey, Coordinates: coordinates}

	equivalentCoordinates, err := instequivalence.GetEquivalentMasterCoordinates(instanceCoordinates)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("Found %+v equivalent coordinates", len(equivalentCoordinates)), Details: equivalentCoordinates})
}

// CanReplicateFrom attempts to move an instance below another via pseudo GTID matching of binlog entries
func (api *API) CanReplicateFrom(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
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
	belowKey, err := request.ResolveInstanceKey(params["belowHost"], params["belowPort"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	belowInstance, found, err := instinventory.ReadInstanceContext(req.Context(), &belowKey)
	if (!found) || (err != nil) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("Cannot read instance: %+v", belowKey)})
		return
	}

	canReplicate, err := instance.CanReplicateFromEx(belowInstance, "CanReplicateFrom()")
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("%t", canReplicate), Details: belowKey})
}

// CanReplicateFromGTID attempts to move an instance below another via GTID.
func (api *API) CanReplicateFromGTID(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
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
	belowKey, err := request.ResolveInstanceKey(params["belowHost"], params["belowPort"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	belowInstance, found, err := instinventory.ReadInstanceContext(req.Context(), &belowKey)
	if (!found) || (err != nil) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("Cannot read instance: %+v", belowKey)})
		return
	}

	canReplicate, err := instance.CanReplicateFromEx(belowInstance, "CanReplicateFromGTID()")
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	if !canReplicate {
		presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("%t", canReplicate), Details: belowKey})
		return
	}
	err = instrelocation.CheckMoveViaGTID(instance, belowInstance)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	canReplicate = (err == nil)

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("%t", canReplicate), Details: belowKey})
}

// setSemiSyncMaster
func (api *API) setSemiSyncMaster(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal, enable bool) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	instance, err := instreplication.SetSemiSyncMaster(&instanceKey, enable)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("master semi-sync set to %t", enable), Details: instance})
}

func (api *API) EnableSemiSyncMaster(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	api.setSemiSyncMaster(params, r, req, user, true)
}
func (api *API) DisableSemiSyncMaster(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	api.setSemiSyncMaster(params, r, req, user, false)
}

// setSemiSyncMaster
func (api *API) setSemiSyncReplica(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal, enable bool) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	instance, err := instreplication.SetSemiSyncReplica(&instanceKey, enable)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("replica semi-sync set to %t", enable), Details: instance})
}

func (api *API) EnableSemiSyncReplica(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	api.setSemiSyncReplica(params, r, req, user, true)
}

func (api *API) DisableSemiSyncReplica(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	api.setSemiSyncReplica(params, r, req, user, false)
}

// DelayReplication delays replication on given instance with given seconds
func (api *API) DelayReplication(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	seconds, err := strconv.Atoi(params["seconds"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Invalid value provided for seconds"})
		return
	}
	err = instreplication.DelayReplication(&instanceKey, seconds)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("Replication delayed: %+v", instanceKey), Details: seconds})
}

// SetReadOnly sets the global read_only variable
func (api *API) SetReadOnly(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	instance, err := instreplication.SetReadOnly(&instanceKey, true)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: "Server set as read-only", Details: instance})
}

// SetWriteable clear the global read_only variable
func (api *API) SetWriteable(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	instance, err := instreplication.SetReadOnly(&instanceKey, false)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: "Server set as writeable", Details: instance})
}

// KillQuery kills a query running on a server
func (api *API) KillQuery(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	processId, err := strconv.ParseInt(params["process"], 10, 0)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	instance, err := instreplication.KillQuery(&instanceKey, processId)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("Query killed on : %+v", instance.Key), Details: instance})
}

// AsciiTopology returns an ascii graph of cluster's instances
