/*
   Copyright 2017 Shlomi Noach, GitHub Inc.

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

package orcraft

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hashicorp/raft"
	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/golib/log"
	"github.com/openark/orchestrator/internal/models/dto"
	"github.com/openark/orchestrator/internal/models/vo"
)

const asyncSnapshotTimeframe = 1 * time.Minute

var store *Store
var runtimeMu sync.RWMutex
var lifecycleMu sync.Mutex
var runtimeCalls sync.WaitGroup
var runtimeDone chan struct{}
var leaderDone chan struct{}
var raftSetupComplete atomic.Int64
var ThisHostname string

var fatalRaftErrorChan = make(chan error, 1)

type leaderURI struct {
	uri string
	sync.Mutex
}

var LeaderURI leaderURI
var thisLeaderURI string

func (luri *leaderURI) Get() string {
	luri.Lock()
	defer luri.Unlock()
	return luri.uri
}

func (luri *leaderURI) Set(uri string) {
	luri.Lock()
	defer luri.Unlock()
	luri.uri = uri
}

func (luri *leaderURI) IsThisLeaderURI() bool {
	luri.Lock()
	defer luri.Unlock()
	return luri.uri == thisLeaderURI
}

// acquireStore pins the runtime until a call finishes, without holding a lock while FSM callbacks run.
func acquireStore() (*Store, func()) {
	runtimeMu.RLock()
	defer runtimeMu.RUnlock()
	if store == nil {
		return nil, func() {}
	}
	runtimeCalls.Add(1)
	return store, runtimeCalls.Done
}

func IsInitialized() bool {
	runtimeMu.RLock()
	defer runtimeMu.RUnlock()
	return store != nil
}

func FatalRaftError(err error) error {
	if err != nil && !enqueueFatalRaftError(fatalRaftErrorChan, err) {
		log.Sugar().Errorw("raft fatal error already pending", "error", err)
	}
	return err
}

func enqueueFatalRaftError(fatalErrors chan<- error, err error) bool {
	select {
	case fatalErrors <- err:
		return true
	default:
		return false
	}
}

func computeLeaderURI() (uri string, err error) {
	if config.Config.Server.HTTPAdvertise != "" {
		return config.Config.Server.HTTPAdvertise, nil
	}
	scheme := "http"
	if config.Config.Server.TLS.Enabled {
		scheme = "https"
	}

	hostname, _, err := net.SplitHostPort(config.Config.Raft.Advertise)
	if err != nil {
		return uri, fmt.Errorf("computeLeaderURI: cannot determine raft advertise host out of %q: %w", config.Config.Raft.Advertise, err)
	}
	_, port, err := net.SplitHostPort(config.Config.Server.Listen.Address)
	if err != nil || port == "" {
		return uri, fmt.Errorf("computeLeaderURI: cannot determine listen port out of config.Config.Server.Listen.Address: %+v", config.Config.Server.Listen.Address)
	}
	return fmt.Sprintf("%s://%s", scheme, net.JoinHostPort(hostname, port)), nil
}

// Setup creates the raft runtime. New clusters are not auto-bootstrapped.
func Setup(applier CommandApplier, snapshotCreatorApplier SnapshotCreatorApplier, thisHostname string) error {
	lifecycleMu.Lock()
	defer lifecycleMu.Unlock()
	runtimeMu.Lock()
	defer runtimeMu.Unlock()
	if store != nil {
		return fmt.Errorf("raft runtime is already initialized")
	}
	log.Debugf("Setting up raft")
	ThisHostname = thisHostname
	created := NewStore(config.Config.Raft.DataDir, config.Config.Raft.Bind, config.Config.Raft.Advertise, config.Config.Raft.NodeID, applier, snapshotCreatorApplier)
	if err := created.Open(); err != nil {
		_ = created.Close()
		return log.Errorf("failed to open raft store: %s", err.Error())
	}

	uri, err := computeLeaderURI()
	if err != nil {
		_ = created.Close()
		return FatalRaftError(err)
	}
	thisLeaderURI = uri
	if err := setupHttpClient(); err != nil {
		_ = created.Close()
		return fmt.Errorf("set up raft HTTP client: %w", err)
	}

	store = created
	leaderCh := store.raft.LeaderCh()
	runtimeDone = make(chan struct{})
	leaderDone = make(chan struct{})
	done, exited := runtimeDone, leaderDone
	go func() {
		defer close(exited)
		for {
			select {
			case <-done:
				return
			case isTurnedLeader := <-leaderCh:
				if isTurnedLeader {
					if _, err := PublishCommand("leader-uri", uri); err != nil {
						log.Errore(err)
					}
				}
			}
		}
	}()

	raftSetupComplete.Store(1)
	return nil
}

func Shutdown() error {
	lifecycleMu.Lock()
	defer lifecycleMu.Unlock()
	runtimeMu.Lock()
	raftSetupComplete.Store(0)
	if store == nil {
		runtimeMu.Unlock()
		return nil
	}
	close(runtimeDone)
	exited, current := leaderDone, store
	store = nil
	runtimeMu.Unlock()
	runtimeCalls.Wait()
	err := current.Close()
	<-exited
	LeaderURI.Set("")
	return err
}

func isRaftSetupComplete() bool {
	return raftSetupComplete.Load() == 1
}

func normalizeRaftNode(node string) (string, error) {
	return config.NormalizeRaftAddress(node, config.Config.Raft.DefaultPort)
}

// IsPartOfQuorum reports whether this node's data is trustworthy.
func IsPartOfQuorum() bool {
	return IsReady()
}

func IsLeader() bool {
	store, release := acquireStore()
	defer release()
	if store == nil {
		return false
	}
	return store.raft.State() == raft.Leader
}

func IsLeaderReady() bool {
	store, release := acquireStore()
	defer release()
	if store == nil {
		return false
	}
	return store.leaderVerified()
}

func IsReady() bool {
	store, release := acquireStore()
	defer release()
	if store == nil {
		return false
	}
	return store.Status().Ready
}

func GetLeader() string {
	store, release := acquireStore()
	defer release()
	if store == nil {
		return ""
	}
	_, id := store.raft.LeaderWithID()
	return string(id)
}

func GetLeaderAddress() string {
	store, release := acquireStore()
	defer release()
	if store == nil {
		return ""
	}
	addr, _ := store.raft.LeaderWithID()
	return string(addr)
}

func QuorumSize() (int, error) {
	store, release := acquireStore()
	defer release()
	if store == nil {
		return 0, ErrNotRunning
	}
	voters, err := store.voterCount()
	if err != nil {
		return 0, err
	}
	return voters/2 + 1, nil
}

func GetState() raft.RaftState {
	store, release := acquireStore()
	defer release()
	if store == nil {
		return raft.Shutdown
	}
	return store.raft.State()
}

func IsHealthy() bool {
	return IsReady()
}

func Snapshot() error {
	store, release := acquireStore()
	defer release()
	if store == nil {
		return ErrNotRunning
	}
	return store.Snapshot()
}

func AsyncSnapshot() error {
	asyncDuration := time.Duration(rand.Int63()) % asyncSnapshotTimeframe
	go time.AfterFunc(asyncDuration, func() {
		Snapshot()
	})
	return nil
}

func GetRaftBind() string {
	store, release := acquireStore()
	defer release()
	if store == nil {
		return ""
	}
	return store.raftBind
}

func GetRaftAdvertise() string {
	store, release := acquireStore()
	defer release()
	if store == nil {
		return ""
	}
	return store.raftAdvertise
}

func GetRaftNodeID() string {
	store, release := acquireStore()
	defer release()
	if store == nil {
		return ""
	}
	return store.nodeID
}

func GetClusterView() (vo.RaftCluster, error) {
	store, release := acquireStore()
	defer release()
	if store == nil {
		return vo.RaftCluster{}, ErrNotRunning
	}
	return store.GetClusterView()
}

func GetStatus() vo.RaftNodeStatus {
	store, release := acquireStore()
	defer release()
	if store == nil {
		return vo.RaftNodeStatus{}
	}
	return store.Status()
}

func Bootstrap() (vo.RaftConfiguration, error) {
	store, release := acquireStore()
	defer release()
	if store == nil {
		return vo.RaftConfiguration{}, ErrNotRunning
	}
	return store.Bootstrap()
}

func AddMember(req dto.RaftMember) (vo.RaftConfiguration, error) {
	store, release := acquireStore()
	defer release()
	if store == nil {
		return vo.RaftConfiguration{}, ErrNotRunning
	}
	if req.Address != "" {
		normalized, err := normalizeRaftNode(req.Address)
		if err != nil {
			return vo.RaftConfiguration{}, invalidArgument("member address is invalid: %v", err)
		}
		req.Address = normalized
	}
	return store.AddMember(req)
}

func RemoveMember(id string, expectedIndex *uint64) (vo.RaftConfiguration, error) {
	store, release := acquireStore()
	defer release()
	if store == nil {
		return vo.RaftConfiguration{}, ErrNotRunning
	}
	return store.RemoveMember(id, expectedIndex)
}

func TransferLeadership(id, address string) error {
	store, release := acquireStore()
	defer release()
	if store == nil {
		return ErrNotRunning
	}
	if address != "" {
		normalized, err := normalizeRaftNode(address)
		if err != nil {
			return invalidArgument("target address is invalid: %v", err)
		}
		address = normalized
	}
	return store.TransferLeadership(id, address)
}

func PublishCommand(op string, value any) (response any, err error) {
	store, release := acquireStore()
	defer release()
	if store == nil {
		return nil, ErrNotRunning
	}
	b, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return store.genericCommand(op, b)
}

// Members returns IDs from the latest Raft configuration. It does not claim
// that every configured member is currently reachable or healthy.
func Members() []string {
	store, release := acquireStore()
	defer release()
	if store == nil {
		return nil
	}
	view, err := store.GetClusterView()
	if err != nil {
		return nil
	}
	ids := make([]string, 0, len(view.Servers))
	for _, server := range view.Servers {
		ids = append(ids, server.ID)
	}
	return ids
}

// Monitor observes leadership state until the Raft runtime reports a fatal error.
func Monitor(ctx context.Context) error {
	tick := time.NewTicker(5 * time.Second)
	heartbeat := time.NewTicker(1 * time.Minute)
	defer tick.Stop()
	defer heartbeat.Stop()
	return monitor(ctx, tick.C, heartbeat.C, fatalRaftErrorChan)
}

func monitor(ctx context.Context, tick, heartbeat <-chan time.Time, fatalErrors <-chan error) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick:
			leaderHint := GetLeader()
			if IsLeader() {
				leaderHint = fmt.Sprintf("%s (this host)", leaderHint)
			}
			log.Debugf("raft leader is %s; state: %s", leaderHint, GetState().String())
		case <-heartbeat:
			if IsLeader() {
				go PublishCommand("heartbeat", "")
			}
		case err, ok := <-fatalErrors:
			if !ok {
				return fmt.Errorf("fatal raft error channel closed")
			}
			if err == nil {
				return fmt.Errorf("raft runtime reported a nil fatal error")
			}
			return fmt.Errorf("raft runtime failed: %w", err)
		}
	}
}
