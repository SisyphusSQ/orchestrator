package recovery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/openark/orchestrator/internal/http/authz"
	"github.com/openark/orchestrator/internal/http/contract"
	"github.com/openark/orchestrator/internal/http/presenter"
	"github.com/openark/orchestrator/internal/http/request"
	"github.com/openark/orchestrator/internal/http/transport"
	instanalysis "github.com/openark/orchestrator/internal/inst/analysis"
	instaudit "github.com/openark/orchestrator/internal/inst/audit"
	instcandidate "github.com/openark/orchestrator/internal/inst/candidate"
	instcluster "github.com/openark/orchestrator/internal/inst/cluster"
	instmodel "github.com/openark/orchestrator/internal/inst/instance"
	instinventory "github.com/openark/orchestrator/internal/inst/inventory"
	instmaintenance "github.com/openark/orchestrator/internal/inst/maintenance"
	logicrecovery "github.com/openark/orchestrator/internal/logic/recovery"
	"github.com/openark/orchestrator/internal/models/domain"
	"github.com/openark/orchestrator/internal/models/dto"
	orchos "github.com/openark/orchestrator/internal/os"
	orcraft "github.com/openark/orchestrator/internal/raft"
	"github.com/openark/orchestrator/internal/recoverypolicy"
)

// API contains failure analysis and recovery handlers.
type API struct{}

func (api *API) replicationAnalysis(clusterName string, instanceKey *instmodel.InstanceKey, params transport.Params, r transport.Responder, req *http.Request) {
	analysis, err := instanalysis.GetReplicationAnalysis(clusterName, &dto.ReplicationAnalysisHints{IncludeDowntimed: req.URL.Query().Get("includeDowntimed") != "false"})
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("Cannot get analysis: %+v", err)})
		return
	}
	// Possibly filter single instance
	if instanceKey != nil {
		filtered := analysis[:0]
		for _, analysisEntry := range analysis {
			if instanceKey.Equals(&analysisEntry.AnalyzedInstanceKey) {
				filtered = append(filtered, analysisEntry)
			}
		}
		analysis = filtered
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: "Analysis", Details: analysis})
}

// ReplicationAnalysis retuens list of issues
func (api *API) ReplicationAnalysis(params transport.Params, r transport.Responder, req *http.Request) {
	api.replicationAnalysis("", nil, params, r, req)
}

// ReplicationAnalysis retuens list of issues
func (api *API) ReplicationAnalysisForCluster(params transport.Params, r transport.Responder, req *http.Request) {
	clusterName, err := instcluster.DeduceClusterName(params["clusterName"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("Cannot get analysis: %+v", err)})
		return
	}
	if clusterName == "" {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("Cannot get cluster name: %+v", params["clusterName"])})
		return
	}
	api.replicationAnalysis(clusterName, nil, params, r, req)
}

// ReplicationAnalysis retuens list of issues
func (api *API) ReplicationAnalysisForKey(params transport.Params, r transport.Responder, req *http.Request) {
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("Cannot get analysis: %+v", err)})
		return
	}
	if !instanceKey.IsValid() {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("Cannot get analysis: invalid key %+v", instanceKey)})
		return
	}
	api.replicationAnalysis("", &instanceKey, params, r, req)
}

// RecoverLite attempts recovery on a given instance, without executing external processes
func (api *API) RecoverLite(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	params["skipProcesses"] = "true"
	api.Recover(params, r, req, user)
}

// Recover attempts recovery on a given instance
func (api *API) Recover(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	var candidateKey *instmodel.InstanceKey
	if key, err := request.ResolveInstanceKey(params["candidateHost"], params["candidatePort"]); err == nil {
		candidateKey = &key
	}

	skipProcesses := (req.URL.Query().Get("skipProcesses") == "true") || (params["skipProcesses"] == "true")
	recoveryAttempted, promotedInstanceKey, err := logicrecovery.CheckAndRecover(&instanceKey, candidateKey, skipProcesses)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err)), Details: instanceKey})
		return
	}
	if !recoveryAttempted {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Recovery not attempted", Details: instanceKey})
		return
	}
	if promotedInstanceKey == nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Recovery attempted but no instance promoted", Details: instanceKey})
		return
	}
	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("Recovery executed on %+v", instanceKey), Details: *promotedInstanceKey})
}

