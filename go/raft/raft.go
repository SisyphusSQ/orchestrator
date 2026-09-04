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
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hashicorp/raft"
	"github.com/openark/golib/log"
	"github.com/openark/orchestrator/go/config"
)

const asyncSnapshotTimeframe = 1 * time.Minute

var store *Store
var raftSetupComplete int64
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

func IsRaftEnabled() bool {
	return store != nil && store.raft != nil
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
	if config.Config.HTTPAdvertise != "" {
		return config.Config.HTTPAdvertise, nil
	}
	scheme := "http"
	if config.Config.UseSSL {
		scheme = "https"
	}

	hostname, _, err := net.SplitHostPort(config.Config.RaftAdvertise)
	if err != nil {
		return uri, fmt.Errorf("computeLeaderURI: cannot determine raft advertise host out of %q: %w", config.Config.RaftAdvertise, err)
	}
	_, port, err := net.SplitHostPort(config.Config.ListenAddress)
	if err != nil || port == "" {
		return uri, fmt.Errorf("computeLeaderURI: cannot determine listen port out of config.Config.ListenAddress: %+v", config.Config.ListenAddress)
	}
	return fmt.Sprintf("%s://%s", scheme, net.JoinHostPort(hostname, port)), nil
}

// Setup creates the raft runtime. New clusters are not auto-bootstrapped.
func Setup(applier CommandApplier, snapshotCreatorApplier SnapshotCreatorApplier, thisHostname string) error {
	log.Debugf("Setting up raft")
	ThisHostname = thisHostname
	created := NewStore(config.Config.RaftDataDir, config.Config.RaftBind, config.Config.RaftAdvertise, config.Config.RaftNodeID, applier, snapshotCreatorApplier)
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

	store = created
	leaderCh := store.raft.LeaderCh()
	go func() {
		for isTurnedLeader := range leaderCh {
			if isTurnedLeader {
				PublishCommand("leader-uri", thisLeaderURI)
			}
		}
	}()

	setupHttpClient()
	atomic.StoreInt64(&raftSetupComplete, 1)
	return nil
}

func Shutdown() error {
	atomic.StoreInt64(&raftSetupComplete, 0)
	if store == nil {
		return nil
	}
	err := store.Close()
	store = nil
	return err
}

func isRaftSetupComplete() bool {
	return atomic.LoadInt64(&raftSetupComplete) == 1
}

func getRaft() *raft.Raft {
	return store.raft
}

func normalizeRaftNode(node string) (string, error) {
	return config.NormalizeRaftAddress(node, config.Config.DefaultRaftPort)
}

// IsPartOfQuorum reports whether this node's data is trustworthy.
func IsPartOfQuorum() bool {
	return IsReady()
}

func IsLeader() bool {
	if !IsRaftEnabled() || !isRaftSetupComplete() {
		return false
	}
	return GetState() == raft.Leader
}

func IsLeaderReady() bool {
	if !IsRaftEnabled() || !isRaftSetupComplete() {
		return false
	}
	return store.leaderVerified()
}

func IsReady() bool {
	if !IsRaftEnabled() || !isRaftSetupComplete() {
		return false
	}
	return store.Status().Ready
}

func GetLeader() string {
	if !isRaftSetupComplete() || !IsRaftEnabled() {
		return ""
	}
	_, id := getRaft().LeaderWithID()
	return string(id)
}

func GetLeaderAddress() string {
	if !isRaftSetupComplete() || !IsRaftEnabled() {
		return ""
	}
	addr, _ := getRaft().LeaderWithID()
	return string(addr)
}

func QuorumSize() (int, error) {
	if !IsRaftEnabled() {
		return 0, RaftNotRunning
	}
	voters, err := store.voterCount()
	if err != nil {
		return 0, err
	}
	return voters/2 + 1, nil
}

func GetState() raft.RaftState {
	if !isRaftSetupComplete() || !IsRaftEnabled() {
		return raft.Shutdown
	}
	return getRaft().State()
}

func IsHealthy() bool {
	return IsReady()
}

func Snapshot() error {
	if !IsRaftEnabled() {
		return RaftNotRunning
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
	if store == nil {
		return ""
	}
	return store.raftBind
}

func GetRaftAdvertise() string {
	if store == nil {
		return ""
	}
	return store.raftAdvertise
}

func GetRaftNodeID() string {
	if store == nil {
		return ""
	}
	return store.nodeID
}

func GetClusterView() (ClusterView, error) {
	if !IsRaftEnabled() {
		return ClusterView{}, RaftNotRunning
	}
	return store.GetClusterView()
}

func GetStatus() NodeStatus {
	if !IsRaftEnabled() {
		return NodeStatus{}
	}
	return store.Status()
}

func Bootstrap() (ConfigurationView, error) {
	if !IsRaftEnabled() {
		return ConfigurationView{}, RaftNotRunning
	}
	return store.Bootstrap()
}

func AddMember(req MemberRequest) (ConfigurationView, error) {
	if !IsRaftEnabled() {
		return ConfigurationView{}, RaftNotRunning
	}
	if req.Address != "" {
		normalized, err := normalizeRaftNode(req.Address)
		if err != nil {
			return ConfigurationView{}, invalidArgument("member address is invalid: %v", err)
		}
		req.Address = normalized
	}
	return store.AddMember(req)
}

func RemoveMember(id string, expectedIndex *uint64) (ConfigurationView, error) {
	if !IsRaftEnabled() {
		return ConfigurationView{}, RaftNotRunning
	}
	return store.RemoveMember(id, expectedIndex)
}

func TransferLeadership(id, address string) error {
	if !IsRaftEnabled() {
		return RaftNotRunning
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

func PublishCommand(op string, value interface{}) (response interface{}, err error) {
	if !IsRaftEnabled() {
		return nil, RaftNotRunning
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
	if !IsRaftEnabled() {
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
func Monitor() error {
	tick := time.NewTicker(5 * time.Second)
	heartbeat := time.NewTicker(1 * time.Minute)
	defer tick.Stop()
	defer heartbeat.Stop()
	return monitor(tick.C, heartbeat.C, fatalRaftErrorChan)
}

func monitor(tick, heartbeat <-chan time.Time, fatalErrors <-chan error) error {
	for {
		select {
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
