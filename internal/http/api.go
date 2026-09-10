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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/openark/orchestrator/internal/golib/log"
	"github.com/openark/orchestrator/internal/golib/util"

	fqdn "github.com/Showmax/go-fqdn"
	"github.com/openark/orchestrator/internal/agent"
	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/inst"
	"github.com/openark/orchestrator/internal/logic"
	"github.com/openark/orchestrator/internal/models/domain"
	"github.com/openark/orchestrator/internal/models/dto"
	orchos "github.com/openark/orchestrator/internal/os"
	"github.com/openark/orchestrator/internal/process"
	orcraft "github.com/openark/orchestrator/internal/raft"
	"github.com/openark/orchestrator/internal/recoverypolicy"
)

// APIResponseCode is an OK/ERROR response code
type APIResponseCode int

const (
	ERROR APIResponseCode = iota
	OK
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
var emptyInstanceKey inst.InstanceKey

func (code *APIResponseCode) MarshalJSON() ([]byte, error) {
	return json.Marshal(code.String())
}

func (code *APIResponseCode) String() string {
	switch *code {
	case ERROR:
		return "ERROR"
	case OK:
		return "OK"
	}
	return "unknown"
}

// HttpStatus returns the respective HTTP status for this response
func (code *APIResponseCode) HttpStatus() int {
	switch *code {
	case ERROR:
		return http.StatusInternalServerError
	case OK:
		return http.StatusOK
	}
	return http.StatusNotImplemented
}

// APIResponse is a response returned as JSON to various requests.
type APIResponse struct {
	Code       APIResponseCode
	Message    string
	Details    any
	ErrorClass string `json:",omitempty"`
}

var messagePrefix string

func Respond(r Responder, apiResponse *APIResponse) {
	RespondStatus(r, apiResponse.Code.HttpStatus(), apiResponse)
}

func RespondStatus(r Responder, status int, apiResponse *APIResponse) {
	apiResponse.Message = fmt.Sprintf("%+v%+v", messagePrefix, apiResponse.Message)
	writeHTTPJSON(r, status, apiResponse)
}

func setupMessagePrefix() {
	act := config.Config.Server.ResponseIdentity.Mode
	if act == "" || act == "none" {
		return
	}
	if act != "FQDN" && act != "hostname" && act != "custom" {
		log.Warning("PrependMessagesWithOrcIdentity option has unsupported value '%+v'")
		return
	}

	var hostname string
	var err error
	fallbackActive := false

	if act == "FQDN" {
		if hostname, err = fqdn.FqdnHostname(); err != nil {
			log.Warning("Failed to get Orchestrator's FQDN. Falling back to hostname.")
			hostname = ""
			fallbackActive = true
		}
	}
	if fallbackActive || act == "hostname" {
		fallbackActive = false
		if hostname, err = os.Hostname(); err != nil {
			log.Warning("Failed to get Orchestrator's FQDN. Falling back to custom prefix (if provided).")
			hostname = ""
			fallbackActive = true
		}
	}
	if (fallbackActive || act == "custom") && config.Config.Server.ResponseIdentity.Custom != "" {
		hostname = config.Config.Server.ResponseIdentity.Custom
	}
	if hostname != "" {
		messagePrefix = fmt.Sprintf("Orchestrator %+v says: ", hostname)
	} else {
		log.Warning("Prepending messages with Orchestrator identity was requested, but identity cannot be determined. Skipping prefix.")
	}
}

type HttpAPI struct {
	URLPrefix string
}

var API HttpAPI

func (api *HttpAPI) getInstanceKeyInternal(host string, port string, resolve bool) (inst.InstanceKey, error) {
	var instanceKey *inst.InstanceKey
	var err error
	if resolve {
		instanceKey, err = inst.NewResolveInstanceKeyStrings(host, port)
	} else {
		instanceKey, err = inst.NewRawInstanceKeyStrings(host, port)
	}
	if err != nil {
		return emptyInstanceKey, err
	}
	instanceKey, err = inst.FigureInstanceKey(instanceKey, nil)
	if err != nil {
		return emptyInstanceKey, err
	}
	if instanceKey == nil {
		return emptyInstanceKey, fmt.Errorf("unexpected nil instanceKey in getInstanceKeyInternal(%+v, %+v, %+v)", host, port, resolve)
	}
	return *instanceKey, nil
}

func (api *HttpAPI) getInstanceKey(host string, port string) (inst.InstanceKey, error) {
	return api.getInstanceKeyInternal(host, port, true)
}

func (api *HttpAPI) getNoResolveInstanceKey(host string, port string) (inst.InstanceKey, error) {
	return api.getInstanceKeyInternal(host, port, false)
}

func getTag(params Params, req *http.Request) (tag *inst.Tag, err error) {
	tagString := req.URL.Query().Get("tag")
	if tagString != "" {
		return inst.ParseTag(tagString)
	}
	return inst.NewTag(params["tagName"], params["tagValue"])
}

func (api *HttpAPI) getBinlogCoordinates(logFile string, logPos string) (inst.BinlogCoordinates, error) {
	coordinates := inst.BinlogCoordinates{LogFile: logFile}
	var err error
	if coordinates.LogPos, err = strconv.ParseInt(logPos, 10, 0); err != nil {
		return coordinates, fmt.Errorf("invalid logPos: %s", logPos)
	}

	return coordinates, err
}

// InstanceReplicas lists all replicas of given instance
func (api *HttpAPI) InstanceReplicas(params Params, r Responder, req *http.Request) {
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	replicas, err := inst.ReadReplicaInstances(&instanceKey)

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("Cannot read instance: %+v", instanceKey)})
		return
	}
	writeHTTPJSON(r, http.StatusOK, replicas)
}

// Instance reads and returns an instance's details.
func (api *HttpAPI) Instance(params Params, r Responder, req *http.Request) {
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	instance, found, err := inst.ReadInstanceContext(req.Context(), &instanceKey)
	if (!found) || (err != nil) {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("Cannot read instance: %+v", instanceKey)})
		return
	}
	writeHTTPJSON(r, http.StatusOK, instance)
}

// AsyncDiscover issues an asynchronous read on an instance. This is
// useful for bulk loads of a new set of instances and will not block
// if the instance is slow to respond or not reachable.
func (api *HttpAPI) AsyncDiscover(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	go api.Discover(params, r, req, user)

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("Asynchronous discovery initiated for Instance: %+v", instanceKey)})
}

// Discover issues a synchronous read on an instance
func (api *HttpAPI) Discover(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	instance, err := inst.ReadTopologyInstanceContext(req.Context(), &instanceKey)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	if _, err := orcraft.PublishCommand("discover", instanceKey); err != nil {
		respondRaft(r, err, "", nil)
		return
	}

	if instance != nil {
		Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("Instance discovered: %+v", instance.Key), Details: instance})
	} else {
		Respond(r, &APIResponse{Code: OK, Message: "No instances discovered", Details: nil})
	}
}

// Refresh synchronuously re-reads a topology instance
func (api *HttpAPI) Refresh(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	_, err = inst.RefreshTopologyInstance(&instanceKey)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("Instance refreshed: %+v", instanceKey), Details: instanceKey})
}

// Forget removes an instance entry fro backend database
func (api *HttpAPI) Forget(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getNoResolveInstanceKey(params["host"], params["port"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	_, err = orcraft.PublishCommand("forget", instanceKey)

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("Instance forgotten: %+v", instanceKey), Details: instanceKey})
}

// ForgetCluster forgets all instacnes of a cluster
func (api *HttpAPI) ForgetCluster(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	clusterName, err := figureClusterName(getClusterHint(params))
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	if _, err := orcraft.PublishCommand("forget-cluster", clusterName); err != nil {
		respondRaft(r, err, "", nil)
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("Cluster forgotten: %+v", clusterName)})
}

// Resolve tries to resolve hostname and then checks to see if port is open on that host.
func (api *HttpAPI) Resolve(params Params, r Responder, req *http.Request) {
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	if conn, err := net.Dial("tcp", instanceKey.DisplayString()); err == nil {
		conn.Close()
	} else {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: "Instance resolved", Details: instanceKey})
}

// BeginMaintenance begins maintenance mode for given instance
func (api *HttpAPI) BeginMaintenance(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	durationSeconds := 0
	if duration := req.URL.Query().Get("duration"); duration != "" {
		durationSeconds, err = util.SimpleTimeToSeconds(duration)
		if err != nil || durationSeconds < 0 {
			Respond(r, &APIResponse{Code: ERROR, Message: "Invalid maintenance duration"})
			return
		}
	}
	key, err := inst.BeginBoundedMaintenance(&instanceKey, params["owner"], params["reason"], uint(durationSeconds), true)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err)), Details: key})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("Maintenance begun: %+v", instanceKey), Details: instanceKey})
}

// EndMaintenance terminates maintenance mode
func (api *HttpAPI) EndMaintenance(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	maintenanceKey, err := strconv.ParseInt(params["maintenanceKey"], 10, 0)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	_, err = inst.EndMaintenance(maintenanceKey)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("Maintenance ended: %+v", maintenanceKey), Details: maintenanceKey})
}

// EndMaintenanceByInstanceKey terminates maintenance mode for given instance
func (api *HttpAPI) EndMaintenanceByInstanceKey(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	_, err = inst.EndMaintenanceByInstanceKey(&instanceKey)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("Maintenance ended: %+v", instanceKey), Details: instanceKey})
}

// EndMaintenanceByInstanceKey terminates maintenance mode for given instance
func (api *HttpAPI) InMaintenance(params Params, r Responder, req *http.Request, user Principal) {
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	inMaintenance, err := inst.InMaintenance(&instanceKey)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	responseDetails := ""
	if inMaintenance {
		responseDetails = instanceKey.StringCode()
	}
	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("%+v", inMaintenance), Details: responseDetails})
}

// Maintenance provides list of instance under active maintenance
func (api *HttpAPI) Maintenance(params Params, r Responder, req *http.Request) {
	maintenanceList, err := inst.ReadActiveMaintenance()

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	writeHTTPJSON(r, http.StatusOK, maintenanceList)
}

// BeginDowntime sets a downtime flag with default duration
func (api *HttpAPI) BeginDowntime(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	var durationSeconds int = 0
	if params["duration"] != "" {
		durationSeconds, err = util.SimpleTimeToSeconds(params["duration"])
		if durationSeconds < 0 {
			err = fmt.Errorf("duration value must be non-negative. Given value: %d", durationSeconds)
		}
		if err != nil {
			Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
			return
		}
	}
	duration := time.Duration(durationSeconds) * time.Second
	downtime := inst.NewDowntime(&instanceKey, params["owner"], params["reason"], duration)

	_, err = orcraft.PublishCommand("begin-downtime", downtime)

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err)), Details: instanceKey})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("Downtime begun: %+v", instanceKey), Details: instanceKey})
}

// EndDowntime terminates downtime (removes downtime flag) for an instance
func (api *HttpAPI) EndDowntime(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	_, err = orcraft.PublishCommand("end-downtime", instanceKey)

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("Downtime ended: %+v", instanceKey), Details: instanceKey})
}

// MoveUp attempts to move an instance up the topology
func (api *HttpAPI) MoveUp(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	instance, err := inst.MoveUp(&instanceKey)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("Instance %+v moved up", instanceKey), Details: instance})
}

// MoveUpReplicas attempts to move up all replicas of an instance
func (api *HttpAPI) MoveUpReplicas(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	replicas, newMaster, errs, err := inst.MoveUpReplicas(&instanceKey, req.URL.Query().Get("pattern"))
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("Moved up %d replicas of %+v below %+v; %d errors: %+v", len(replicas), instanceKey, newMaster.Key, len(errs), errs), Details: replicas})
}

// Repoint positiones a replica under another (or same) master with exact same coordinates.
// Useful for binlog servers
func (api *HttpAPI) Repoint(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	var belowKey *inst.InstanceKey
	if params["belowHost"] != "" {
		key, err := api.getInstanceKey(params["belowHost"], params["belowPort"])
		if err != nil {
			Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
			return
		}
		belowKey = &key
	}

	instance, err := inst.Repoint(&instanceKey, belowKey, domain.GTIDHintNeutral)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("Instance %+v repointed below %+v", instanceKey, belowKey), Details: instance})
}

// MoveUpReplicas attempts to move up all replicas of an instance
func (api *HttpAPI) RepointReplicas(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	var destination *inst.InstanceKey
	if raw := req.URL.Query().Get("destination"); raw != "" {
		destination, err = inst.ParseResolveInstanceKey(raw)
		if err != nil {
			Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
			return
		}
	}
	replicas, _, err := inst.RepointReplicasTo(&instanceKey, req.URL.Query().Get("pattern"), destination)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("Repointed %d replicas of %+v", len(replicas), instanceKey), Details: replicas})
}

