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

package agent

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	agentservice "github.com/openark/orchestrator/internal/agent"
	"github.com/openark/orchestrator/internal/attributes"
	"github.com/openark/orchestrator/internal/http/contract"
	"github.com/openark/orchestrator/internal/http/presenter"
	"github.com/openark/orchestrator/internal/http/transport"
)

type RegistrationAPI struct {
	URLPrefix string
}

// SubmitAgent registers an agent. It is initiated by an agent to register itself.
func (api *RegistrationAPI) SubmitAgent(params transport.Params, r transport.Responder) {
	port, err := strconv.Atoi(params["port"])
	if err != nil {
		presenter.WriteJSON(r, 200, &contract.Response{Code: contract.ERROR, Message: err.Error()})
		return
	}

	output, err := agentservice.SubmitAgent(params["host"], port, params["token"])
	if err != nil {
		presenter.WriteJSON(r, 200, &contract.Response{Code: contract.ERROR, Message: err.Error()})
		return
	}
	presenter.WriteJSON(r, 200, output)
}

// SetHostAttribute is a utility method that allows per-host key-value store.
func (api *RegistrationAPI) SetHostAttribute(params transport.Params, r transport.Responder, req *http.Request) {
	err := attributes.SetHostAttributes(params["host"], params["attrVame"], params["attrValue"])

	if err != nil {
		presenter.WriteJSON(r, 200, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err)})
		return
	}

	presenter.WriteJSON(r, 200, (err == nil))
}

// GetHostAttributeByAttributeName returns a host attribute
func (api *RegistrationAPI) GetHostAttributeByAttributeName(params transport.Params, r transport.Responder, req *http.Request) {

	output, err := attributes.GetHostAttributesByAttribute(params["attr"], req.URL.Query().Get("valueMatch"))

	if err != nil {
		presenter.WriteJSON(r, 200, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err)})
		return
	}

	presenter.WriteJSON(r, 200, output)
}

// AgentsHosts provides list of agent host names
func (api *RegistrationAPI) AgentsHosts(params transport.Params, r transport.Responder, req *http.Request) string {
	agents, err := agentservice.ReadAgents()
	hostnames := []string{}
	for _, agent := range agents {
		hostnames = append(hostnames, agent.Hostname)
	}

	if err != nil {
		presenter.WriteJSON(r, 200, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err)})
		return ""
	}

	if req.URL.Query().Get("format") == "txt" {
		return strings.Join(hostnames, "\n")
	} else {
		presenter.WriteJSON(r, 200, hostnames)
	}
	return ""
}

// AgentsInstances provides list of assumed MySQL instances (host:port)
func (api *RegistrationAPI) AgentsInstances(params transport.Params, r transport.Responder, req *http.Request) string {
	agents, err := agentservice.ReadAgents()
	hostnames := []string{}
	for _, agent := range agents {
		hostnames = append(hostnames, fmt.Sprintf("%s:%d", agent.Hostname, agent.MySQLPort))
	}

	if err != nil {
		presenter.WriteJSON(r, 200, &contract.Response{Code: contract.ERROR, Message: fmt.Sprintf("%+v", err)})
		return ""
	}

	if req.URL.Query().Get("format") == "txt" {
		return strings.Join(hostnames, "\n")
	} else {
		presenter.WriteJSON(r, 200, hostnames)
	}
	return ""
}

func (api *RegistrationAPI) AgentPing(params transport.Params, r transport.Responder, req *http.Request) {
	presenter.WriteJSON(r, 200, "OK")
}

// RegisterRequests makes for the de-facto list of known API calls
func (api *RegistrationAPI) RegisterRequests(m *transport.Router) {
	m.Get(api.URLPrefix+"/api/submit-agent/:host/:port/:token", api.SubmitAgent)
	m.Get(api.URLPrefix+"/api/host-attribute/:host/:attrVame/:attrValue", api.SetHostAttribute)
	m.Get(api.URLPrefix+"/api/host-attribute/attr/:attr/", api.GetHostAttributeByAttributeName)
	m.Get(api.URLPrefix+"/api/agents-hosts", api.AgentsHosts)
	m.Get(api.URLPrefix+"/api/agents-instances", api.AgentsInstances)
	m.Get(api.URLPrefix+"/api/agent-ping", api.AgentPing)
}
