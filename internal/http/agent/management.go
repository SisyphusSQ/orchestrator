package agent

import (
	"fmt"
	"net/http"
	"strconv"

	agentservice "github.com/openark/orchestrator/internal/agent"
	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/http/authz"
	"github.com/openark/orchestrator/internal/http/contract"
	"github.com/openark/orchestrator/internal/http/presenter"
	"github.com/openark/orchestrator/internal/http/transport"
	orcraft "github.com/openark/orchestrator/internal/raft"
)

// ManagementAPI contains the agent and seed handlers served by the standard API.
type ManagementAPI struct{}

func execute(req *http.Request, user transport.Principal, r transport.Responder, operation func() (any, error)) {
	if !authz.ForAction(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	if !config.Config.Agents.ServeHTTP {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Agents not served"})
		return
	}
	output, err := operation()
	if err != nil {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err), ErrorClass: string(orcraft.ClassOf(err))})
		return
	}
	presenter.WriteJSON(r, http.StatusOK, output)
}

func (api *ManagementAPI) Agents(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	execute(req, user, r, func() (any, error) { return agentservice.ReadAgents() })
}

func (api *ManagementAPI) Agent(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	execute(req, user, r, func() (any, error) { return agentservice.GetAgent(params["host"]) })
}

func (api *ManagementAPI) Unmount(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	execute(req, user, r, func() (any, error) { return agentservice.Unmount(params["host"]) })
}

func (api *ManagementAPI) MountLV(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	execute(req, user, r, func() (any, error) { return agentservice.MountLV(params["host"], req.URL.Query().Get("lv")) })
}

func (api *ManagementAPI) CreateSnapshot(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	execute(req, user, r, func() (any, error) { return agentservice.CreateSnapshot(params["host"]) })
}

func (api *ManagementAPI) RemoveLV(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	execute(req, user, r, func() (any, error) { return agentservice.RemoveLV(params["host"], req.URL.Query().Get("lv")) })
}

func (api *ManagementAPI) MySQLStop(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	execute(req, user, r, func() (any, error) { return agentservice.MySQLStop(params["host"]) })
}

func (api *ManagementAPI) MySQLStart(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	execute(req, user, r, func() (any, error) { return agentservice.MySQLStart(params["host"]) })
}

func (api *ManagementAPI) CustomCommand(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	execute(req, user, r, func() (any, error) { return agentservice.CustomCommand(params["host"], params["command"]) })
}

func (api *ManagementAPI) Seed(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	execute(req, user, r, func() (any, error) { return agentservice.Seed(params["targetHost"], params["sourceHost"]) })
}

func (api *ManagementAPI) ActiveSeeds(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	execute(req, user, r, func() (any, error) { return agentservice.ReadActiveSeedsForHost(params["host"]) })
}

func (api *ManagementAPI) RecentSeeds(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	execute(req, user, r, func() (any, error) { return agentservice.ReadRecentCompletedSeedsForHost(params["host"]) })
}

func (api *ManagementAPI) SeedDetails(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	execute(req, user, r, func() (any, error) {
		seedID, err := strconv.ParseInt(params["seedId"], 10, 0)
		if err != nil {
			return nil, err
		}
		return agentservice.AgentSeedDetails(seedID)
	})
}

func (api *ManagementAPI) SeedStates(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	execute(req, user, r, func() (any, error) {
		seedID, err := strconv.ParseInt(params["seedId"], 10, 0)
		if err != nil {
			return nil, err
		}
		return agentservice.ReadSeedStates(seedID)
	})
}

func (api *ManagementAPI) Seeds(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	execute(req, user, r, func() (any, error) { return agentservice.ReadRecentSeeds() })
}

func (api *ManagementAPI) AbortSeed(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	execute(req, user, r, func() (any, error) {
		seedID, err := strconv.ParseInt(params["seedId"], 10, 0)
		if err != nil {
			return nil, err
		}
		err = agentservice.AbortSeed(seedID)
		return err == nil, err
	})
}