// MakeCoMaster attempts to make an instance co-master with its own master
func (api *HttpAPI) MakeCoMaster(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	instance, err := inst.MakeCoMaster(&instanceKey)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("Instance made co-master: %+v", instance.Key), Details: instance})
}

// ResetReplication makes a replica forget about its master, effectively breaking the replication
func (api *HttpAPI) ResetReplication(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	instance, err := inst.ResetReplicationOperation(&instanceKey)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("Replica reset on %+v", instance.Key), Details: instance})
}

// ChangeMasterCredentials re-applies replication user/password (and SSL material supplied via
// ReplicationCredentialsQuery) on an instance while preserving its existing SOURCE_SSL/TLS
// configuration. Useful for credential rotation and for exercising the TLS-preservation path.
func (api *HttpAPI) ChangeMasterCredentials(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	creds, err := inst.ReadReplicationCredentials(&instanceKey)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	instance, err := inst.ChangeMasterCredentials(&instanceKey, creds)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("Replication credentials re-applied on %+v", instance.Key), Details: instance})
}

// DetachReplicaMasterHost detaches a replica from its master by setting an invalid
// (yet revertible) host name
func (api *HttpAPI) DetachReplicaMasterHost(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	instance, err := inst.DetachReplicaMasterHost(&instanceKey)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("Replica detached: %+v", instance.Key), Details: instance})
}

// ReattachReplicaMasterHost reverts a detachReplicaMasterHost command
// by resoting the original master hostname in CHANGE MASTER TO
func (api *HttpAPI) ReattachReplicaMasterHost(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	instance, err := inst.ReattachReplicaMasterHost(&instanceKey)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("Replica reattached: %+v", instance.Key), Details: instance})
}

// EnableGTID attempts to enable GTID on a replica
func (api *HttpAPI) EnableGTID(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	instance, err := inst.EnableGTID(&instanceKey)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("Enabled GTID on %+v", instance.Key), Details: instance})
}

// DisableGTID attempts to disable GTID on a replica, and revert to binlog file:pos
func (api *HttpAPI) DisableGTID(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	instance, err := inst.DisableGTID(&instanceKey)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("Disabled GTID on %+v", instance.Key), Details: instance})
}

// LocateErrantGTID identifies the binlog positions for errant GTIDs on an instance
func (api *HttpAPI) LocateErrantGTID(params Params, r Responder, req *http.Request, user Principal) {
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	errantBinlogs, err := inst.LocateErrantGTID(&instanceKey)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	Respond(r, &APIResponse{Code: OK, Message: "located errant GTID", Details: errantBinlogs})
}

// ErrantGTIDResetMaster removes errant transactions on a server by way of RESET MASTER
func (api *HttpAPI) ErrantGTIDResetMaster(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	instance, err := inst.ErrantGTIDResetMaster(&instanceKey)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("Removed errant GTID on %+v and issued a RESET MASTER", instance.Key), Details: instance})
}

// ErrantGTIDInjectEmpty removes errant transactions by injecting and empty transaction on the cluster's master
func (api *HttpAPI) ErrantGTIDInjectEmpty(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	instance, clusterMaster, countInjectedTransactions, err := inst.ErrantGTIDInjectEmpty(&instanceKey)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("Have injected %+v transactions on cluster master %+v", countInjectedTransactions, clusterMaster.Key), Details: instance})
}

// MoveBelow attempts to move an instance below its supposed sibling
func (api *HttpAPI) MoveBelow(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	siblingKey, err := api.getInstanceKey(params["siblingHost"], params["siblingPort"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	instance, err := inst.MoveBelow(&instanceKey, &siblingKey)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("Instance %+v moved below %+v", instanceKey, siblingKey), Details: instance})
}

// MoveBelowGTID attempts to move an instance below another, via GTID
func (api *HttpAPI) MoveBelowGTID(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	belowKey, err := api.getInstanceKey(params["belowHost"], params["belowPort"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	instance, err := inst.MoveBelowGTID(&instanceKey, &belowKey)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("Instance %+v moved below %+v via GTID", instanceKey, belowKey), Details: instance})
}

// MoveReplicasGTID attempts to move an instance below another, via GTID
func (api *HttpAPI) MoveReplicasGTID(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	belowKey, err := api.getInstanceKey(params["belowHost"], params["belowPort"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	movedReplicas, _, errs, err := inst.MoveReplicasGTID(&instanceKey, &belowKey, req.URL.Query().Get("pattern"))
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("Moved %d replicas of %+v below %+v via GTID; %d errors: %+v", len(movedReplicas), instanceKey, belowKey, len(errs), errs), Details: belowKey})
}

// TakeSiblings
func (api *HttpAPI) TakeSiblings(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	instance, count, err := inst.TakeSiblings(&instanceKey)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("Took %d siblings of %+v", count, instanceKey), Details: instance})
}

// TakeMaster
func (api *HttpAPI) TakeMaster(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	instance, err := inst.TakeMaster(&instanceKey, false)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("%+v took its master", instanceKey), Details: instance})
}

// RelocateBelow attempts to move an instance below another, orchestrator choosing the best (potentially multi-step)
// relocation method
func (api *HttpAPI) RelocateBelow(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	belowKey, err := api.getInstanceKey(params["belowHost"], params["belowPort"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	instance, err := inst.RelocateBelow(&instanceKey, &belowKey)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("Instance %+v relocated below %+v", instanceKey, belowKey), Details: instance})
}

// Relocates attempts to smartly relocate replicas of a given instance below another
func (api *HttpAPI) RelocateReplicas(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	belowKey, err := api.getInstanceKey(params["belowHost"], params["belowPort"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	replicas, _, errs, err := inst.RelocateReplicas(&instanceKey, &belowKey, req.URL.Query().Get("pattern"))
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("Relocated %d replicas of %+v below %+v; %d errors: %+v", len(replicas), instanceKey, belowKey, len(errs), errs), Details: replicas})
}

// MoveEquivalent attempts to move an instance below another, baseed on known equivalence master coordinates
func (api *HttpAPI) MoveEquivalent(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	belowKey, err := api.getInstanceKey(params["belowHost"], params["belowPort"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	instance, err := inst.MoveEquivalent(&instanceKey, &belowKey)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("Instance %+v relocated via equivalence coordinates below %+v", instanceKey, belowKey), Details: instance})
}

// LastPseudoGTID attempts to find the last pseugo-gtid entry in an instance
func (api *HttpAPI) LastPseudoGTID(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	instance, found, err := inst.ReadInstanceContext(req.Context(), &instanceKey)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	if instance == nil || !found {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("Instance not found: %+v", instanceKey)})
		return
	}
	coordinates, text, err := inst.FindLastPseudoGTIDEntry(instance, instance.RelaylogCoordinates, nil, req.URL.Query().Get("strict") == "true", nil)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("%+v", *coordinates), Details: text})
}

// MatchBelow attempts to move an instance below another via pseudo GTID matching of binlog entries
func (api *HttpAPI) MatchBelow(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	belowKey, err := api.getInstanceKey(params["belowHost"], params["belowPort"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	instance, matchedCoordinates, err := inst.MatchBelow(&instanceKey, &belowKey, true)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("Instance %+v matched below %+v at %+v", instanceKey, belowKey, *matchedCoordinates), Details: instance})
}

// MatchBelow attempts to move an instance below another via pseudo GTID matching of binlog entries
func (api *HttpAPI) MatchUp(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	instance, matchedCoordinates, err := inst.MatchUp(&instanceKey, true)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("Instance %+v matched up at %+v", instanceKey, *matchedCoordinates), Details: instance})
}

// MultiMatchReplicas attempts to match all replicas of a given instance below another, efficiently
func (api *HttpAPI) MultiMatchReplicas(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	belowKey, err := api.getInstanceKey(params["belowHost"], params["belowPort"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	replicas, newMaster, errs, err := inst.MultiMatchReplicas(&instanceKey, &belowKey, req.URL.Query().Get("pattern"))
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("Matched %d replicas of %+v below %+v; %d errors: %+v", len(replicas), instanceKey, newMaster.Key, len(errs), errs), Details: newMaster.Key})
}

// MatchUpReplicas attempts to match up all replicas of an instance
func (api *HttpAPI) MatchUpReplicas(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	replicas, newMaster, errs, err := inst.MatchUpReplicas(&instanceKey, req.URL.Query().Get("pattern"))
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("Matched up %d replicas of %+v below %+v; %d errors: %+v", len(replicas), instanceKey, newMaster.Key, len(errs), errs), Details: newMaster.Key})
}

// RegroupReplicas attempts to pick a replica of a given instance and make it take its siblings, using any
// method possible (GTID, Pseudo-GTID, binlog servers)
func (api *HttpAPI) RegroupReplicas(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	lostReplicas, equalReplicas, aheadReplicas, cannotReplicateReplicas, promotedReplica, err := inst.RegroupReplicas(&instanceKey, false, nil, nil)
	lostReplicas = append(lostReplicas, cannotReplicateReplicas...)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("promoted replica: %s, lost: %d, trivial: %d, pseudo-gtid: %d",
		promotedReplica.Key.DisplayString(), len(lostReplicas), len(equalReplicas), len(aheadReplicas)), Details: promotedReplica.Key})
}

// RegroupReplicas attempts to pick a replica of a given instance and make it take its siblings, efficiently,
// using pseudo-gtid if necessary
func (api *HttpAPI) RegroupReplicasPseudoGTID(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	lostReplicas, equalReplicas, aheadReplicas, cannotReplicateReplicas, promotedReplica, err := inst.RegroupReplicasPseudoGTID(&instanceKey, false, nil, nil, nil)
	lostReplicas = append(lostReplicas, cannotReplicateReplicas...)

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("promoted replica: %s, lost: %d, trivial: %d, pseudo-gtid: %d",
		promotedReplica.Key.DisplayString(), len(lostReplicas), len(equalReplicas), len(aheadReplicas)), Details: promotedReplica.Key})
}

// RegroupReplicasGTID attempts to pick a replica of a given instance and make it take its siblings, efficiently, using GTID
func (api *HttpAPI) RegroupReplicasGTID(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	lostReplicas, movedReplicas, cannotReplicateReplicas, promotedReplica, err := inst.RegroupReplicasGTID(&instanceKey, false, true, nil, nil, nil)
	lostReplicas = append(lostReplicas, cannotReplicateReplicas...)

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("promoted replica: %s, lost: %d, moved: %d",
		promotedReplica.Key.DisplayString(), len(lostReplicas), len(movedReplicas)), Details: promotedReplica.Key})
}

// RegroupReplicasBinlogServers attempts to pick a replica of a given instance and make it take its siblings, efficiently, using GTID
func (api *HttpAPI) RegroupReplicasBinlogServers(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	_, promotedBinlogServer, err := inst.RegroupReplicasBinlogServers(&instanceKey, false)

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("promoted binlog server: %s",
		promotedBinlogServer.Key.DisplayString()), Details: promotedBinlogServer.Key})
}

// MakeMaster attempts to make the given instance a master, and match its siblings to be its replicas
func (api *HttpAPI) MakeMaster(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	instance, err := inst.MakeMaster(&instanceKey)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("Instance %+v now made master", instanceKey), Details: instance})
}

// MakeLocalMaster attempts to make the given instance a local master: take over its master by
// enslaving its siblings and replicating from its grandparent.
func (api *HttpAPI) MakeLocalMaster(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	instance, err := inst.MakeLocalMaster(&instanceKey)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("Instance %+v now made local master", instanceKey), Details: instance})
}

// SkipQuery skips a single query on a failed replication instance
func (api *HttpAPI) SkipQuery(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	instance, err := inst.SkipQuery(&instanceKey)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("Query skipped on %+v", instance.Key), Details: instance})
}

// StartReplication starts replication on given instance
func (api *HttpAPI) StartReplication(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	instance, err := inst.StartReplication(&instanceKey)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("Replica started: %+v", instance.Key), Details: instance})
}

// RestartReplication stops & starts replication on given instance
func (api *HttpAPI) RestartReplication(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	instance, err := inst.RestartReplication(&instanceKey)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("Replica restarted: %+v", instance.Key), Details: instance})
}

// StopReplication stops replication on given instance
func (api *HttpAPI) StopReplication(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	instance, err := inst.StopReplication(&instanceKey)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("Replica stopped: %+v", instance.Key), Details: instance})
}

// StopReplicationNicely stops replication on given instance, such that sql thead is aligned with IO thread
func (api *HttpAPI) StopReplicationNicely(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	instance, err := inst.StopReplicationNicely(&instanceKey, 0)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("Replica stopped nicely: %+v", instance.Key), Details: instance})
}

