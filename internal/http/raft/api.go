package raft

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/openark/orchestrator/internal/http/authz"
	"github.com/openark/orchestrator/internal/http/contract"
	"github.com/openark/orchestrator/internal/http/presenter"
	"github.com/openark/orchestrator/internal/http/transport"
	"github.com/openark/orchestrator/internal/models/dto"
	orcraft "github.com/openark/orchestrator/internal/raft"
)

type raftMemberBody struct {
	ID            string  `json:"id"`
	Address       string  `json:"address"`
	Suffrage      string  `json:"suffrage"`
	ExpectedIndex *uint64 `json:"expectedIndex"`
}

type raftTransferBody struct {
	ID      string `json:"id"`
	Address string `json:"address"`
}

type raftRemoveBody struct {
	ID            string  `json:"id"`
	ExpectedIndex *uint64 `json:"expectedIndex"`
}

// API contains the Raft administration handlers.
type API struct{}

func raftHTTPStatus(err error) int {
	switch orcraft.ClassOf(err) {
	case orcraft.ClassInvalidArgument:
		return http.StatusBadRequest
	case orcraft.ClassNotFound:
		return http.StatusNotFound
	case orcraft.ClassNotBootstrapped, orcraft.ClassNotLeader, orcraft.ClassConflict:
		return http.StatusConflict
	case orcraft.ClassIndeterminate, orcraft.ClassUnavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

func Respond(r transport.Responder, err error, okMessage string, details any) {
	if err == nil {
		presenter.Respond(r, &contract.Response{Code: contract.OK, Message: okMessage, Details: details})
		return
	}
	presenter.RespondStatus(r, raftHTTPStatus(err), &contract.Response{
		Code:       contract.ERROR,
		Message:    err.Error(),
		Details:    details,
		ErrorClass: string(orcraft.ClassOf(err)),
	})
}

func decodeOptionalJSON(req *http.Request, dst any) error {
	return decodeRaftJSON(req, dst, true)
}

func decodeRequiredJSON(req *http.Request, dst any) error {
	return decodeRaftJSON(req, dst, false)
}

func decodeRaftJSON(req *http.Request, dst any, optional bool) error {
	if req == nil || req.Body == nil {
		if optional {
			return nil
		}
		return orcraft.ErrInvalidArgument
	}
	decoder := json.NewDecoder(req.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		if optional && err == io.EOF {
			return nil
		}
		return orcraft.ErrInvalidArgument
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return orcraft.ErrInvalidArgument
	}
	return nil
}

func ExpectedIndexFromRequest(req *http.Request, bodyIndex *uint64) (*uint64, error) {
	if req == nil || req.URL == nil {
		return bodyIndex, nil
	}
	values, found := req.URL.Query()["expectedIndex"]
	if !found {
		return bodyIndex, nil
	}
	if len(values) != 1 || values[0] == "" {
		return nil, orcraft.ErrInvalidArgument
	}
	value, err := strconv.ParseUint(values[0], 10, 64)
	if err != nil {
		return nil, orcraft.ErrInvalidArgument
	}
	if bodyIndex != nil && *bodyIndex != value {
		return nil, orcraft.ErrInvalidArgument
	}
	if bodyIndex != nil {
		return bodyIndex, nil
	}
	return &value, nil
}

func (api *API) Configuration(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	view, err := orcraft.GetClusterView()
	Respond(r, err, "raft configuration", view)
}

func (api *API) Bootstrap(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForWrite(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	view, err := orcraft.Bootstrap()
	Respond(r, err, "raft cluster bootstrapped", view)
}

func (api *API) AddMember(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForWrite(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	var body raftMemberBody
	if err := decodeRequiredJSON(req, &body); err != nil {
		Respond(r, err, "", nil)
		return
	}
	expectedIndex, err := ExpectedIndexFromRequest(req, body.ExpectedIndex)
	if err != nil {
		Respond(r, err, "", nil)
		return
	}
	view, err := orcraft.AddMember(dto.RaftMember{
		ID:            body.ID,
		Address:       body.Address,
		Suffrage:      body.Suffrage,
		ExpectedIndex: expectedIndex,
	})
	Respond(r, err, "raft member added", view)
}

func (api *API) RemoveMember(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForWrite(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	var body raftRemoveBody
	if err := decodeOptionalJSON(req, &body); err != nil {
		Respond(r, err, "", nil)
		return
	}
	id := params["id"]
	if body.ID != "" && id != "" && body.ID != id {
		Respond(r, orcraft.ErrIdentityConflict, "", nil)
		return
	}
	if id == "" {
		id = body.ID
	}
	expectedIndex, err := ExpectedIndexFromRequest(req, body.ExpectedIndex)
	if err != nil {
		Respond(r, err, "", nil)
		return
	}
	view, err := orcraft.RemoveMember(id, expectedIndex)
	Respond(r, err, "raft member removed", view)
}

func (api *API) LeadershipTransfer(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForWrite(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	var body raftTransferBody
	if err := decodeOptionalJSON(req, &body); err != nil {
		Respond(r, err, "", nil)
		return
	}
	err := orcraft.TransferLeadership(body.ID, body.Address)
	Respond(r, err, "raft leadership transfer requested", nil)
}

func (api *API) Snapshot(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !authz.ForWrite(req, user) {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	err := orcraft.Snapshot()
	Respond(r, err, "snapshot created", nil)
}

func (api *API) State(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !orcraft.IsInitialized() {
		Respond(r, orcraft.ErrNotRunning, "", nil)
		return
	}
	presenter.WriteJSON(r, http.StatusOK, orcraft.GetState().String())
}

func (api *API) Leader(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !orcraft.IsInitialized() {
		Respond(r, orcraft.ErrNotRunning, "", nil)
		return
	}
	presenter.WriteJSON(r, http.StatusOK, map[string]string{
		"id":      orcraft.GetLeader(),
		"address": orcraft.GetLeaderAddress(),
	})
}

func (api *API) Health(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !orcraft.IsInitialized() {
		Respond(r, orcraft.ErrNotRunning, "", nil)
		return
	}
	status := orcraft.GetStatus()
	if !status.Ready {
		presenter.Respond(r, &contract.Response{Code: contract.ERROR, Message: "unhealthy", Details: status})
		return
	}
	presenter.WriteJSON(r, http.StatusOK, "healthy")
}

func (api *API) Status(params transport.Params, r transport.Responder, req *http.Request, user transport.Principal) {
	if !orcraft.IsInitialized() {
		Respond(r, orcraft.ErrNotRunning, "", nil)
		return
	}
	status := orcraft.GetStatus()
	view, err := orcraft.GetClusterView()
	if err != nil {
		Respond(r, err, "", status)
		return
	}
	presenter.WriteJSON(r, http.StatusOK, map[string]any{
		"status":        status,
		"configuration": view,
		"leaderURI":     orcraft.LeaderURI.Get(),
	})
}