// GracefulMasterTakeover gracefully fails over a master onto its single replica.
func (api *API) gracefulMasterTakeover(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal, auto bool) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	clusterName, err := request.ClusterName(request.ClusterHint(params))
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	designatedKey, _ := request.ResolveInstanceKey(params["designatedHost"], params["designatedPort"])
	// designatedKey may be empty/invalid
	topologyRecovery, _, err := logicrecovery.GracefulMasterTakeover(clusterName, &designatedKey, auto)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err)), Details: topologyRecovery})
		return
	}
	if topologyRecovery == nil || topologyRecovery.SuccessorKey == nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "graceful-master-takeover: no successor promoted", Details: topologyRecovery})
		return
	}
	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: "graceful-master-takeover: successor promoted", Details: topologyRecovery})
}

// GracefulMasterTakeover gracefully fails over a master, either:
// - onto its single replica, or
// - onto a replica indicated by the user
func (api *API) GracefulMasterTakeover(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	api.gracefulMasterTakeover(params, r, req, user, false)
}

// GracefulMasterTakeoverAuto gracefully fails over a master onto a replica of orchestrator's choosing
func (api *API) GracefulMasterTakeoverAuto(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	api.gracefulMasterTakeover(params, r, req, user, true)
}

// ForceMasterFailover fails over a master (even if there's no particular problem with the master)
func (api *API) ForceMasterFailover(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	clusterName, err := request.ClusterName(request.ClusterHint(params))
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	topologyRecovery, err := logicrecovery.ForceMasterFailover(clusterName)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	if topologyRecovery.SuccessorKey != nil {
		presenter.Respond(r, &contract.Response{Code: contract.OK, Message: "Master failed over", Details: topologyRecovery})
	} else {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Master not failed over", Details: topologyRecovery})
	}
}

// ForceMasterTakeover fails over a master (even if there's no particular problem with the master)
func (api *API) ForceMasterTakeover(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	clusterName, err := request.ClusterName(request.ClusterHint(params))
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	designatedKey, _ := request.ResolveInstanceKey(params["designatedHost"], params["designatedPort"])
	designatedInstance, _, err := instinventory.ReadInstanceContext(req.Context(), &designatedKey)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	if designatedInstance == nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Instance not found"})
		return
	}

	topologyRecovery, err := logicrecovery.ForceMasterTakeover(clusterName, designatedInstance)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	if topologyRecovery.SuccessorKey != nil {
		presenter.Respond(r, &contract.Response{Code: contract.OK, Message: "Master failed over", Details: topologyRecovery})
	} else {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Master not failed over", Details: topologyRecovery})
	}
}

// Registers promotion preference for given instance
func (api *API) RegisterCandidate(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	promotionRule, err := instmodel.ParseCandidatePromotionRule(params["promotionRule"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	candidate := instcandidate.NewCandidateDatabaseInstance(&instanceKey, promotionRule).WithCurrentTime()

	_, err = orcraft.PublishCommand("register-candidate", candidate)

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: "Registered candidate", Details: instanceKey})
}

// AutomatedRecoveryFilters retuens list of clusters which are configured with automated recovery
func (api *API) AutomatedRecoveryFilters(params transport.Params, r transport.Responder, req *http.Request) {
	doc, err := recoverypolicy.GetPolicy(req.Context(), domain.ScopeGlobal, domain.GlobalKey)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error()})
		return
	}
	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: "Automated recovery configuration details", Details: doc.Effective})
}