// FlushBinaryLogs runs a single FLUSH BINARY LOGS
func (api *HttpAPI) FlushBinaryLogs(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	var instance *inst.Instance
	if file := req.URL.Query().Get("binlog"); file != "" {
		instance, err = inst.FlushBinaryLogsTo(&instanceKey, file)
	} else {
		instance, err = inst.FlushBinaryLogs(&instanceKey, 1)
	}
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("Binary logs flushed on: %+v", instance.Key), Details: instance})
}

// PurgeBinaryLogs purges binary logs up to given binlog file
func (api *HttpAPI) PurgeBinaryLogs(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	logFile := params["logFile"]
	if logFile == "" {
		Respond(r, &APIResponse{Code: ERROR, Message: "purge-binary-logs: expected log file name or 'latest'"})
		return
	}
	force := (req.URL.Query().Get("force") == "true") || (params["force"] == "true")
	var instance *inst.Instance
	if logFile == "latest" {
		instance, err = inst.PurgeBinaryLogsToLatest(&instanceKey, force)
	} else {
		instance, err = inst.PurgeBinaryLogsTo(&instanceKey, logFile, force)
	}
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("Binary logs flushed on: %+v", instance.Key), Details: instance})
}

// RestartReplicationStatements receives a query to execute that requires a replication restart to apply.
// As an example, this may be `set global rpl_semi_sync_slave_enabled=1`. orchestrator will check
// replication status on given host and will wrap with appropriate stop/start statements, if need be.
func (api *HttpAPI) RestartReplicationStatements(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	query := req.URL.Query().Get("q")
	statements, err := inst.GetReplicationRestartPreserveStatements(&instanceKey, query)

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("statements for: %+v", instanceKey), Details: statements})
}

// MasterEquivalent provides (possibly empty) list of master coordinates equivalent to the given ones
func (api *HttpAPI) MasterEquivalent(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	coordinates, err := api.getBinlogCoordinates(params["logFile"], params["logPos"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	instanceCoordinates := &inst.InstanceBinlogCoordinates{Key: instanceKey, Coordinates: coordinates}

	equivalentCoordinates, err := inst.GetEquivalentMasterCoordinates(instanceCoordinates)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("Found %+v equivalent coordinates", len(equivalentCoordinates)), Details: equivalentCoordinates})
}

// CanReplicateFrom attempts to move an instance below another via pseudo GTID matching of binlog entries
func (api *HttpAPI) CanReplicateFrom(params Params, r Responder, req *http.Request, user Principal) {
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	instance, found, err := inst.ReadInstanceContext(req.Context(), &instanceKey)
	if (!found) || (err != nil) {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("Cannot read instance: %+v", instanceKey)})
		return
	}
	belowKey, err := api.getInstanceKey(params["belowHost"], params["belowPort"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	belowInstance, found, err := inst.ReadInstanceContext(req.Context(), &belowKey)
	if (!found) || (err != nil) {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("Cannot read instance: %+v", belowKey)})
		return
	}

	canReplicate, err := instance.CanReplicateFromEx(belowInstance, "CanReplicateFrom()")
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("%t", canReplicate), Details: belowKey})
}

// CanReplicateFromGTID attempts to move an instance below another via GTID.
func (api *HttpAPI) CanReplicateFromGTID(params Params, r Responder, req *http.Request, user Principal) {
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	instance, found, err := inst.ReadInstanceContext(req.Context(), &instanceKey)
	if (!found) || (err != nil) {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("Cannot read instance: %+v", instanceKey)})
		return
	}
	belowKey, err := api.getInstanceKey(params["belowHost"], params["belowPort"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	belowInstance, found, err := inst.ReadInstanceContext(req.Context(), &belowKey)
	if (!found) || (err != nil) {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("Cannot read instance: %+v", belowKey)})
		return
	}

	canReplicate, err := instance.CanReplicateFromEx(belowInstance, "CanReplicateFromGTID()")
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	if !canReplicate {
		Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("%t", canReplicate), Details: belowKey})
		return
	}
	err = inst.CheckMoveViaGTID(instance, belowInstance)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	canReplicate = (err == nil)

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("%t", canReplicate), Details: belowKey})
}

// setSemiSyncMaster
func (api *HttpAPI) setSemiSyncMaster(params Params, r Responder, req *http.Request, user Principal, enable bool) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	instance, err := inst.SetSemiSyncMaster(&instanceKey, enable)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("master semi-sync set to %t", enable), Details: instance})
}

func (api *HttpAPI) EnableSemiSyncMaster(params Params, r Responder, req *http.Request, user Principal) {
	api.setSemiSyncMaster(params, r, req, user, true)
}
func (api *HttpAPI) DisableSemiSyncMaster(params Params, r Responder, req *http.Request, user Principal) {
	api.setSemiSyncMaster(params, r, req, user, false)
}

// setSemiSyncMaster
func (api *HttpAPI) setSemiSyncReplica(params Params, r Responder, req *http.Request, user Principal, enable bool) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	instance, err := inst.SetSemiSyncReplica(&instanceKey, enable)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("replica semi-sync set to %t", enable), Details: instance})
}

func (api *HttpAPI) EnableSemiSyncReplica(params Params, r Responder, req *http.Request, user Principal) {
	api.setSemiSyncReplica(params, r, req, user, true)
}

func (api *HttpAPI) DisableSemiSyncReplica(params Params, r Responder, req *http.Request, user Principal) {
	api.setSemiSyncReplica(params, r, req, user, false)
}

// DelayReplication delays replication on given instance with given seconds
func (api *HttpAPI) DelayReplication(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	seconds, err := strconv.Atoi(params["seconds"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: "Invalid value provided for seconds"})
		return
	}
	err = inst.DelayReplication(&instanceKey, seconds)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("Replication delayed: %+v", instanceKey), Details: seconds})
}

// SetReadOnly sets the global read_only variable
func (api *HttpAPI) SetReadOnly(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	instance, err := inst.SetReadOnly(&instanceKey, true)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: "Server set as read-only", Details: instance})
}

// SetWriteable clear the global read_only variable
func (api *HttpAPI) SetWriteable(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	instance, err := inst.SetReadOnly(&instanceKey, false)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: "Server set as writeable", Details: instance})
}

// KillQuery kills a query running on a server
func (api *HttpAPI) KillQuery(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	processId, err := strconv.ParseInt(params["process"], 10, 0)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	instance, err := inst.KillQuery(&instanceKey, processId)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("Query killed on : %+v", instance.Key), Details: instance})
}

// AsciiTopology returns an ascii graph of cluster's instances
func (api *HttpAPI) asciiTopology(params Params, r Responder, req *http.Request, tabulated bool, printTags bool) {
	clusterName, err := figureClusterName(getClusterHint(params))
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	asciiOutput, err := inst.ASCIITopology(clusterName, "", tabulated, printTags)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("Topology for cluster %s", clusterName), Details: asciiOutput})
}

// SnapshotTopologies triggers orchestrator to record a snapshot of host/master for all known hosts.
func (api *HttpAPI) SnapshotTopologies(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	start := time.Now()
	if err := inst.SnapshotTopologies(); err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err)), Details: fmt.Sprintf("Took %v", time.Since(start))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: "Topology Snapshot completed", Details: fmt.Sprintf("Took %v", time.Since(start))})
}

// AsciiTopology returns an ascii graph of cluster's instances
func (api *HttpAPI) AsciiTopology(params Params, r Responder, req *http.Request) {
	api.asciiTopology(params, r, req, false, false)
}

// AsciiTopology returns an ascii graph of cluster's instances
func (api *HttpAPI) AsciiTopologyTabulated(params Params, r Responder, req *http.Request) {
	api.asciiTopology(params, r, req, true, false)
}

// AsciiTopologyTags returns an ascii graph of cluster's instances and instance tags
func (api *HttpAPI) AsciiTopologyTags(params Params, r Responder, req *http.Request) {
	api.asciiTopology(params, r, req, false, true)
}

// Cluster provides list of instances in given cluster
func (api *HttpAPI) Cluster(params Params, r Responder, req *http.Request) {
	clusterName, err := figureClusterName(getClusterHint(params))
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	instances, err := inst.ReadClusterInstances(clusterName)

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	writeHTTPJSON(r, http.StatusOK, instances)
}

// ClusterByAlias provides list of instances in given cluster
func (api *HttpAPI) ClusterByAlias(params Params, r Responder, req *http.Request) {
	clusterName, err := inst.GetClusterByAlias(params["clusterAlias"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	params["clusterName"] = clusterName
	api.Cluster(params, r, req)
}

// ClusterByInstance provides list of instances in cluster an instance belongs to
func (api *HttpAPI) ClusterByInstance(params Params, r Responder, req *http.Request) {
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	instance, found, err := inst.ReadInstanceContext(req.Context(), &instanceKey)
	if (!found) || (err != nil) {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("Cannot read instance: %+v", instanceKey)})
		return
	}

	params["clusterName"] = instance.ClusterName
	api.Cluster(params, r, req)
}

// ClusterInfo provides details of a given cluster
func (api *HttpAPI) ClusterInfo(params Params, r Responder, req *http.Request) {
	clusterName, err := figureClusterName(getClusterHint(params))
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	clusterInfo, err := inst.ReadClusterInfo(clusterName)

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	writeHTTPJSON(r, http.StatusOK, clusterInfo)
}

// Cluster provides list of instances in given cluster
func (api *HttpAPI) ClusterInfoByAlias(params Params, r Responder, req *http.Request) {
	clusterName, err := inst.GetClusterByAlias(params["clusterAlias"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	params["clusterName"] = clusterName
	api.ClusterInfo(params, r, req)
}

// ClusterOSCReplicas returns heuristic list of OSC replicas
func (api *HttpAPI) ClusterOSCReplicas(params Params, r Responder, req *http.Request) {
	clusterName, err := figureClusterName(getClusterHint(params))
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	instances, err := inst.GetClusterOSCReplicas(clusterName)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	writeHTTPJSON(r, http.StatusOK, instances)
}

// SetClusterAlias will change an alias for a given clustername
func (api *HttpAPI) SetClusterAliasManualOverride(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	clusterName := params["clusterName"]
	alias := req.URL.Query().Get("alias")

	var err error

	_, err = orcraft.PublishCommand("set-cluster-alias-manual-override", []string{clusterName, alias})

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("Cluster %s now has alias '%s'", clusterName, alias)})
}

// Clusters provides list of known clusters
func (api *HttpAPI) Clusters(params Params, r Responder, req *http.Request) {
	clusterNames, err := inst.ReadClusters()

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	writeHTTPJSON(r, http.StatusOK, clusterNames)
}

// ClustersInfo provides list of known clusters, along with some added metadata per cluster
func (api *HttpAPI) ClustersInfo(params Params, r Responder, req *http.Request) {
	clustersInfo, err := inst.ReadClustersInfo("")

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	writeHTTPJSON(r, http.StatusOK, clustersInfo)
}

// Tags lists existing tags for a given instance
func (api *HttpAPI) Tags(params Params, r Responder, req *http.Request) {
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	tags, err := inst.ReadInstanceTags(&instanceKey)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	tagStrings := []string{}
	for _, tag := range tags {
		tagStrings = append(tagStrings, tag.String())
	}
	writeHTTPJSON(r, http.StatusOK, tagStrings)
}

// TagValue returns a given tag's value for a specific instance
func (api *HttpAPI) TagValue(params Params, r Responder, req *http.Request) {
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	tag, err := getTag(params, req)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	tagExists, err := inst.ReadInstanceTag(&instanceKey, tag)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	if tagExists {
		writeHTTPJSON(r, http.StatusOK, tag.TagValue)
	} else {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("tag %s not found for %+v", tag.TagName, instanceKey)})
	}
}

// Tagged return instance keys tagged by "tag" query param
func (api *HttpAPI) Tagged(params Params, r Responder, req *http.Request) {
	tagsString := req.URL.Query().Get("tag")
	instanceKeyMap, err := inst.GetInstanceKeysByTags(tagsString)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	writeHTTPJSON(r, http.StatusOK, instanceKeyMap.GetInstanceKeys())
}

// Tags adds a tag to a given instance
func (api *HttpAPI) Tag(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	tag, err := getTag(params, req)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	_, err = orcraft.PublishCommand("put-instance-tag", inst.InstanceTag{Key: instanceKey, T: *tag})

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("%+v tagged with %s", instanceKey, tag.String()), Details: instanceKey})
}

