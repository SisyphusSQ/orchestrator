package orcraft

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/raft"
	"github.com/openark/orchestrator/internal/models/dto"
	"github.com/openark/orchestrator/internal/models/vo"
)

const (
	suffrageVoter    = "voter"
	suffrageNonvoter = "nonvoter"
)

func (store *Store) configuration() (raft.Configuration, uint64, error) {
	if store == nil || store.raft == nil || store.configLog == nil {
		return raft.Configuration{}, 0, ErrNotRunning
	}
	deadline := time.Now().Add(raftTimeout)
	for {
		future := store.raft.GetConfiguration()
		if err := future.Error(); err != nil {
			return raft.Configuration{}, 0, classifyRaftError(err)
		}
		configuration := future.Configuration()
		lastSnapshotIndex, err := strconv.ParseUint(store.raft.Stats()["last_snapshot_index"], 10, 64)
		if err != nil {
			return raft.Configuration{}, 0, wrapError(ClassFailed, "read raft last snapshot index", err)
		}
		if err := store.configLog.refreshForSnapshotIndex(lastSnapshotIndex); err != nil {
			return raft.Configuration{}, 0, wrapError(ClassFailed, "refresh raft configuration from snapshot", err)
		}
		trackedConfiguration, index := store.configLog.latest()
		if configurationsEqual(configuration, trackedConfiguration) {
			return configuration, index, nil
		}
		if err := store.configLog.refresh(); err != nil {
			return raft.Configuration{}, 0, wrapError(ClassFailed, "refresh raft configuration index", err)
		}
		if time.Now().After(deadline) {
			return raft.Configuration{}, 0, newError(ClassFailed, "pair raft configuration with its persisted index")
		}
		time.Sleep(time.Millisecond)
	}
}

func configurationsEqual(left, right raft.Configuration) bool {
	if len(left.Servers) != len(right.Servers) {
		return false
	}
	for index := range left.Servers {
		if left.Servers[index] != right.Servers[index] {
			return false
		}
	}
	return true
}

func mapConfiguration(cfg raft.Configuration, index uint64) vo.RaftConfiguration {
	view := vo.RaftConfiguration{Index: index, Servers: make([]vo.RaftServer, 0, len(cfg.Servers))}
	for _, server := range cfg.Servers {
		view.Servers = append(view.Servers, vo.RaftServer{
			ID:       string(server.ID),
			Address:  string(server.Address),
			Suffrage: suffrageName(server.Suffrage),
		})
	}
	return view
}

func configurationCommitted(hasServers bool, configurationIndex, commitIndex uint64) bool {
	return hasServers && configurationIndex > 0 && configurationIndex <= commitIndex
}

func (store *Store) configurationView(cfg raft.Configuration, index uint64) vo.RaftConfiguration {
	view := mapConfiguration(cfg, index)
	view.Committed = store != nil && store.raft != nil && configurationCommitted(len(cfg.Servers) > 0, index, store.raft.CommitIndex())
	return view
}