// AuditFailureDetection provides list of topology_failure_detection entries
func (api *API) AuditFailureDetection(params transport.Params, r transport.Responder, req *http.Request) {

	var audits []*logicrecovery.TopologyRecovery
	var err error

	if detectionId, derr := strconv.ParseInt(params["id"], 10, 0); derr == nil && detectionId > 0 {
		audits, err = logicrecovery.ReadFailureDetection(detectionId)
	} else {
		page, derr := strconv.Atoi(params["page"])
		if derr != nil || page < 0 {
			page = 0
		}
		audits, err = logicrecovery.ReadRecentFailureDetections(params["clusterAlias"], page)
	}

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.WriteJSON(r, http.StatusOK, audits)
}

// AuditRecoverySteps returns audited steps of a given recovery
func (api *API) AuditRecoverySteps(params transport.Params, r transport.Responder, req *http.Request) {
	recoveryUID := params["uid"]
	audits, err := logicrecovery.ReadTopologyRecoverySteps(recoveryUID)

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.WriteJSON(r, http.StatusOK, audits)
}

// ReadReplicationAnalysisChangelog lists instances and their analysis changelog
func (api *API) ReadReplicationAnalysisChangelog(params transport.Params, r transport.Responder, req *http.Request) {
	changelogs, err := instanalysis.ReadReplicationAnalysisChangelog()

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.WriteJSON(r, http.StatusOK, changelogs)
}

// AuditRecovery provides list of topology-recovery entries
func (api *API) AuditRecovery(params transport.Params, r transport.Responder, req *http.Request) {
	var audits []*logicrecovery.TopologyRecovery
	var err error

	if recoveryUID := params["uid"]; recoveryUID != "" {
		audits, err = logicrecovery.ReadRecoveryByUID(recoveryUID)
	} else if recoveryId, derr := strconv.ParseInt(params["id"], 10, 0); derr == nil && recoveryId > 0 {
		audits, err = logicrecovery.ReadRecovery(recoveryId)
	} else {
		page, derr := strconv.Atoi(params["page"])
		if derr != nil || page < 0 {
			page = 0
		}
		unacknowledgedOnly := (req.URL.Query().Get("unacknowledged") == "true")

		audits, err = logicrecovery.ReadRecentRecoveries(params["clusterName"], params["clusterAlias"], unacknowledgedOnly, page)
	}

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.WriteJSON(r, http.StatusOK, audits)
}

// ActiveClusterRecovery returns recoveries in-progress for a given cluster
func (api *API) ActiveClusterRecovery(params transport.Params, r transport.Responder, req *http.Request) {
	recoveries, err := logicrecovery.ReadActiveClusterRecovery(params["clusterName"])

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.WriteJSON(r, http.StatusOK, recoveries)
}

// RecentlyActiveClusterRecovery returns recoveries in-progress for a given cluster
func (api *API) RecentlyActiveClusterRecovery(params transport.Params, r transport.Responder, req *http.Request) {
	recoveries, err := logicrecovery.ReadRecentlyActiveClusterRecovery(params["clusterName"])

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.WriteJSON(r, http.StatusOK, recoveries)
}

// RecentlyActiveClusterRecovery returns recoveries in-progress for a given cluster
func (api *API) RecentlyActiveInstanceRecovery(params transport.Params, r transport.Responder, req *http.Request) {
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	recoveries, err := logicrecovery.ReadRecentlyActiveInstanceRecovery(&instanceKey)

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.WriteJSON(r, http.StatusOK, recoveries)
}

// ClusterInfo provides details of a given cluster
func (api *API) AcknowledgeClusterRecoveries(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}

	var clusterName string
	var err error
	if params["clusterAlias"] != "" {
		clusterName, err = instcluster.GetClusterByAlias(params["clusterAlias"])
	} else {
		clusterName, err = request.ClusterName(request.ClusterHint(params))
	}

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	comment := strings.TrimSpace(req.URL.Query().Get("comment"))
	if comment == "" {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "No acknowledge comment given"})
		return
	}
	userId := authz.UserID(req, user)
	if userId == "" {
		userId = instmaintenance.GetMaintenanceOwner()
	}

	ack := logicrecovery.NewRecoveryAcknowledgement(userId, comment)
	ack.ClusterName = clusterName
	_, err = orcraft.PublishCommand("ack-recovery", ack)

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: "Acknowledged cluster recoveries", Details: clusterName})
}