// Untag removes a tag from an instance
func (api *HttpAPI) Untag(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	tag, err := getTag(params, req)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	untagged, err := untagThroughRaft(&instanceKey, tag)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("%s removed from %+v instances", tag.TagName, len(*untagged)), Details: untagged.GetInstanceKeys()})
}

// UntagAll removes a tag from all matching instances
func (api *HttpAPI) UntagAll(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	tag, err := getTag(params, req)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	untagged, err := untagThroughRaft(nil, tag)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("%s removed from %+v instances", tag.TagName, len(*untagged)), Details: untagged.GetInstanceKeys()})
}

// Write a cluster's master (or all clusters masters) to kv stores.
// This should generally only happen once in a lifetime of a cluster. Otherwise KV
// stores are updated via failovers.
func (api *HttpAPI) SubmitMastersToKvStores(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	clusterName, err := getClusterNameIfExists(params)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	kvPairs, submittedCount, err := logic.SubmitMastersToKvStores(clusterName, true)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("Submitted %d masters", submittedCount), Details: kvPairs})
}

// Clusters provides list of known masters
func (api *HttpAPI) Masters(params Params, r Responder, req *http.Request) {
	instances, err := inst.ReadWriteableClustersMasters()

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	writeHTTPJSON(r, http.StatusOK, instances)
}

// ClusterMaster returns the writable master of a given cluster
func (api *HttpAPI) ClusterMaster(params Params, r Responder, req *http.Request) {
	clusterName, err := figureClusterName(getClusterHint(params))
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	masters, err := inst.ReadClusterMaster(clusterName)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	if len(masters) == 0 {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("No masters found for %+v", clusterName)})
		return
	}

	writeHTTPJSON(r, http.StatusOK, masters[0])
}

// Downtimed lists downtimed instances, potentially filtered by cluster
func (api *HttpAPI) Downtimed(params Params, r Responder, req *http.Request) {
	clusterName, err := getClusterNameIfExists(params)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	instances, err := inst.ReadDowntimedInstances(clusterName)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	writeHTTPJSON(r, http.StatusOK, instances)
}

// AllInstances lists all known instances
func (api *HttpAPI) AllInstances(params Params, r Responder, req *http.Request) {
	instances, err := inst.SearchInstances("")

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	writeHTTPJSON(r, http.StatusOK, instances)
}

// Search provides list of instances matching given search param via various criteria.
func (api *HttpAPI) Search(params Params, r Responder, req *http.Request) {
	searchString := params["searchString"]
	if searchString == "" {
		searchString = req.URL.Query().Get("s")
	}
	instances, err := inst.SearchInstances(searchString)

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	writeHTTPJSON(r, http.StatusOK, instances)
}

// Problems provides list of instances with known problems
func (api *HttpAPI) Problems(params Params, r Responder, req *http.Request) {
	clusterName := params["clusterName"]
	instances, err := inst.ReadProblemInstances(clusterName)

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	writeHTTPJSON(r, http.StatusOK, instances)
}

// Audit provides list of audit entries by given page number
func (api *HttpAPI) Audit(params Params, r Responder, req *http.Request) {
	page, err := strconv.Atoi(params["page"])
	if err != nil || page < 0 {
		page = 0
	}
	var auditedInstanceKey *inst.InstanceKey
	if instanceKey, err := api.getInstanceKey(params["host"], params["port"]); err == nil {
		auditedInstanceKey = &instanceKey
	}

	audits, err := inst.ReadRecentAudit(auditedInstanceKey, page)

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	writeHTTPJSON(r, http.StatusOK, audits)
}

// HostnameResolveCache shows content of in-memory hostname cache
func (api *HttpAPI) HostnameResolveCache(params Params, r Responder, req *http.Request) {
	content, err := inst.HostnameResolveCache()

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: "Cache retrieved", Details: content})
}

// ResetHostnameResolveCache clears in-memory hostname resovle cache
func (api *HttpAPI) ResetHostnameResolveCache(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	err := inst.ResetHostnameResolveCache()

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: "Hostname cache cleared"})
}

// DeregisterHostnameUnresolve deregisters the unresolve name used previously
func (api *HttpAPI) DeregisterHostnameUnresolve(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}

	var instanceKey *inst.InstanceKey
	if instKey, err := api.getInstanceKey(params["host"], params["port"]); err == nil {
		instanceKey = &instKey
	}

	var err error
	registration := inst.NewHostnameDeregistration(instanceKey)

	_, err = orcraft.PublishCommand("register-hostname-unresolve", registration)

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	Respond(r, &APIResponse{Code: OK, Message: "Hostname deregister unresolve completed", Details: instanceKey})
}

// RegisterHostnameUnresolve registers the unresolve name to use
func (api *HttpAPI) RegisterHostnameUnresolve(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}

	var instanceKey *inst.InstanceKey
	if instKey, err := api.getInstanceKey(params["host"], params["port"]); err == nil {
		instanceKey = &instKey
	}

	hostname := params["virtualname"]
	var err error
	registration := inst.NewHostnameRegistration(instanceKey, hostname)

	_, err = orcraft.PublishCommand("register-hostname-unresolve", registration)

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	Respond(r, &APIResponse{Code: OK, Message: "Hostname register unresolve completed", Details: instanceKey})
}

// SubmitPoolInstances (re-)applies the list of hostnames for a given pool
func (api *HttpAPI) SubmitPoolInstances(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	pool := params["pool"]
	instances := req.URL.Query().Get("instances")

	var err error
	submission := inst.NewPoolInstancesSubmission(pool, instances)

	_, err = orcraft.PublishCommand("submit-pool-instances", submission)

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("Applied %s pool instances", pool), Details: pool})
}

// SubmitPoolHostnames (re-)applies the list of hostnames for a given pool
func (api *HttpAPI) ReadClusterPoolInstancesMap(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	clusterName := params["clusterName"]
	pool := params["pool"]

	poolInstancesMap, err := inst.ReadClusterPoolInstancesMap(clusterName, pool)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("Read pool instances for cluster %s", clusterName), Details: poolInstancesMap})
}

// GetHeuristicClusterPoolInstances returns instances belonging to a cluster's pool
func (api *HttpAPI) GetHeuristicClusterPoolInstances(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	clusterName, err := figureClusterName(getClusterHint(params))
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	pool := params["pool"]

	instances, err := inst.GetHeuristicClusterPoolInstances(clusterName, pool)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("Heuristic pool instances for cluster %s", clusterName), Details: instances})
}

// GetHeuristicClusterPoolInstances returns instances belonging to a cluster's pool
func (api *HttpAPI) GetHeuristicClusterPoolInstancesLag(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	clusterName, err := inst.ReadClusterNameByAlias(params["clusterName"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	pool := params["pool"]

	lag, err := inst.GetHeuristicClusterPoolInstancesLag(clusterName, pool)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("Heuristic pool lag for cluster %s", clusterName), Details: lag})
}

// ReloadClusterAlias clears in-memory hostname resovle cache
func (api *HttpAPI) ReloadClusterAlias(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}

	Respond(r, &APIResponse{Code: ERROR, Message: "This API call has been retired"})
}

// BulkPromotionRules returns a list of the known promotion rules for each instance
func (api *HttpAPI) BulkPromotionRules(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}

	promotionRules, err := inst.BulkReadCandidateDatabaseInstance()
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	writeHTTPJSON(r, http.StatusOK, promotionRules)
}

// BulkInstances returns a list of all known instances
func (api *HttpAPI) BulkInstances(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}

	instances, err := inst.BulkReadInstance()
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	writeHTTPJSON(r, http.StatusOK, instances)
}

// Agents provides complete list of registered agents (See https://github.com/openark/orchestrator-agent)
func (api *HttpAPI) Agents(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	if !config.Config.Agents.ServeHTTP {
		Respond(r, &APIResponse{Code: ERROR, Message: "Agents not served"})
		return
	}

	agents, err := agent.ReadAgents()

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	writeHTTPJSON(r, http.StatusOK, agents)
}

// Agent returns complete information of a given agent
func (api *HttpAPI) Agent(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	if !config.Config.Agents.ServeHTTP {
		Respond(r, &APIResponse{Code: ERROR, Message: "Agents not served"})
		return
	}

	agent, err := agent.GetAgent(params["host"])

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	writeHTTPJSON(r, http.StatusOK, agent)
}

// AgentUnmount instructs an agent to unmount the designated mount point
func (api *HttpAPI) AgentUnmount(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	if !config.Config.Agents.ServeHTTP {
		Respond(r, &APIResponse{Code: ERROR, Message: "Agents not served"})
		return
	}

	output, err := agent.Unmount(params["host"])

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	writeHTTPJSON(r, http.StatusOK, output)
}

// AgentMountLV instructs an agent to mount a given volume on the designated mount point
func (api *HttpAPI) AgentMountLV(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	if !config.Config.Agents.ServeHTTP {
		Respond(r, &APIResponse{Code: ERROR, Message: "Agents not served"})
		return
	}

	output, err := agent.MountLV(params["host"], req.URL.Query().Get("lv"))

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	writeHTTPJSON(r, http.StatusOK, output)
}

// AgentCreateSnapshot instructs an agent to create a new snapshot. Agent's DIY implementation.
func (api *HttpAPI) AgentCreateSnapshot(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	if !config.Config.Agents.ServeHTTP {
		Respond(r, &APIResponse{Code: ERROR, Message: "Agents not served"})
		return
	}

	output, err := agent.CreateSnapshot(params["host"])

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	writeHTTPJSON(r, http.StatusOK, output)
}

// AgentRemoveLV instructs an agent to remove a logical volume
func (api *HttpAPI) AgentRemoveLV(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	if !config.Config.Agents.ServeHTTP {
		Respond(r, &APIResponse{Code: ERROR, Message: "Agents not served"})
		return
	}

	output, err := agent.RemoveLV(params["host"], req.URL.Query().Get("lv"))

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	writeHTTPJSON(r, http.StatusOK, output)
}

// AgentMySQLStop stops MySQL service on agent
func (api *HttpAPI) AgentMySQLStop(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	if !config.Config.Agents.ServeHTTP {
		Respond(r, &APIResponse{Code: ERROR, Message: "Agents not served"})
		return
	}

	output, err := agent.MySQLStop(params["host"])

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	writeHTTPJSON(r, http.StatusOK, output)
}

// AgentMySQLStart starts MySQL service on agent
func (api *HttpAPI) AgentMySQLStart(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	if !config.Config.Agents.ServeHTTP {
		Respond(r, &APIResponse{Code: ERROR, Message: "Agents not served"})
		return
	}

	output, err := agent.MySQLStart(params["host"])

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	writeHTTPJSON(r, http.StatusOK, output)
}

func (api *HttpAPI) AgentCustomCommand(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	if !config.Config.Agents.ServeHTTP {
		Respond(r, &APIResponse{Code: ERROR, Message: "Agents not served"})
		return
	}

	output, err := agent.CustomCommand(params["host"], params["command"])

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	writeHTTPJSON(r, http.StatusOK, output)
}

// AgentSeed completely seeds a host with another host's snapshots. This is a complex operation
// governed by orchestrator and executed by the two agents involved.
func (api *HttpAPI) AgentSeed(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	if !config.Config.Agents.ServeHTTP {
		Respond(r, &APIResponse{Code: ERROR, Message: "Agents not served"})
		return
	}

	output, err := agent.Seed(params["targetHost"], params["sourceHost"])

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	writeHTTPJSON(r, http.StatusOK, output)
}

// AgentActiveSeeds lists active seeds and their state
func (api *HttpAPI) AgentActiveSeeds(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	if !config.Config.Agents.ServeHTTP {
		Respond(r, &APIResponse{Code: ERROR, Message: "Agents not served"})
		return
	}

	output, err := agent.ReadActiveSeedsForHost(params["host"])

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	writeHTTPJSON(r, http.StatusOK, output)
}

// AgentRecentSeeds lists recent seeds of a given agent
func (api *HttpAPI) AgentRecentSeeds(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	if !config.Config.Agents.ServeHTTP {
		Respond(r, &APIResponse{Code: ERROR, Message: "Agents not served"})
		return
	}

	output, err := agent.ReadRecentCompletedSeedsForHost(params["host"])

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	writeHTTPJSON(r, http.StatusOK, output)
}

// AgentSeedDetails provides details of a given seed
func (api *HttpAPI) AgentSeedDetails(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	if !config.Config.Agents.ServeHTTP {
		Respond(r, &APIResponse{Code: ERROR, Message: "Agents not served"})
		return
	}

	seedId, err := strconv.ParseInt(params["seedId"], 10, 0)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	output, err := agent.AgentSeedDetails(seedId)

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	writeHTTPJSON(r, http.StatusOK, output)
}

