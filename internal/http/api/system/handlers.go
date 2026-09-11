package system

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/http/authz"
	"github.com/openark/orchestrator/internal/http/contract"
	"github.com/openark/orchestrator/internal/http/presenter"
	"github.com/openark/orchestrator/internal/http/transport"
	instaudit "github.com/openark/orchestrator/internal/inst/audit"
	"github.com/openark/orchestrator/internal/logic/discovery"
	"github.com/openark/orchestrator/internal/process"
)

// API contains local process health and configuration handlers.
type API struct{}

func (api *API) Headers(params transport.Params, r transport.Responder, req *http.Request) {
	presenter.WriteJSON(r, http.StatusOK, req.Header)
}

func (api *API) Health(params transport.Params, r transport.Responder, req *http.Request) {
	health, err := process.HealthTest()
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("Application node is unhealthy %+v", err), Details: health})
		return
	}
	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: "Application node is healthy", Details: health})
}

func (api *API) LBCheck(params transport.Params, r transport.Responder, req *http.Request) {
	presenter.WriteJSON(r, http.StatusOK, "OK")
}

func (api *API) LeaderCheck(params transport.Params, r transport.Responder, req *http.Request) {
	respondStatus, err := strconv.Atoi(params["errorStatusCode"])
	if err != nil || respondStatus < 0 {
		respondStatus = http.StatusNotFound
	}
	if discovery.IsLeader() {
		presenter.WriteJSON(r, http.StatusOK, "OK")
		return
	}
	presenter.WriteJSON(r, respondStatus, "Not leader")
}

func (api *API) StatusCheck(params transport.Params, r transport.Responder, req *http.Request) {
	health, err := process.HealthTest()
	if err != nil {
		presenter.WriteJSON(r, http.StatusInternalServerError, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("Application node is unhealthy %+v", err), Details: health})
		return
	}
	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: "Application node is healthy", Details: health})
}

func (api *API) ReloadConfiguration(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	extraConfigFile := req.URL.Query().Get("config")
	if _, err := config.Reload(extraConfigFile); err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("Cannot reload config: %+v", err)})
		return
	}
	instaudit.AuditOperation("reload-configuration", nil, "Triggered via API")
	presenter.Respond(r, &contract.Response{Code: contract.OK, Message: "Config reloaded", Details: extraConfigFile})
}