// ClusterInfo provides details of a given cluster
func (api *API) AcknowledgeInstanceRecoveries(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}

	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	comment := strings.TrimSpace(req.URL.Query().Get("comment"))
	if comment == "" {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "No acknowledge comment given"})
		return
	}
	userId := authz.UserID(req, user)
	if userId == "" {
		userId = instmaintenance.GetMaintenanceOwner()
	}

	ack := logicrecovery.NewRecoveryAcknowledgement(userId, comment)
	ack.Key = instanceKey
	_, err = orcraft.PublishCommand("ack-recovery", ack)

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: "Acknowledged instance recoveries", Details: instanceKey})
}

// ClusterInfo provides details of a given cluster
func (api *API) AcknowledgeRecovery(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	var err error
	var recoveryId int64
	var idParam string

	// Ack either via id or uid
	recoveryUid := params["uid"]
	if recoveryUid == "" {
		idParam = params["recoveryId"]
		recoveryId, err = strconv.ParseInt(idParam, 10, 0)
		if err != nil {
			presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
			return
		}
	} else {
		idParam = recoveryUid
	}
	comment := strings.TrimSpace(req.URL.Query().Get("comment"))
	if comment == "" {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "No acknowledge comment given"})
		return
	}
	userId := authz.UserID(req, user)
	if userId == "" {
		userId = instmaintenance.GetMaintenanceOwner()
	}

	ack := logicrecovery.NewRecoveryAcknowledgement(userId, comment)
	ack.Id = recoveryId
	ack.UID = recoveryUid
	_, err = orcraft.PublishCommand("ack-recovery", ack)

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: "Acknowledged recovery", Details: idParam})
}

// ClusterInfo provides details of a given cluster
func (api *API) AcknowledgeAllRecoveries(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}

	comment := strings.TrimSpace(req.URL.Query().Get("comment"))
	if comment == "" {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "No acknowledge comment given"})
		return
	}
	userId := authz.UserID(req, user)
	if userId == "" {
		userId = instmaintenance.GetMaintenanceOwner()
	}
	var err error

	ack := logicrecovery.NewRecoveryAcknowledgement(userId, comment)
	ack.AllRecoveries = true
	_, err = orcraft.PublishCommand("ack-recovery", ack)

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: "Acknowledged all recoveries", Details: comment})
}

// BlockedRecoveries reads list of currently blocked recoveries, optionally filtered by cluster name
func (api *API) BlockedRecoveries(params transport.Params, r transport.Responder, req *http.Request) {
	blockedRecoveries, err := logicrecovery.ReadBlockedRecoveries(params["clusterName"])

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.WriteJSON(r, http.StatusOK, blockedRecoveries)
}

// DisableGlobalRecoveries globally disables recoveries
func (api *API) DisableGlobalRecoveries(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}

	var err error

	_, err = orcraft.PublishCommand("disable-global-recoveries", 0)

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: "Globally disabled recoveries", Details: "disabled"})
}

// EnableGlobalRecoveries globally enables recoveries
func (api *API) EnableGlobalRecoveries(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}

	var err error

	_, err = orcraft.PublishCommand("enable-global-recoveries", 0)

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: "Globally enabled recoveries", Details: "enabled"})
}

// CheckGlobalRecoveries checks whether
func (api *API) CheckGlobalRecoveries(params transport.Params, r transport.Responder, req *http.Request) {
	isDisabled, err := logicrecovery.IsRecoveryDisabled()

	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	details := "enabled"
	if isDisabled {
		details = "disabled"
	}
	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("Global recoveries %+v", details), Details: details})
}