// AgentSeedStates returns the breakdown of states (steps) of a given seed
func (api *HttpAPI) AgentSeedStates(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	if !config.Config.Agents.ServeHTTP {
		Respond(r, &APIResponse{Code: ERROR, Message: "Agents not served"})
		return
	}

	seedId, err := strconv.ParseInt(params["seedId"], 10, 0)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	output, err := agent.ReadSeedStates(seedId)

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	writeHTTPJSON(r, http.StatusOK, output)
}

// Seeds returns all recent seeds
func (api *HttpAPI) Seeds(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	if !config.Config.Agents.ServeHTTP {
		Respond(r, &APIResponse{Code: ERROR, Message: "Agents not served"})
		return
	}

	output, err := agent.ReadRecentSeeds()

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	writeHTTPJSON(r, http.StatusOK, output)
}

// AbortSeed instructs agents to abort an active seed
func (api *HttpAPI) AbortSeed(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	if !config.Config.Agents.ServeHTTP {
		Respond(r, &APIResponse{Code: ERROR, Message: "Agents not served"})
		return
	}

	seedId, err := strconv.ParseInt(params["seedId"], 10, 0)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	err = agent.AbortSeed(seedId)

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	writeHTTPJSON(r, http.StatusOK, err == nil)
}

// Headers is a self-test call which returns HTTP headers
func (api *HttpAPI) Headers(params Params, r Responder, req *http.Request) {
	writeHTTPJSON(r, http.StatusOK, req.Header)
}

// Health performs a self test
func (api *HttpAPI) Health(params Params, r Responder, req *http.Request) {
	health, err := process.HealthTest()
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("Application node is unhealthy %+v", err), Details: health})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: "Application node is healthy", Details: health})

}

// LBCheck returns a constant response, and this can be used by load balancers that expect a given string.
func (api *HttpAPI) LBCheck(params Params, r Responder, req *http.Request) {
	writeHTTPJSON(r, http.StatusOK, "OK")
}

// LBCheck returns a constant response, and this can be used by load balancers that expect a given string.
func (api *HttpAPI) LeaderCheck(params Params, r Responder, req *http.Request) {
	respondStatus, err := strconv.Atoi(params["errorStatusCode"])
	if err != nil || respondStatus < 0 {
		respondStatus = http.StatusNotFound
	}

	if logic.IsLeader() {
		writeHTTPJSON(r, http.StatusOK, "OK")
	} else {
		writeHTTPJSON(r, respondStatus, "Not leader")
	}
}

// A configurable endpoint that can be for regular status checks or whatever.  While similar to
// Health() this returns 500 on failure.  This will prevent issues for those that have come to
// expect a 200
// It might be a good idea to deprecate the current Health() behavior and roll this in at some
// point
func (api *HttpAPI) StatusCheck(params Params, r Responder, req *http.Request) {
	health, err := process.HealthTest()
	if err != nil {
		writeHTTPJSON(r, 500, &APIResponse{Code: ERROR, Message: fmt.Sprintf("Application node is unhealthy %+v", err), Details: health})
		return
	}
	Respond(r, &APIResponse{Code: OK, Message: "Application node is healthy", Details: health})
}

// ReloadConfiguration reloads confiug settings (not all of which will apply after change)
func (api *HttpAPI) ReloadConfiguration(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	extraConfigFile := req.URL.Query().Get("config")
	if _, err := config.Reload(extraConfigFile); err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("Cannot reload config: %+v", err)})
		return
	}
	inst.AuditOperation("reload-configuration", nil, "Triggered via API")

	Respond(r, &APIResponse{Code: OK, Message: "Config reloaded", Details: extraConfigFile})
}

// ReplicationAnalysis retuens list of issues
func (api *HttpAPI) replicationAnalysis(clusterName string, instanceKey *inst.InstanceKey, params Params, r Responder, req *http.Request) {
	analysis, err := inst.GetReplicationAnalysis(clusterName, &dto.ReplicationAnalysisHints{IncludeDowntimed: req.URL.Query().Get("includeDowntimed") != "false"})
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("Cannot get analysis: %+v", err)})
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

	Respond(r, &APIResponse{Code: OK, Message: "Analysis", Details: analysis})
}

// ReplicationAnalysis retuens list of issues
func (api *HttpAPI) ReplicationAnalysis(params Params, r Responder, req *http.Request) {
	api.replicationAnalysis("", nil, params, r, req)
}

// ReplicationAnalysis retuens list of issues
func (api *HttpAPI) ReplicationAnalysisForCluster(params Params, r Responder, req *http.Request) {
	clusterName, err := inst.DeduceClusterName(params["clusterName"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("Cannot get analysis: %+v", err)})
		return
	}
	if clusterName == "" {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("Cannot get cluster name: %+v", params["clusterName"])})
		return
	}
	api.replicationAnalysis(clusterName, nil, params, r, req)
}

// ReplicationAnalysis retuens list of issues
func (api *HttpAPI) ReplicationAnalysisForKey(params Params, r Responder, req *http.Request) {
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("Cannot get analysis: %+v", err)})
		return
	}
	if !instanceKey.IsValid() {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("Cannot get analysis: invalid key %+v", instanceKey)})
		return
	}
	api.replicationAnalysis("", &instanceKey, params, r, req)
}

// RecoverLite attempts recovery on a given instance, without executing external processes
func (api *HttpAPI) RecoverLite(params Params, r Responder, req *http.Request, user Principal) {
	params["skipProcesses"] = "true"
	api.Recover(params, r, req, user)
}

// Recover attempts recovery on a given instance
func (api *HttpAPI) Recover(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	var candidateKey *inst.InstanceKey
	if key, err := api.getInstanceKey(params["candidateHost"], params["candidatePort"]); err == nil {
		candidateKey = &key
	}

	skipProcesses := (req.URL.Query().Get("skipProcesses") == "true") || (params["skipProcesses"] == "true")
	recoveryAttempted, promotedInstanceKey, err := logic.CheckAndRecover(&instanceKey, candidateKey, skipProcesses)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err)), Details: instanceKey})
		return
	}
	if !recoveryAttempted {
		Respond(r, &APIResponse{Code: ERROR, Message: "Recovery not attempted", Details: instanceKey})
		return
	}
	if promotedInstanceKey == nil {
		Respond(r, &APIResponse{Code: ERROR, Message: "Recovery attempted but no instance promoted", Details: instanceKey})
		return
	}
	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("Recovery executed on %+v", instanceKey), Details: *promotedInstanceKey})
}

// GracefulMasterTakeover gracefully fails over a master onto its single replica.
func (api *HttpAPI) gracefulMasterTakeover(params Params, r Responder, req *http.Request, user Principal, auto bool) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	clusterName, err := figureClusterName(getClusterHint(params))
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	designatedKey, _ := api.getInstanceKey(params["designatedHost"], params["designatedPort"])
	// designatedKey may be empty/invalid
	topologyRecovery, _, err := logic.GracefulMasterTakeover(clusterName, &designatedKey, auto)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err)), Details: topologyRecovery})
		return
	}
	if topologyRecovery == nil || topologyRecovery.SuccessorKey == nil {
		Respond(r, &APIResponse{Code: ERROR, Message: "graceful-master-takeover: no successor promoted", Details: topologyRecovery})
		return
	}
	Respond(r, &APIResponse{Code: OK, Message: "graceful-master-takeover: successor promoted", Details: topologyRecovery})
}

// GracefulMasterTakeover gracefully fails over a master, either:
// - onto its single replica, or
// - onto a replica indicated by the user
func (api *HttpAPI) GracefulMasterTakeover(params Params, r Responder, req *http.Request, user Principal) {
	api.gracefulMasterTakeover(params, r, req, user, false)
}

// GracefulMasterTakeoverAuto gracefully fails over a master onto a replica of orchestrator's choosing
func (api *HttpAPI) GracefulMasterTakeoverAuto(params Params, r Responder, req *http.Request, user Principal) {
	api.gracefulMasterTakeover(params, r, req, user, true)
}

// ForceMasterFailover fails over a master (even if there's no particular problem with the master)
func (api *HttpAPI) ForceMasterFailover(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	clusterName, err := figureClusterName(getClusterHint(params))
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	topologyRecovery, err := logic.ForceMasterFailover(clusterName)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	if topologyRecovery.SuccessorKey != nil {
		Respond(r, &APIResponse{Code: OK, Message: "Master failed over", Details: topologyRecovery})
	} else {
		Respond(r, &APIResponse{Code: ERROR, Message: "Master not failed over", Details: topologyRecovery})
	}
}

// ForceMasterTakeover fails over a master (even if there's no particular problem with the master)
func (api *HttpAPI) ForceMasterTakeover(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	clusterName, err := figureClusterName(getClusterHint(params))
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	designatedKey, _ := api.getInstanceKey(params["designatedHost"], params["designatedPort"])
	designatedInstance, _, err := inst.ReadInstanceContext(req.Context(), &designatedKey)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	if designatedInstance == nil {
		Respond(r, &APIResponse{Code: ERROR, Message: "Instance not found"})
		return
	}

	topologyRecovery, err := logic.ForceMasterTakeover(clusterName, designatedInstance)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	if topologyRecovery.SuccessorKey != nil {
		Respond(r, &APIResponse{Code: OK, Message: "Master failed over", Details: topologyRecovery})
	} else {
		Respond(r, &APIResponse{Code: ERROR, Message: "Master not failed over", Details: topologyRecovery})
	}
}

// Registers promotion preference for given instance
func (api *HttpAPI) RegisterCandidate(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	promotionRule, err := inst.ParseCandidatePromotionRule(params["promotionRule"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	candidate := inst.NewCandidateDatabaseInstance(&instanceKey, promotionRule).WithCurrentTime()

	_, err = orcraft.PublishCommand("register-candidate", candidate)

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: "Registered candidate", Details: instanceKey})
}

// AutomatedRecoveryFilters retuens list of clusters which are configured with automated recovery
func (api *HttpAPI) AutomatedRecoveryFilters(params Params, r Responder, req *http.Request) {
	doc, err := recoverypolicy.GetPolicy(req.Context(), domain.ScopeGlobal, domain.GlobalKey)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error()})
		return
	}
	Respond(r, &APIResponse{Code: OK, Message: "Automated recovery configuration details", Details: doc.Effective})
}

// AuditFailureDetection provides list of topology_failure_detection entries
func (api *HttpAPI) AuditFailureDetection(params Params, r Responder, req *http.Request) {

	var audits []*logic.TopologyRecovery
	var err error

	if detectionId, derr := strconv.ParseInt(params["id"], 10, 0); derr == nil && detectionId > 0 {
		audits, err = logic.ReadFailureDetection(detectionId)
	} else {
		page, derr := strconv.Atoi(params["page"])
		if derr != nil || page < 0 {
			page = 0
		}
		audits, err = logic.ReadRecentFailureDetections(params["clusterAlias"], page)
	}

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	writeHTTPJSON(r, http.StatusOK, audits)
}

// AuditRecoverySteps returns audited steps of a given recovery
func (api *HttpAPI) AuditRecoverySteps(params Params, r Responder, req *http.Request) {
	recoveryUID := params["uid"]
	audits, err := logic.ReadTopologyRecoverySteps(recoveryUID)

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	writeHTTPJSON(r, http.StatusOK, audits)
}

// ReadReplicationAnalysisChangelog lists instances and their analysis changelog
func (api *HttpAPI) ReadReplicationAnalysisChangelog(params Params, r Responder, req *http.Request) {
	changelogs, err := inst.ReadReplicationAnalysisChangelog()

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	writeHTTPJSON(r, http.StatusOK, changelogs)
}

// AuditRecovery provides list of topology-recovery entries
func (api *HttpAPI) AuditRecovery(params Params, r Responder, req *http.Request) {
	var audits []*logic.TopologyRecovery
	var err error

	if recoveryUID := params["uid"]; recoveryUID != "" {
		audits, err = logic.ReadRecoveryByUID(recoveryUID)
	} else if recoveryId, derr := strconv.ParseInt(params["id"], 10, 0); derr == nil && recoveryId > 0 {
		audits, err = logic.ReadRecovery(recoveryId)
	} else {
		page, derr := strconv.Atoi(params["page"])
		if derr != nil || page < 0 {
			page = 0
		}
		unacknowledgedOnly := (req.URL.Query().Get("unacknowledged") == "true")

		audits, err = logic.ReadRecentRecoveries(params["clusterName"], params["clusterAlias"], unacknowledgedOnly, page)
	}

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	writeHTTPJSON(r, http.StatusOK, audits)
}