func (store *Store) membershipConfiguration() (raft.Configuration, uint64, vo.RaftConfiguration, error) {
	deadline := time.Now().Add(raftTimeout)
	for {
		cfg, index, err := store.configuration()
		if err != nil {
			return raft.Configuration{}, 0, vo.RaftConfiguration{}, err
		}
		if len(cfg.Servers) == 0 {
			return raft.Configuration{}, 0, vo.RaftConfiguration{}, ErrNotBootstrapped
		}
		view := store.configurationView(cfg, index)
		if store.raft.State() != raft.Leader {
			return cfg, index, view, ErrNotLeader
		}
		if view.Committed {
			return cfg, index, view, nil
		}
		if time.Now().After(deadline) {
			return cfg, index, view, ErrConfigurationInProgress
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func suffrageName(s raft.ServerSuffrage) string {
	switch s {
	case raft.Voter:
		return suffrageVoter
	case raft.Nonvoter:
		return suffrageNonvoter
	default:
		return strings.ToLower(s.String())
	}
}

func parseSuffrage(value string) (raft.ServerSuffrage, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case suffrageVoter:
		return raft.Voter, nil
	case suffrageNonvoter:
		return raft.Nonvoter, nil
	default:
		return raft.Voter, invalidArgument("suffrage must be voter or nonvoter")
	}
}

func (store *Store) GetClusterView() (vo.RaftCluster, error) {
	cfg, index, err := store.configuration()
	if err != nil {
		return vo.RaftCluster{}, err
	}
	leaderAddr, leaderID := store.raft.LeaderWithID()
	return vo.RaftCluster{
		RaftConfiguration: store.configurationView(cfg, index),
		LeaderID:          string(leaderID),
		LeaderAddress:     string(leaderAddr),
		LocalID:           store.nodeID,
		LocalAddress:      store.raftAdvertise,
		LocalState:        store.raft.State().String(),
	}, nil
}

func (store *Store) hasExistingState() (bool, error) {
	if store.logStore == nil || store.stableStore == nil || store.snapshots == nil {
		return false, ErrNotRunning
	}
	return raft.HasExistingState(store.logStore, store.stableStore, store.snapshots)
}

func (store *Store) Bootstrap() (vo.RaftConfiguration, error) {
	if store == nil || store.raft == nil {
		return vo.RaftConfiguration{}, ErrNotRunning
	}
	existing, err := store.hasExistingState()
	if err != nil {
		return vo.RaftConfiguration{}, classifyRaftError(err)
	}
	if existing {
		return vo.RaftConfiguration{}, ErrAlreadyBootstrapped
	}
	cfg, _, err := store.configuration()
	if err != nil {
		return vo.RaftConfiguration{}, err
	}
	if len(cfg.Servers) > 0 {
		return vo.RaftConfiguration{}, ErrAlreadyBootstrapped
	}

	bootstrap := raft.Configuration{
		Servers: []raft.Server{{
			Suffrage: raft.Voter,
			ID:       raft.ServerID(store.nodeID),
			Address:  raft.ServerAddress(store.raftAdvertise),
		}},
	}
	futureErr := waitFuture(store.raft.BootstrapCluster(bootstrap), raftTimeout)
	if futureErr != nil && !isIndeterminate(futureErr) {
		return vo.RaftConfiguration{}, classifyRaftError(futureErr)
	}

	deadline := time.Now().Add(raftTimeout)
	for {
		view, readErr := store.currentConfigurationView()
		if readErr != nil {
			return vo.RaftConfiguration{}, wrapError(ClassIndeterminate, "raft bootstrap result is indeterminate", readErr)
		}
		if bootstrapConfigurationMatches(view, store.nodeID, store.raftAdvertise) {
			if view.Committed {
				return view, nil
			}
		} else if view.Committed && len(view.Servers) > 0 {
			return view, ErrAlreadyBootstrapped
		}
		if time.Now().After(deadline) {
			if futureErr == nil {
				futureErr = ErrConfigurationInProgress
			}
			return view, wrapError(ClassIndeterminate, "raft bootstrap result is indeterminate", futureErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func bootstrapConfigurationMatches(view vo.RaftConfiguration, id, address string) bool {
	return len(view.Servers) == 1 &&
		view.Servers[0].ID == id &&
		view.Servers[0].Address == address &&
		view.Servers[0].Suffrage == suffrageVoter
}

func (store *Store) currentConfigurationView() (vo.RaftConfiguration, error) {
	cfg, index, err := store.configuration()
	if err != nil {
		return vo.RaftConfiguration{}, err
	}
	return store.configurationView(cfg, index), nil
}

func (store *Store) AddMember(req dto.RaftMember) (vo.RaftConfiguration, error) {
	if store == nil || store.raft == nil {
		return vo.RaftConfiguration{}, ErrNotRunning
	}
	id := strings.TrimSpace(req.ID)
	address := strings.TrimSpace(req.Address)
	if id == "" {
		return vo.RaftConfiguration{}, invalidArgument("member id is required")
	}
	if address == "" {
		return vo.RaftConfiguration{}, invalidArgument("member address is required")
	}
	wantSuffrage, err := parseSuffrage(req.Suffrage)
	if err != nil {
		return vo.RaftConfiguration{}, err
	}

	store.membershipMu.Lock()
	defer store.membershipMu.Unlock()
	cfg, index, view, err := store.membershipConfiguration()
	if err != nil {
		return view, err
	}
	if req.ExpectedIndex != nil && *req.ExpectedIndex != index {
		return view, ErrStaleConfiguration
	}

	if conflict := membershipConflict(cfg, raft.ServerID(id), raft.ServerAddress(address)); conflict != nil {
		return view, conflict
	}
	if existing, ok := findServerByID(cfg, raft.ServerID(id)); ok {
		if existing.Address == raft.ServerAddress(address) && existing.Suffrage == wantSuffrage {
			return view, nil
		}
	}

	prevIndex := index
	var future raft.IndexFuture
	switch wantSuffrage {
	case raft.Voter:
		future = store.raft.AddVoter(raft.ServerID(id), raft.ServerAddress(address), prevIndex, raftTimeout)
	case raft.Nonvoter:
		if existing, ok := findServerByID(cfg, raft.ServerID(id)); ok && existing.Address == raft.ServerAddress(address) && existing.Suffrage == raft.Voter {
			future = store.raft.DemoteVoter(raft.ServerID(id), prevIndex, raftTimeout)
		} else {
			future = store.raft.AddNonvoter(raft.ServerID(id), raft.ServerAddress(address), prevIndex, raftTimeout)
		}
	}
	return store.finishMembership(future, func(view vo.RaftConfiguration) bool {
		server, ok := findServerView(view, id)
		return ok && server.Address == address && server.Suffrage == suffrageName(wantSuffrage)
	})
}

func (store *Store) RemoveMember(id string, expectedIndex *uint64) (vo.RaftConfiguration, error) {
	if store == nil || store.raft == nil {
		return vo.RaftConfiguration{}, ErrNotRunning
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return vo.RaftConfiguration{}, invalidArgument("member id is required")
	}
	store.membershipMu.Lock()
	defer store.membershipMu.Unlock()
	cfg, index, view, err := store.membershipConfiguration()
	if err != nil {
		return view, err
	}
	if expectedIndex != nil && *expectedIndex != index {
		return view, ErrStaleConfiguration
	}
	if _, ok := findServerByID(cfg, raft.ServerID(id)); !ok {
		return view, ErrNotFound
	}
	future := store.raft.RemoveServer(raft.ServerID(id), index, raftTimeout)
	return store.finishMembership(future, func(view vo.RaftConfiguration) bool {
		_, ok := findServerView(view, id)
		return !ok
	})
}

func (store *Store) TransferLeadership(id, address string) error {
	if store == nil || store.raft == nil {
		return ErrNotRunning
	}
	id = strings.TrimSpace(id)
	address = strings.TrimSpace(address)
	if id == "" && address != "" {
		return invalidArgument("target id is required when target address is provided")
	}

	store.membershipMu.Lock()
	defer store.membershipMu.Unlock()
	cfg, _, _, err := store.membershipConfiguration()
	if err != nil {
		return err
	}
	if id == "" {
		for _, server := range cfg.Servers {
			if server.ID != raft.ServerID(store.nodeID) && server.Suffrage == raft.Voter {
				return classifyLeadershipTransferError(waitFuture(store.raft.LeadershipTransfer(), raftTimeout))
			}
		}
		return newError(ClassConflict, "raft leadership transfer requires another voter")
	}
	if id == store.nodeID {
		return invalidArgument("leadership transfer target must not be the local server")
	}
	server, ok := findServerByID(cfg, raft.ServerID(id))
	if !ok {
		return ErrNotFound
	}
	if address != "" && server.Address != raft.ServerAddress(address) {
		return ErrIdentityConflict
	}
	if server.Suffrage != raft.Voter {
		return invalidArgument("leadership transfer target must be a voter")
	}
	return classifyLeadershipTransferError(waitFuture(store.raft.LeadershipTransferToServer(server.ID, server.Address), raftTimeout))
}

func (store *Store) Snapshot() error {
	if store == nil || store.raft == nil {
		return ErrNotRunning
	}
	future := store.raft.Snapshot()
	if err := waitFuture(future, raftTimeout); err != nil {
		return classifyRaftError(err)
	}
	return nil
}

func (store *Store) finishMembership(future raft.IndexFuture, succeeded func(vo.RaftConfiguration) bool) (vo.RaftConfiguration, error) {
	err := waitFuture(future, raftTimeout)
	if err == nil {
		return store.currentConfigurationView()
	}
	if !isIndeterminate(err) {
		return vo.RaftConfiguration{}, classifyRaftError(err)
	}
	view, readErr := store.currentConfigurationView()
	if readErr != nil {
		return vo.RaftConfiguration{}, wrapError(ClassIndeterminate, "raft mutation result is indeterminate", readErr)
	}
	if view.Committed && succeeded(view) {
		return view, nil
	}
	return view, wrapError(ClassIndeterminate, "raft mutation result is indeterminate", err)
}

func isIndeterminate(err error) bool {
	if err == nil {
		return false
	}
	if errorsIsTimeout(err) {
		return true
	}
	return errors.Is(err, raft.ErrLeadershipLost)
}

func membershipConflict(cfg raft.Configuration, id raft.ServerID, address raft.ServerAddress) error {
	idMatch, idFound := findServerByID(cfg, id)
	addrMatch, addrFound := findServerByAddress(cfg, address)
	if idFound && idMatch.Address != address {
		return ErrIdentityConflict
	}
	if addrFound && addrMatch.ID != id {
		return ErrIdentityConflict
	}
	return nil
}

func findServerByID(cfg raft.Configuration, id raft.ServerID) (raft.Server, bool) {
	for _, server := range cfg.Servers {
		if server.ID == id {
			return server, true
		}
	}
	return raft.Server{}, false
}

func findServerByAddress(cfg raft.Configuration, address raft.ServerAddress) (raft.Server, bool) {
	for _, server := range cfg.Servers {
		if server.Address == address {
			return server, true
		}
	}
	return raft.Server{}, false
}

func findServerView(view vo.RaftConfiguration, id string) (vo.RaftServer, bool) {
	for _, server := range view.Servers {
		if server.ID == id {
			return server, true
		}
	}
	return vo.RaftServer{}, false
}

func waitFuture(future raft.Future, timeout time.Duration) error {
	if future == nil {
		return invalidArgument("missing raft future")
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- future.Error()
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-errCh:
		return err
	case <-timer.C:
		return fmt.Errorf("%w", ErrTimeout)
	}
}

func errorsIsTimeout(err error) bool {
	return err != nil && errors.Is(err, ErrTimeout)
}