func decodeConfigurationBody(req *http.Request, target any) error {
	const maxBodyBytes = 1 << 20
	body, err := io.ReadAll(io.LimitReader(req.Body, maxBodyBytes+1))
	if err != nil {
		return fmt.Errorf("read JSON body: %w", err)
	}
	if len(body) > maxBodyBytes {
		return fmt.Errorf("JSON body exceeds %d bytes", maxBodyBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("invalid trailing JSON content")
	}
	return nil
}

func configurationUser(req *http.Request, user transport.Principal) string {
	if id := authz.UserID(req, user); id != "" {
		return id
	}
	return "local-session"
}

func (api *API) RecoveryPolicy(params transport.Params, r transport.Responder, req *http.Request) {
	doc, err := recoverypolicy.GetPolicy(req.Context(), params["scopeType"], params["scopeKey"])
	if err != nil {
		presenter.RespondStatus(r, http.StatusBadRequest, &contract.Response{Code: contract.ERROR, Message: err.Error()})
		return
	}
	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: "Recovery policy", Details: doc})
}

func (api *API) SaveRecoveryPolicy(_ transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForConfiguration(req, user) {
		presenter.RespondStatus(r, http.StatusForbidden, &contract.Response{Code: contract.ERROR, Message: "Configuration administrator permission required"})
		return
	}
	var command dto.SaveRecoveryPolicyCommand
	if err := decodeConfigurationBody(req, &command); err != nil {
		presenter.RespondStatus(r, http.StatusBadRequest, &contract.Response{Code: contract.ERROR, Message: err.Error()})
		return
	}
	command.UpdatedBy = configurationUser(req, user)
	if strings.TrimSpace(command.ChangeReason) == "" {
		presenter.RespondStatus(r, http.StatusBadRequest, &contract.Response{Code: contract.ERROR, Message: "changeReason is required"})
		return
	}
	if _, err := orcraft.PublishCommand("save-recovery-policy", command); err != nil {
		status := http.StatusInternalServerError
		if recoverypolicy.IsRevisionConflict(err) {
			status = http.StatusConflict
		}
		presenter.RespondStatus(r, status, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	doc, err := recoverypolicy.GetPolicy(req.Context(), command.ScopeType, command.ScopeKey)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error()})
		return
	}
	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: "Recovery policy saved", Details: doc})
}

func (api *API) RecoveryHookProfiles(_ transport.Params, r transport.Responder, req *http.Request) {
	profiles, err := recoverypolicy.ListHookProfiles(req.Context())
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error()})
		return
	}
	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: "Recovery hook profiles", Details: profiles})
}

func (api *API) SaveRecoveryHookProfile(_ transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForConfiguration(req, user) {
		presenter.RespondStatus(r, http.StatusForbidden, &contract.Response{Code: contract.ERROR, Message: "Configuration administrator permission required"})
		return
	}
	var command dto.SaveRecoveryHookProfileCommand
	if err := decodeConfigurationBody(req, &command); err != nil {
		presenter.RespondStatus(r, http.StatusBadRequest, &contract.Response{Code: contract.ERROR, Message: err.Error()})
		return
	}
	command.Profile.UpdatedBy = configurationUser(req, user)
	if strings.TrimSpace(command.Profile.ChangeReason) == "" {
		presenter.RespondStatus(r, http.StatusBadRequest, &contract.Response{Code: contract.ERROR, Message: "changeReason is required"})
		return
	}
	if _, err := orcraft.PublishCommand("save-recovery-hook-profile", command); err != nil {
		status := http.StatusInternalServerError
		if recoverypolicy.IsRevisionConflict(err) {
			status = http.StatusConflict
		}
		presenter.RespondStatus(r, status, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	profiles, err := recoverypolicy.ListHookProfiles(req.Context())
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error()})
		return
	}
	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: "Recovery hook profile saved", Details: profiles})
}