// ActiveClusterRecovery returns recoveries in-progress for a given cluster
func (api *HttpAPI) ActiveClusterRecovery(params Params, r Responder, req *http.Request) {
	recoveries, err := logic.ReadActiveClusterRecovery(params["clusterName"])

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	writeHTTPJSON(r, http.StatusOK, recoveries)
}

// RecentlyActiveClusterRecovery returns recoveries in-progress for a given cluster
func (api *HttpAPI) RecentlyActiveClusterRecovery(params Params, r Responder, req *http.Request) {
	recoveries, err := logic.ReadRecentlyActiveClusterRecovery(params["clusterName"])

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	writeHTTPJSON(r, http.StatusOK, recoveries)
}

// RecentlyActiveClusterRecovery returns recoveries in-progress for a given cluster
func (api *HttpAPI) RecentlyActiveInstanceRecovery(params Params, r Responder, req *http.Request) {
	instanceKey, err := api.getInstanceKey(params["host"], params["port"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	recoveries, err := logic.ReadRecentlyActiveInstanceRecovery(&instanceKey)

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	writeHTTPJSON(r, http.StatusOK, recoveries)
}

// ClusterInfo provides details of a given cluster
func (api *HttpAPI) AcknowledgeClusterRecoveries(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}

	var clusterName string
	var err error
	if params["clusterAlias"] != "" {
		clusterName, err = inst.GetClusterByAlias(params["clusterAlias"])
	} else {
		clusterName, err = figureClusterName(getClusterHint(params))
	}

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	comment := strings.TrimSpace(req.URL.Query().Get("comment"))
	if comment == "" {
		Respond(r, &APIResponse{Code: ERROR, Message: "No acknowledge comment given"})
		return
	}
	userId := getUserId(req, user)
	if userId == "" {
		userId = inst.GetMaintenanceOwner()
	}

	ack := logic.NewRecoveryAcknowledgement(userId, comment)
	ack.ClusterName = clusterName
	_, err = orcraft.PublishCommand("ack-recovery", ack)

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: "Acknowledged cluster recoveries", Details: clusterName})
}

// ClusterInfo provides details of a given cluster
func (api *HttpAPI) AcknowledgeInstanceRecoveries(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}

	instanceKey, err := api.getInstanceKey(params["host"], params["port"])
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	comment := strings.TrimSpace(req.URL.Query().Get("comment"))
	if comment == "" {
		Respond(r, &APIResponse{Code: ERROR, Message: "No acknowledge comment given"})
		return
	}
	userId := getUserId(req, user)
	if userId == "" {
		userId = inst.GetMaintenanceOwner()
	}

	ack := logic.NewRecoveryAcknowledgement(userId, comment)
	ack.Key = instanceKey
	_, err = orcraft.PublishCommand("ack-recovery", ack)

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: "Acknowledged instance recoveries", Details: instanceKey})
}

// ClusterInfo provides details of a given cluster
func (api *HttpAPI) AcknowledgeRecovery(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
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
			Respond(r, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
			return
		}
	} else {
		idParam = recoveryUid
	}
	comment := strings.TrimSpace(req.URL.Query().Get("comment"))
	if comment == "" {
		Respond(r, &APIResponse{Code: ERROR, Message: "No acknowledge comment given"})
		return
	}
	userId := getUserId(req, user)
	if userId == "" {
		userId = inst.GetMaintenanceOwner()
	}

	ack := logic.NewRecoveryAcknowledgement(userId, comment)
	ack.Id = recoveryId
	ack.UID = recoveryUid
	_, err = orcraft.PublishCommand("ack-recovery", ack)

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: "Acknowledged recovery", Details: idParam})
}

// ClusterInfo provides details of a given cluster
func (api *HttpAPI) AcknowledgeAllRecoveries(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}

	comment := strings.TrimSpace(req.URL.Query().Get("comment"))
	if comment == "" {
		Respond(r, &APIResponse{Code: ERROR, Message: "No acknowledge comment given"})
		return
	}
	userId := getUserId(req, user)
	if userId == "" {
		userId = inst.GetMaintenanceOwner()
	}
	var err error

	ack := logic.NewRecoveryAcknowledgement(userId, comment)
	ack.AllRecoveries = true
	_, err = orcraft.PublishCommand("ack-recovery", ack)

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: "Acknowledged all recoveries", Details: comment})
}

// BlockedRecoveries reads list of currently blocked recoveries, optionally filtered by cluster name
func (api *HttpAPI) BlockedRecoveries(params Params, r Responder, req *http.Request) {
	blockedRecoveries, err := logic.ReadBlockedRecoveries(params["clusterName"])

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	writeHTTPJSON(r, http.StatusOK, blockedRecoveries)
}

// DisableGlobalRecoveries globally disables recoveries
func (api *HttpAPI) DisableGlobalRecoveries(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}

	var err error

	_, err = orcraft.PublishCommand("disable-global-recoveries", 0)

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: "Globally disabled recoveries", Details: "disabled"})
}

// EnableGlobalRecoveries globally enables recoveries
func (api *HttpAPI) EnableGlobalRecoveries(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForAction(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}

	var err error

	_, err = orcraft.PublishCommand("enable-global-recoveries", 0)

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}

	Respond(r, &APIResponse{Code: OK, Message: "Globally enabled recoveries", Details: "enabled"})
}

// CheckGlobalRecoveries checks whether
func (api *HttpAPI) CheckGlobalRecoveries(params Params, r Responder, req *http.Request) {
	isDisabled, err := logic.IsRecoveryDisabled()

	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	details := "enabled"
	if isDisabled {
		details = "disabled"
	}
	Respond(r, &APIResponse{Code: OK, Message: fmt.Sprintf("Global recoveries %+v", details), Details: details})
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

func configurationUser(req *http.Request, user Principal) string {
	if id := getUserId(req, user); id != "" {
		return id
	}
	return "local-session"
}

func (api *HttpAPI) RecoveryPolicy(params Params, r Responder, req *http.Request) {
	doc, err := recoverypolicy.GetPolicy(req.Context(), params["scopeType"], params["scopeKey"])
	if err != nil {
		RespondStatus(r, http.StatusBadRequest, &APIResponse{Code: ERROR, Message: err.Error()})
		return
	}
	Respond(r, &APIResponse{Code: OK, Message: "Recovery policy", Details: doc})
}

func (api *HttpAPI) SaveRecoveryPolicy(_ Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForConfiguration(req, user) {
		RespondStatus(r, http.StatusForbidden, &APIResponse{Code: ERROR, Message: "Configuration administrator permission required"})
		return
	}
	var command dto.SaveRecoveryPolicyCommand
	if err := decodeConfigurationBody(req, &command); err != nil {
		RespondStatus(r, http.StatusBadRequest, &APIResponse{Code: ERROR, Message: err.Error()})
		return
	}
	command.UpdatedBy = configurationUser(req, user)
	if strings.TrimSpace(command.ChangeReason) == "" {
		RespondStatus(r, http.StatusBadRequest, &APIResponse{Code: ERROR, Message: "changeReason is required"})
		return
	}
	if _, err := orcraft.PublishCommand("save-recovery-policy", command); err != nil {
		status := http.StatusInternalServerError
		if recoverypolicy.IsRevisionConflict(err) {
			status = http.StatusConflict
		}
		RespondStatus(r, status, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	doc, err := recoverypolicy.GetPolicy(req.Context(), command.ScopeType, command.ScopeKey)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error()})
		return
	}
	Respond(r, &APIResponse{Code: OK, Message: "Recovery policy saved", Details: doc})
}

func (api *HttpAPI) RecoveryHookProfiles(_ Params, r Responder, req *http.Request) {
	profiles, err := recoverypolicy.ListHookProfiles(req.Context())
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error()})
		return
	}
	Respond(r, &APIResponse{Code: OK, Message: "Recovery hook profiles", Details: profiles})
}

func (api *HttpAPI) SaveRecoveryHookProfile(_ Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForConfiguration(req, user) {
		RespondStatus(r, http.StatusForbidden, &APIResponse{Code: ERROR, Message: "Configuration administrator permission required"})
		return
	}
	var command dto.SaveRecoveryHookProfileCommand
	if err := decodeConfigurationBody(req, &command); err != nil {
		RespondStatus(r, http.StatusBadRequest, &APIResponse{Code: ERROR, Message: err.Error()})
		return
	}
	command.Profile.UpdatedBy = configurationUser(req, user)
	if strings.TrimSpace(command.Profile.ChangeReason) == "" {
		RespondStatus(r, http.StatusBadRequest, &APIResponse{Code: ERROR, Message: "changeReason is required"})
		return
	}
	if _, err := orcraft.PublishCommand("save-recovery-hook-profile", command); err != nil {
		status := http.StatusInternalServerError
		if recoverypolicy.IsRevisionConflict(err) {
			status = http.StatusConflict
		}
		RespondStatus(r, status, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	profiles, err := recoverypolicy.ListHookProfiles(req.Context())
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error()})
		return
	}
	Respond(r, &APIResponse{Code: OK, Message: "Recovery hook profile saved", Details: profiles})
}

func (api *HttpAPI) TestRecoveryHookProfile(_ Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForConfiguration(req, user) {
		RespondStatus(r, http.StatusForbidden, &APIResponse{Code: ERROR, Message: "Configuration administrator permission required"})
		return
	}
	var request struct {
		ProfileID string `json:"profileId"`
	}
	if err := decodeConfigurationBody(req, &request); err != nil {
		RespondStatus(r, http.StatusBadRequest, &APIResponse{Code: ERROR, Message: err.Error()})
		return
	}
	profiles, err := recoverypolicy.ListHookProfiles(req.Context())
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error()})
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
		RespondStatus(r, http.StatusNotFound, &APIResponse{Code: ERROR, Message: "enabled hook profile not found"})
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
		inst.AuditOperation("test-recovery-hook", nil, fmt.Sprintf("profile=%s revision=%d command=%d error=%v", selected.ID, selected.Revision, index+1, commandErr))
		if commandErr != nil && selected.FailurePolicy == "abort" {
			break
		}
	}
	Respond(r, &APIResponse{Code: OK, Message: "Recovery hook test completed", Details: results})
}

func (api *HttpAPI) RecoveryHookAssignments(params Params, r Responder, req *http.Request) {
	assignments, err := recoverypolicy.ListHookAssignments(req.Context(), params["scopeType"], params["scopeKey"])
	if err != nil {
		RespondStatus(r, http.StatusBadRequest, &APIResponse{Code: ERROR, Message: err.Error()})
		return
	}
	Respond(r, &APIResponse{Code: OK, Message: "Recovery hook assignments", Details: assignments})
}

