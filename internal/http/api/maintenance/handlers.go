package maintenance

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/openark/orchestrator/internal/golib/util"
	"github.com/openark/orchestrator/internal/http/authz"
	"github.com/openark/orchestrator/internal/http/contract"
	"github.com/openark/orchestrator/internal/http/presenter"
	"github.com/openark/orchestrator/internal/http/request"
	"github.com/openark/orchestrator/internal/http/transport"
	instdowntime "github.com/openark/orchestrator/internal/inst/downtime"
	instmaintenance "github.com/openark/orchestrator/internal/inst/maintenance"
	orcraft "github.com/openark/orchestrator/internal/raft"
)

// API contains maintenance and downtime handlers.
type API struct{}

func (api *API) Begin(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	durationSeconds := 0
	if duration := req.URL.Query().Get("duration"); duration != "" {
		durationSeconds, err = util.SimpleTimeToSeconds(duration)
		if err != nil || durationSeconds < 0 {
			presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Invalid maintenance duration"})
			return
		}
	}
	key, err := instmaintenance.BeginBoundedMaintenance(&instanceKey, params["owner"], params["reason"], uint(durationSeconds), true)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err)), Details: key})
		return
	}
	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("Maintenance begun: %+v", instanceKey), Details: instanceKey})
}

func (api *API) End(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	maintenanceKey, err := strconv.ParseInt(params["maintenanceKey"], 10, 0)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	if _, err = instmaintenance.EndMaintenance(maintenanceKey); err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("Maintenance ended: %+v", maintenanceKey), Details: maintenanceKey})
}

func (api *API) EndByInstance(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	if _, err = instmaintenance.EndMaintenanceByInstanceKey(&instanceKey); err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("Maintenance ended: %+v", instanceKey), Details: instanceKey})
}

func (api *API) InMaintenance(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	inMaintenance, err := instmaintenance.InMaintenance(&instanceKey)
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	details := ""
	if inMaintenance {
		details = instanceKey.StringCode()
	}
	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("%+v", inMaintenance), Details: details})
}

func (api *API) List(params transport.Params, r transport.Responder, req *http.Request) {
	maintenanceList, err := instmaintenance.ReadActiveMaintenance()
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	presenter.WriteJSON(r, http.StatusOK, maintenanceList)
}

func (api *API) BeginDowntime(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	durationSeconds := 0
	if params["duration"] != "" {
		durationSeconds, err = util.SimpleTimeToSeconds(params["duration"])
		if durationSeconds < 0 {
			err = fmt.Errorf("duration value must be non-negative. Given value: %d", durationSeconds)
		}
		if err != nil {
			presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
			return
		}
	}
	downtime := instdowntime.NewDowntime(&instanceKey, params["owner"], params["reason"], time.Duration(durationSeconds)*time.Second)
	if _, err = orcraft.PublishCommand("begin-downtime", downtime); err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err)), Details: instanceKey})
		return
	}
	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("Downtime begun: %+v", instanceKey), Details: instanceKey})
}

func (api *API) EndDowntime(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	instanceKey, err := request.ResolveInstanceKey(params["host"], params["port"])
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	if _, err = orcraft.PublishCommand("end-downtime", instanceKey); err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: err.Error(), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: fmt.Sprintf("Downtime ended: %+v", instanceKey), Details: instanceKey})
}