func (api *API) TestRecoveryHookProfile(_ transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForConfiguration(req, user) {
		presenter.RespondStatus(r, http.StatusForbidden, &contract.Response{Code: contract.ERROR, Message: "Configuration administrator permission required"})
		return
	}
	var request struct {
		ProfileID string `json:"profileId"`
	}
	if err := decodeConfigurationBody(req, &request); err != nil {
		presenter.RespondStatus(r, http.StatusBadRequest, &contract.Response{Code: contract.ERROR, Message: err.Error()})
		return
	}
	profiles, err := recoverypolicy.ListHookProfiles(req.Context())
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error()})
		return
	}
	var selected *domain.RecoveryHookProfile
	for index := range profiles {
		if profiles[index].ID == request.ProfileID {
			selected = &profiles[index]
			break
		}
	}
	if selected == nil || !selected.Enabled {
		presenter.RespondStatus(r, http.StatusNotFound, &contract.Response{Code: contract.ERROR, Message: "enabled hook profile not found"})
		return
	}
	type result struct {
		Command  int    `json:"command"`
		Duration string `json:"duration"`
		Output   string `json:"output"`
		Error    string `json:"error,omitempty"`
	}
	results := make([]result, 0, len(selected.Commands))
	for index, command := range selected.Commands {
		commandCtx, cancel := context.WithTimeout(req.Context(), time.Duration(selected.TimeoutSeconds)*time.Second)
		start := time.Now()
		output, commandErr := orchos.CommandRunContext(commandCtx, command, os.Environ(), selected.OutputLimitBytes)
		cancel()
		item := result{Command: index + 1, Duration: time.Since(start).Round(time.Millisecond).String(), Output: recoverypolicy.RedactOutput(output)}
		if commandErr != nil {
			item.Error = commandErr.Error()
		}
		results = append(results, item)
		instaudit.AuditOperation("test-recovery-hook", nil, fmt.Sprintf("profile=%s revision=%d command=%d error=%v", selected.ID, selected.Revision, index+1, commandErr))
		if commandErr != nil && selected.FailurePolicy == "abort" {
			break
		}
	}
	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: "Recovery hook test completed", Details: results})
}

func (api *API) RecoveryHookAssignments(params transport.Params, r transport.Responder, req *http.Request) {
	assignments, err := recoverypolicy.ListHookAssignments(req.Context(), params["scopeType"], params["scopeKey"])
	if err != nil {
		presenter.RespondStatus(r, http.StatusBadRequest, &contract.Response{Code: contract.ERROR, Message: err.Error()})
		return
	}
	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: "Recovery hook assignments", Details: assignments})
}

func (api *API) SaveRecoveryHookAssignment(_ transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForConfiguration(req, user) {
		presenter.RespondStatus(r, http.StatusForbidden, &contract.Response{Code: contract.ERROR, Message: "Configuration administrator permission required"})
		return
	}
	var command dto.SaveRecoveryHookAssignmentCommand
	if err := decodeConfigurationBody(req, &command); err != nil {
		presenter.RespondStatus(r, http.StatusBadRequest, &contract.Response{Code: contract.ERROR, Message: err.Error()})
		return
	}
	command.Assignment.UpdatedBy = configurationUser(req, user)
	if strings.TrimSpace(command.Assignment.ChangeReason) == "" {
		presenter.RespondStatus(r, http.StatusBadRequest, &contract.Response{Code: contract.ERROR, Message: "changeReason is required"})
		return
	}
	if _, err := orcraft.PublishCommand("save-recovery-hook-assignment", command); err != nil {
		status := http.StatusInternalServerError
		if recoverypolicy.IsRevisionConflict(err) {
			status = http.StatusConflict
		}
		presenter.RespondStatus(r, status, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	assignments, err := recoverypolicy.ListHookAssignments(req.Context(), command.Assignment.ScopeType, command.Assignment.ScopeKey)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error()})
		return
	}
	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: "Recovery hook assignment saved", Details: assignments})
}