func (api *HttpAPI) SaveRecoveryHookAssignment(_ Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForConfiguration(req, user) {
		RespondStatus(r, http.StatusForbidden, &APIResponse{Code: ERROR, Message: "Configuration administrator permission required"})
		return
	}
	var command dto.SaveRecoveryHookAssignmentCommand
	if err := decodeConfigurationBody(req, &command); err != nil {
		RespondStatus(r, http.StatusBadRequest, &APIResponse{Code: ERROR, Message: err.Error()})
		return
	}
	command.Assignment.UpdatedBy = configurationUser(req, user)
	if strings.TrimSpace(command.Assignment.ChangeReason) == "" {
		RespondStatus(r, http.StatusBadRequest, &APIResponse{Code: ERROR, Message: "changeReason is required"})
		return
	}
	if _, err := orcraft.PublishCommand("save-recovery-hook-assignment", command); err != nil {
		status := http.StatusInternalServerError
		if recoverypolicy.IsRevisionConflict(err) {
			status = http.StatusConflict
		}
		RespondStatus(r, status, &APIResponse{Code: ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	assignments, err := recoverypolicy.ListHookAssignments(req.Context(), command.Assignment.ScopeType, command.Assignment.ScopeKey)
	if err != nil {
		Respond(r, &APIResponse{Code: ERROR, Message: err.Error()})
		return
	}
	Respond(r, &APIResponse{Code: OK, Message: "Recovery hook assignment saved", Details: assignments})
}

func (api *HttpAPI) getSynonymPath(path string) (synonymPath string) {
	pathBase, _, _ := strings.Cut(path, "/")
	if synonym, ok := apiSynonyms[pathBase]; ok {
		synonymPath = fmt.Sprintf("%s%s", synonym, path[len(pathBase):])
	}
	return synonymPath
}

func (api *HttpAPI) registerSingleAPIRequest(m *Router, path string, handler Handler, allowProxy bool) {
	registeredPaths = append(registeredPaths, path)
	fullPath := fmt.Sprintf("%s/api/%s", api.URLPrefix, path)

	if allowProxy {
		m.Get(fullPath, raftReverseProxy, handler)
	} else {
		m.Get(fullPath, handler)
	}
	if isWebAction(path) {
		handlers := []Handler{guardWebAction, handler}
		if allowProxy {
			handlers = []Handler{guardWebAction, raftReverseProxy, handler}
		}
		m.Post(fullPath, handlers...)
	}
}

func (api *HttpAPI) registerAPIRequestInternal(m *Router, path string, handler Handler, allowProxy bool) {
	api.registerSingleAPIRequest(m, path, handler, allowProxy)

	if synonym := api.getSynonymPath(path); synonym != "" {
		api.registerSingleAPIRequest(m, synonym, handler, allowProxy)
	}
}

func (api *HttpAPI) registerAPIRequest(m *Router, path string, handler Handler) {
	api.registerAPIRequestInternal(m, path, handler, true)
}

func (api *HttpAPI) registerAPIRequestNoProxy(m *Router, path string, handler Handler) {
	api.registerAPIRequestInternal(m, path, handler, false)
}

func (api *HttpAPI) registerAPIMethod(m *Router, method, path string, handler Handler, allowProxy bool) {
	registeredPaths = append(registeredPaths, path)
	fullPath := fmt.Sprintf("%s/api/%s", api.URLPrefix, path)
	handlers := []Handler{handler}
	if method != http.MethodGet {
		handlers = []Handler{guardWebAction, handler}
	}
	if allowProxy {
		handlers = append(handlers[:len(handlers)-1], raftReverseProxy, handler)
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
func (api *HttpAPI) RegisterRequests(m *Router) {
	api.registerCLIRequests(m)
	// Smart relocation:
	api.registerAPIRequest(m, "relocate/:host/:port/:belowHost/:belowPort", api.RelocateBelow)
	api.registerAPIRequest(m, "relocate-below/:host/:port/:belowHost/:belowPort", api.RelocateBelow)
	api.registerAPIRequest(m, "relocate-slaves/:host/:port/:belowHost/:belowPort", api.RelocateReplicas)
	api.registerAPIRequest(m, "regroup-slaves/:host/:port", api.RegroupReplicas)

	// Classic file:pos relocation:
	api.registerAPIRequest(m, "move-up/:host/:port", api.MoveUp)
	api.registerAPIRequest(m, "move-up-slaves/:host/:port", api.MoveUpReplicas)
	api.registerAPIRequest(m, "move-below/:host/:port/:siblingHost/:siblingPort", api.MoveBelow)
	api.registerAPIRequest(m, "move-equivalent/:host/:port/:belowHost/:belowPort", api.MoveEquivalent)
	api.registerAPIRequest(m, "repoint/:host/:port", api.Repoint)
	api.registerAPIRequest(m, "repoint/:host/:port/:belowHost/:belowPort", api.Repoint)
	api.registerAPIRequest(m, "repoint-slaves/:host/:port", api.RepointReplicas)
	api.registerAPIRequest(m, "make-co-master/:host/:port", api.MakeCoMaster)
	api.registerAPIRequest(m, "enslave-siblings/:host/:port", api.TakeSiblings)
	api.registerAPIRequest(m, "enslave-master/:host/:port", api.TakeMaster)
	api.registerAPIRequest(m, "master-equivalent/:host/:port/:logFile/:logPos", api.MasterEquivalent)

	// Binlog server relocation:
	api.registerAPIRequest(m, "regroup-slaves-bls/:host/:port", api.RegroupReplicasBinlogServers)

	// GTID relocation:
	api.registerAPIRequest(m, "move-below-gtid/:host/:port/:belowHost/:belowPort", api.MoveBelowGTID)
	api.registerAPIRequest(m, "move-slaves-gtid/:host/:port/:belowHost/:belowPort", api.MoveReplicasGTID)
	api.registerAPIRequest(m, "regroup-slaves-gtid/:host/:port", api.RegroupReplicasGTID)

	// Pseudo-GTID relocation:
	api.registerAPIRequest(m, "match/:host/:port/:belowHost/:belowPort", api.MatchBelow)
	api.registerAPIRequest(m, "match-below/:host/:port/:belowHost/:belowPort", api.MatchBelow)
	api.registerAPIRequest(m, "match-up/:host/:port", api.MatchUp)
	api.registerAPIRequest(m, "match-slaves/:host/:port/:belowHost/:belowPort", api.MultiMatchReplicas)
	api.registerAPIRequest(m, "match-up-slaves/:host/:port", api.MatchUpReplicas)
	api.registerAPIRequest(m, "regroup-slaves-pgtid/:host/:port", api.RegroupReplicasPseudoGTID)
	// Legacy, need to revisit:
	api.registerAPIRequest(m, "make-master/:host/:port", api.MakeMaster)
	api.registerAPIRequest(m, "make-local-master/:host/:port", api.MakeLocalMaster)

	// Replication, general:
	api.registerAPIRequest(m, "enable-gtid/:host/:port", api.EnableGTID)
	api.registerAPIRequest(m, "disable-gtid/:host/:port", api.DisableGTID)
	api.registerAPIRequest(m, "locate-gtid-errant/:host/:port", api.LocateErrantGTID)
	api.registerAPIRequest(m, "gtid-errant-reset-master/:host/:port", api.ErrantGTIDResetMaster)
	api.registerAPIRequest(m, "gtid-errant-inject-empty/:host/:port", api.ErrantGTIDInjectEmpty)
	api.registerAPIRequest(m, "skip-query/:host/:port", api.SkipQuery)
	api.registerAPIRequest(m, "start-slave/:host/:port", api.StartReplication)
	api.registerAPIRequest(m, "restart-slave/:host/:port", api.RestartReplication)
	api.registerAPIRequest(m, "stop-slave/:host/:port", api.StopReplication)
	api.registerAPIRequest(m, "stop-slave-nice/:host/:port", api.StopReplicationNicely)
	api.registerAPIRequest(m, "reset-slave/:host/:port", api.ResetReplication)
	api.registerAPIRequest(m, "change-master-credentials/:host/:port", api.ChangeMasterCredentials)
	api.registerAPIRequest(m, "detach-slave/:host/:port", api.DetachReplicaMasterHost)
	api.registerAPIRequest(m, "reattach-slave/:host/:port", api.ReattachReplicaMasterHost)
	api.registerAPIRequest(m, "detach-slave-master-host/:host/:port", api.DetachReplicaMasterHost)
	api.registerAPIRequest(m, "reattach-slave-master-host/:host/:port", api.ReattachReplicaMasterHost)
	api.registerAPIRequest(m, "flush-binary-logs/:host/:port", api.FlushBinaryLogs)
	api.registerAPIRequest(m, "purge-binary-logs/:host/:port/:logFile", api.PurgeBinaryLogs)
	api.registerAPIRequest(m, "restart-slave-statements/:host/:port", api.RestartReplicationStatements)
	api.registerAPIRequest(m, "enable-semi-sync-master/:host/:port", api.EnableSemiSyncMaster)
	api.registerAPIRequest(m, "disable-semi-sync-master/:host/:port", api.DisableSemiSyncMaster)
	api.registerAPIRequest(m, "enable-semi-sync-replica/:host/:port", api.EnableSemiSyncReplica)
	api.registerAPIRequest(m, "disable-semi-sync-replica/:host/:port", api.DisableSemiSyncReplica)
	api.registerAPIRequest(m, "delay-replication/:host/:port/:seconds", api.DelayReplication)

	// Replication information:
	api.registerAPIRequest(m, "can-replicate-from/:host/:port/:belowHost/:belowPort", api.CanReplicateFrom)
	api.registerAPIRequest(m, "can-replicate-from-gtid/:host/:port/:belowHost/:belowPort", api.CanReplicateFromGTID)

	// Instance:
	api.registerAPIRequest(m, "set-read-only/:host/:port", api.SetReadOnly)
	api.registerAPIRequest(m, "set-writeable/:host/:port", api.SetWriteable)
	api.registerAPIRequest(m, "kill-query/:host/:port/:process", api.KillQuery)

	// Binary logs:
	api.registerAPIRequest(m, "last-pseudo-gtid/:host/:port", api.LastPseudoGTID)

	// Pools:
	api.registerAPIRequest(m, "submit-pool-instances/:pool", api.SubmitPoolInstances)
	api.registerAPIRequest(m, "cluster-pool-instances/:clusterName", api.ReadClusterPoolInstancesMap)
	api.registerAPIRequest(m, "cluster-pool-instances/:clusterName/:pool", api.ReadClusterPoolInstancesMap)
	api.registerAPIRequest(m, "heuristic-cluster-pool-instances/:clusterName", api.GetHeuristicClusterPoolInstances)
	api.registerAPIRequest(m, "heuristic-cluster-pool-instances/:clusterName/:pool", api.GetHeuristicClusterPoolInstances)
	api.registerAPIRequest(m, "heuristic-cluster-pool-lag/:clusterName", api.GetHeuristicClusterPoolInstancesLag)
	api.registerAPIRequest(m, "heuristic-cluster-pool-lag/:clusterName/:pool", api.GetHeuristicClusterPoolInstancesLag)

	// Information:
	api.registerAPIRequest(m, "search/:searchString", api.Search)
	api.registerAPIRequest(m, "search", api.Search)

	// Cluster
	api.registerAPIRequest(m, "cluster/:clusterHint", api.Cluster)
	api.registerAPIRequest(m, "cluster/alias/:clusterAlias", api.ClusterByAlias)
	api.registerAPIRequest(m, "cluster/instance/:host/:port", api.ClusterByInstance)
	api.registerAPIRequest(m, "cluster-info/:clusterHint", api.ClusterInfo)
	api.registerAPIRequest(m, "cluster-info/alias/:clusterAlias", api.ClusterInfoByAlias)
	api.registerAPIRequest(m, "cluster-osc-slaves/:clusterHint", api.ClusterOSCReplicas)
	api.registerAPIRequest(m, "set-cluster-alias/:clusterName", api.SetClusterAliasManualOverride)
	api.registerAPIRequest(m, "clusters", api.Clusters)
	api.registerAPIRequest(m, "clusters-info", api.ClustersInfo)

	api.registerAPIRequest(m, "masters", api.Masters)
	api.registerAPIRequest(m, "master/:clusterHint", api.ClusterMaster)
	api.registerAPIRequest(m, "instance-replicas/:host/:port", api.InstanceReplicas)
	api.registerAPIRequest(m, "all-instances", api.AllInstances)
	api.registerAPIRequest(m, "downtimed", api.Downtimed)
	api.registerAPIRequest(m, "downtimed/:clusterHint", api.Downtimed)
	api.registerAPIRequest(m, "topology/:clusterHint", api.AsciiTopology)
	api.registerAPIRequest(m, "topology/:host/:port", api.AsciiTopology)
	api.registerAPIRequest(m, "topology-tabulated/:clusterHint", api.AsciiTopologyTabulated)
	api.registerAPIRequest(m, "topology-tabulated/:host/:port", api.AsciiTopologyTabulated)
	api.registerAPIRequest(m, "topology-tags/:clusterHint", api.AsciiTopologyTags)
	api.registerAPIRequest(m, "topology-tags/:host/:port", api.AsciiTopologyTags)
	api.registerAPIRequest(m, "snapshot-topologies", api.SnapshotTopologies)

	// Key-value:
	api.registerAPIRequest(m, "submit-masters-to-kv-stores", api.SubmitMastersToKvStores)
	api.registerAPIRequest(m, "submit-masters-to-kv-stores/:clusterHint", api.SubmitMastersToKvStores)

	// Tags:
	api.registerAPIRequest(m, "tagged", api.Tagged)
	api.registerAPIRequest(m, "tags/:host/:port", api.Tags)
	api.registerAPIRequest(m, "tag-value/:host/:port", api.TagValue)
	api.registerAPIRequest(m, "tag-value/:host/:port/:tagName", api.TagValue)
	api.registerAPIRequest(m, "tag/:host/:port", api.Tag)
	api.registerAPIRequest(m, "tag/:host/:port/:tagName/:tagValue", api.Tag)
	api.registerAPIRequest(m, "untag/:host/:port", api.Untag)
	api.registerAPIRequest(m, "untag/:host/:port/:tagName", api.Untag)
	api.registerAPIRequest(m, "untag-all", api.UntagAll)
	api.registerAPIRequest(m, "untag-all/:tagName/:tagValue", api.UntagAll)

	// Instance management:
	api.registerAPIRequest(m, "instance/:host/:port", api.Instance)
	api.registerAPIRequest(m, "discover/:host/:port", api.Discover)
	api.registerAPIRequest(m, "async-discover/:host/:port", api.AsyncDiscover)
	api.registerAPIRequest(m, "refresh/:host/:port", api.Refresh)
	api.registerAPIRequest(m, "forget/:host/:port", api.Forget)
	api.registerAPIRequest(m, "forget-cluster/:clusterHint", api.ForgetCluster)
	api.registerAPIRequest(m, "begin-maintenance/:host/:port/:owner/:reason", api.BeginMaintenance)
	api.registerAPIRequest(m, "end-maintenance/:host/:port", api.EndMaintenanceByInstanceKey)
	api.registerAPIRequest(m, "in-maintenance/:host/:port", api.InMaintenance)
	api.registerAPIRequest(m, "end-maintenance/:maintenanceKey", api.EndMaintenance)
	api.registerAPIRequest(m, "maintenance", api.Maintenance)
	api.registerAPIRequest(m, "begin-downtime/:host/:port/:owner/:reason", api.BeginDowntime)
	api.registerAPIRequest(m, "begin-downtime/:host/:port/:owner/:reason/:duration", api.BeginDowntime)
	api.registerAPIRequest(m, "end-downtime/:host/:port", api.EndDowntime)

	// Recovery:
	api.registerAPIMethod(m, http.MethodGet, "recovery-policy/:scopeType/:scopeKey", api.RecoveryPolicy, true)
	api.registerAPIMethod(m, http.MethodPost, "recovery-policy", api.SaveRecoveryPolicy, true)
	api.registerAPIMethod(m, http.MethodGet, "recovery-hook-profiles", api.RecoveryHookProfiles, true)
	api.registerAPIMethod(m, http.MethodPost, "recovery-hook-profiles", api.SaveRecoveryHookProfile, true)
	api.registerAPIMethod(m, http.MethodPost, "recovery-hook-test", api.TestRecoveryHookProfile, true)
	api.registerAPIMethod(m, http.MethodGet, "recovery-hook-assignments/:scopeType/:scopeKey", api.RecoveryHookAssignments, true)
	api.registerAPIMethod(m, http.MethodPost, "recovery-hook-assignments", api.SaveRecoveryHookAssignment, true)
	api.registerAPIRequest(m, "replication-analysis", api.ReplicationAnalysis)
	api.registerAPIRequest(m, "replication-analysis/:clusterName", api.ReplicationAnalysisForCluster)
	api.registerAPIRequest(m, "replication-analysis/instance/:host/:port", api.ReplicationAnalysisForKey)
	api.registerAPIRequest(m, "recover/:host/:port", api.Recover)
	api.registerAPIRequest(m, "recover/:host/:port/:candidateHost/:candidatePort", api.Recover)
	api.registerAPIRequest(m, "recover-lite/:host/:port", api.RecoverLite)
	api.registerAPIRequest(m, "recover-lite/:host/:port/:candidateHost/:candidatePort", api.RecoverLite)
	api.registerAPIRequest(m, "graceful-master-takeover/:host/:port", api.GracefulMasterTakeover)
	api.registerAPIRequest(m, "graceful-master-takeover/:host/:port/:designatedHost/:designatedPort", api.GracefulMasterTakeover)
	api.registerAPIRequest(m, "graceful-master-takeover/:clusterHint", api.GracefulMasterTakeover)
	api.registerAPIRequest(m, "graceful-master-takeover/:clusterHint/:designatedHost/:designatedPort", api.GracefulMasterTakeover)
	api.registerAPIRequest(m, "graceful-master-takeover-auto/:host/:port", api.GracefulMasterTakeoverAuto)
	api.registerAPIRequest(m, "graceful-master-takeover-auto/:host/:port/:designatedHost/:designatedPort", api.GracefulMasterTakeoverAuto)
	api.registerAPIRequest(m, "graceful-master-takeover-auto/:clusterHint", api.GracefulMasterTakeoverAuto)
	api.registerAPIRequest(m, "graceful-master-takeover-auto/:clusterHint/:designatedHost/:designatedPort", api.GracefulMasterTakeoverAuto)
	api.registerAPIRequest(m, "force-master-failover/:host/:port", api.ForceMasterFailover)
	api.registerAPIRequest(m, "force-master-failover/:clusterHint", api.ForceMasterFailover)
	api.registerAPIRequest(m, "force-master-takeover/:clusterHint/:designatedHost/:designatedPort", api.ForceMasterTakeover)
	api.registerAPIRequest(m, "force-master-takeover/:host/:port/:designatedHost/:designatedPort", api.ForceMasterTakeover)
	api.registerAPIRequest(m, "register-candidate/:host/:port/:promotionRule", api.RegisterCandidate)
	api.registerAPIRequest(m, "automated-recovery-filters", api.AutomatedRecoveryFilters)
	api.registerAPIRequest(m, "audit-failure-detection", api.AuditFailureDetection)
	api.registerAPIRequest(m, "audit-failure-detection/:page", api.AuditFailureDetection)
	api.registerAPIRequest(m, "audit-failure-detection/id/:id", api.AuditFailureDetection)
	api.registerAPIRequest(m, "audit-failure-detection/alias/:clusterAlias", api.AuditFailureDetection)
	api.registerAPIRequest(m, "audit-failure-detection/alias/:clusterAlias/:page", api.AuditFailureDetection)
	api.registerAPIRequest(m, "replication-analysis-changelog", api.ReadReplicationAnalysisChangelog)
	api.registerAPIRequest(m, "audit-recovery", api.AuditRecovery)
	api.registerAPIRequest(m, "audit-recovery/:page", api.AuditRecovery)
	api.registerAPIRequest(m, "audit-recovery/id/:id", api.AuditRecovery)
	api.registerAPIRequest(m, "audit-recovery/uid/:uid", api.AuditRecovery)
	api.registerAPIRequest(m, "audit-recovery/cluster/:clusterName", api.AuditRecovery)
	api.registerAPIRequest(m, "audit-recovery/cluster/:clusterName/:page", api.AuditRecovery)
	api.registerAPIRequest(m, "audit-recovery/alias/:clusterAlias", api.AuditRecovery)
	api.registerAPIRequest(m, "audit-recovery/alias/:clusterAlias/:page", api.AuditRecovery)
	api.registerAPIRequest(m, "audit-recovery-steps/:uid", api.AuditRecoverySteps)
	api.registerAPIRequest(m, "active-cluster-recovery/:clusterName", api.ActiveClusterRecovery)
	api.registerAPIRequest(m, "recently-active-cluster-recovery/:clusterName", api.RecentlyActiveClusterRecovery)
	api.registerAPIRequest(m, "recently-active-instance-recovery/:host/:port", api.RecentlyActiveInstanceRecovery)
	api.registerAPIRequest(m, "ack-recovery/cluster/:clusterHint", api.AcknowledgeClusterRecoveries)
	api.registerAPIRequest(m, "ack-recovery/cluster/alias/:clusterAlias", api.AcknowledgeClusterRecoveries)
	api.registerAPIRequest(m, "ack-recovery/instance/:host/:port", api.AcknowledgeInstanceRecoveries)
	api.registerAPIRequest(m, "ack-recovery/:recoveryId", api.AcknowledgeRecovery)
	api.registerAPIRequest(m, "ack-recovery/uid/:uid", api.AcknowledgeRecovery)
	api.registerAPIRequest(m, "ack-all-recoveries", api.AcknowledgeAllRecoveries)
	api.registerAPIRequest(m, "blocked-recoveries", api.BlockedRecoveries)
	api.registerAPIRequest(m, "blocked-recoveries/cluster/:clusterName", api.BlockedRecoveries)
	api.registerAPIRequest(m, "disable-global-recoveries", api.DisableGlobalRecoveries)
	api.registerAPIRequest(m, "enable-global-recoveries", api.EnableGlobalRecoveries)
	api.registerAPIRequest(m, "check-global-recoveries", api.CheckGlobalRecoveries)

	// General
	api.registerAPIRequest(m, "problems", api.Problems)
	api.registerAPIRequest(m, "problems/:clusterName", api.Problems)
	api.registerAPIRequest(m, "audit", api.Audit)
	api.registerAPIRequest(m, "audit/:page", api.Audit)
	api.registerAPIRequest(m, "audit/instance/:host/:port", api.Audit)
	api.registerAPIRequest(m, "audit/instance/:host/:port/:page", api.Audit)
	api.registerAPIRequest(m, "resolve/:host/:port", api.Resolve)

	// Meta, no proxy
	api.registerAPIRequestNoProxy(m, "headers", api.Headers)
	api.registerAPIRequestNoProxy(m, "health", api.Health)
	api.registerAPIRequestNoProxy(m, "lb-check", api.LBCheck)
	api.registerAPIRequestNoProxy(m, "_ping", api.LBCheck)
	api.registerAPIRequestNoProxy(m, "leader-check", api.LeaderCheck)
	api.registerAPIRequestNoProxy(m, "leader-check/:errorStatusCode", api.LeaderCheck)
	api.registerAPIRequestNoProxy(m, "raft/configuration", api.RaftConfiguration)
	api.registerAPIMethod(m, http.MethodPost, "raft/bootstrap", api.RaftBootstrap, false)
	api.registerAPIMethod(m, http.MethodPost, "raft/members", api.RaftAddMember, true)
	api.registerAPIMethod(m, http.MethodDelete, "raft/members/:id", api.RaftRemoveMember, true)
	api.registerAPIMethod(m, http.MethodPost, "raft/leadership/transfer", api.RaftLeadershipTransfer, true)
	api.registerAPIMethod(m, http.MethodPost, "raft/snapshot", api.RaftSnapshot, false)
	api.registerAPIRequestNoProxy(m, "raft-state", api.RaftState)
	api.registerAPIRequestNoProxy(m, "raft-leader", api.RaftLeader)
	api.registerAPIRequestNoProxy(m, "raft-health", api.RaftHealth)
	api.registerAPIRequestNoProxy(m, "raft-status", api.RaftStatus)
	api.registerAPIRequestNoProxy(m, "reload-configuration", api.ReloadConfiguration)
	api.registerAPIRequestNoProxy(m, "hostname-resolve-cache", api.HostnameResolveCache)
	api.registerAPIRequestNoProxy(m, "reset-hostname-resolve-cache", api.ResetHostnameResolveCache)
	// Meta
	api.registerAPIRequest(m, "routed-leader-check", api.LeaderCheck)
	api.registerAPIRequest(m, "reload-cluster-alias", api.ReloadClusterAlias)
	api.registerAPIRequest(m, "deregister-hostname-unresolve/:host/:port", api.DeregisterHostnameUnresolve)
	api.registerAPIRequest(m, "register-hostname-unresolve/:host/:port/:virtualname", api.RegisterHostnameUnresolve)

	// Bulk access to information
	api.registerAPIRequest(m, "bulk-instances", api.BulkInstances)
	api.registerAPIRequest(m, "bulk-promotion-rules", api.BulkPromotionRules)

	// Monitoring

	// Agents
	api.registerAPIRequest(m, "agents", api.Agents)
	api.registerAPIRequest(m, "agent/:host", api.Agent)
	api.registerAPIRequest(m, "agent-umount/:host", api.AgentUnmount)
	api.registerAPIRequest(m, "agent-mount/:host", api.AgentMountLV)
	api.registerAPIRequest(m, "agent-create-snapshot/:host", api.AgentCreateSnapshot)
	api.registerAPIRequest(m, "agent-removelv/:host", api.AgentRemoveLV)
	api.registerAPIRequest(m, "agent-mysql-stop/:host", api.AgentMySQLStop)
	api.registerAPIRequest(m, "agent-mysql-start/:host", api.AgentMySQLStart)
	api.registerAPIRequest(m, "agent-seed/:targetHost/:sourceHost", api.AgentSeed)
	api.registerAPIRequest(m, "agent-active-seeds/:host", api.AgentActiveSeeds)
	api.registerAPIRequest(m, "agent-recent-seeds/:host", api.AgentRecentSeeds)
	api.registerAPIRequest(m, "agent-seed-details/:seedId", api.AgentSeedDetails)
	api.registerAPIRequest(m, "agent-seed-states/:seedId", api.AgentSeedStates)
	api.registerAPIRequest(m, "agent-abort-seed/:seedId", api.AbortSeed)
	api.registerAPIRequest(m, "agent-custom-command/:host/:command", api.AgentCustomCommand)
	api.registerAPIRequest(m, "seeds", api.Seeds)

	// Configurable status check endpoint
	if config.Config.Server.Status.Endpoint == config.DefaultStatusAPIEndpoint {
		api.registerAPIRequestNoProxy(m, "status", api.StatusCheck)
	} else {
		m.Get(config.Config.Server.Status.Endpoint, api.StatusCheck)
	}

	setupMessagePrefix()
}
