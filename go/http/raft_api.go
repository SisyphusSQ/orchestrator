package http

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	orcraft "github.com/openark/orchestrator/go/raft"
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

func raftHTTPStatus(err error) int {
	switch orcraft.ClassOf(err) {
	case orcraft.ClassInvalidArgument, orcraft.ClassDisabled:
		return http.StatusBadRequest
	case orcraft.ClassNotFound:
		return http.StatusNotFound
	case orcraft.ClassNotBootstrapped, orcraft.ClassNotLeader, orcraft.ClassConflict:
		return http.StatusConflict
	case orcraft.ClassIndeterminate:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

func respondRaft(r Responder, err error, okMessage string, details interface{}) {
	if err == nil {
		Respond(r, &APIResponse{Code: OK, Message: okMessage, Details: details})
		return
	}
	RespondStatus(r, raftHTTPStatus(err), &APIResponse{
		Code:       ERROR,
		Message:    err.Error(),
		Details:    details,
		ErrorClass: string(orcraft.ClassOf(err)),
	})
}

func decodeOptionalJSON(req *http.Request, dst interface{}) error {
	return decodeRaftJSON(req, dst, true)
}

func decodeRequiredJSON(req *http.Request, dst interface{}) error {
	return decodeRaftJSON(req, dst, false)
}

func decodeRaftJSON(req *http.Request, dst interface{}, optional bool) error {
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

func expectedIndexFromRequest(req *http.Request, bodyIndex *uint64) (*uint64, error) {
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

func (this *HttpAPI) RaftConfiguration(params Params, r Responder, req *http.Request, user Principal) {
	view, err := orcraft.GetClusterView()
	respondRaft(r, err, "raft configuration", view)
}

func (this *HttpAPI) RaftBootstrap(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForWrite(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	view, err := orcraft.Bootstrap()
	respondRaft(r, err, "raft cluster bootstrapped", view)
}

func (this *HttpAPI) RaftAddMember(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForWrite(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	var body raftMemberBody
	if err := decodeRequiredJSON(req, &body); err != nil {
		respondRaft(r, err, "", nil)
		return
	}
	expectedIndex, err := expectedIndexFromRequest(req, body.ExpectedIndex)
	if err != nil {
		respondRaft(r, err, "", nil)
		return
	}
	view, err := orcraft.AddMember(orcraft.MemberRequest{
		ID:            body.ID,
		Address:       body.Address,
		Suffrage:      body.Suffrage,
		ExpectedIndex: expectedIndex,
	})
	respondRaft(r, err, "raft member added", view)
}

func (this *HttpAPI) RaftRemoveMember(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForWrite(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	var body raftRemoveBody
	if err := decodeOptionalJSON(req, &body); err != nil {
		respondRaft(r, err, "", nil)
		return
	}
	id := params["id"]
	if body.ID != "" && id != "" && body.ID != id {
		respondRaft(r, orcraft.ErrIdentityConflict, "", nil)
		return
	}
	if id == "" {
		id = body.ID
	}
	expectedIndex, err := expectedIndexFromRequest(req, body.ExpectedIndex)
	if err != nil {
		respondRaft(r, err, "", nil)
		return
	}
	view, err := orcraft.RemoveMember(id, expectedIndex)
	respondRaft(r, err, "raft member removed", view)
}

func (this *HttpAPI) RaftLeadershipTransfer(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForWrite(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	var body raftTransferBody
	if err := decodeOptionalJSON(req, &body); err != nil {
		respondRaft(r, err, "", nil)
		return
	}
	err := orcraft.TransferLeadership(body.ID, body.Address)
	respondRaft(r, err, "raft leadership transfer requested", nil)
}

func (this *HttpAPI) RaftSnapshot(params Params, r Responder, req *http.Request, user Principal) {
	if !isAuthorizedForWrite(req, user) {
		Respond(r, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	err := orcraft.Snapshot()
	respondRaft(r, err, "snapshot created", nil)
}

func (this *HttpAPI) RaftState(params Params, r Responder, req *http.Request, user Principal) {
	if !orcraft.IsRaftEnabled() {
		respondRaft(r, orcraft.RaftNotRunning, "", nil)
		return
	}
	r.JSON(http.StatusOK, orcraft.GetState().String())
}

func (this *HttpAPI) RaftLeader(params Params, r Responder, req *http.Request, user Principal) {
	if !orcraft.IsRaftEnabled() {
		respondRaft(r, orcraft.RaftNotRunning, "", nil)
		return
	}
	r.JSON(http.StatusOK, map[string]string{
		"id":      orcraft.GetLeader(),
		"address": orcraft.GetLeaderAddress(),
	})
}

func (this *HttpAPI) RaftHealth(params Params, r Responder, req *http.Request, user Principal) {
	if !orcraft.IsRaftEnabled() {
		respondRaft(r, orcraft.RaftNotRunning, "", nil)
		return
	}
	status := orcraft.GetStatus()
	if !status.Ready {
		Respond(r, &APIResponse{Code: ERROR, Message: "unhealthy", Details: status})
		return
	}
	r.JSON(http.StatusOK, "healthy")
}

func (this *HttpAPI) RaftStatus(params Params, r Responder, req *http.Request, user Principal) {
	if !orcraft.IsRaftEnabled() {
		respondRaft(r, orcraft.RaftNotRunning, "", nil)
		return
	}
	status := orcraft.GetStatus()
	view, err := orcraft.GetClusterView()
	if err != nil {
		respondRaft(r, err, "", status)
		return
	}
	r.JSON(http.StatusOK, map[string]interface{}{
		"status":        status,
		"configuration": view,
		"leaderURI":     orcraft.LeaderURI.Get(),
	})
}
