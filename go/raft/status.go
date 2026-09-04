package orcraft

import (
	"time"

	"github.com/hashicorp/raft"
	"github.com/openark/orchestrator/go/config"
)

// NodeStatus is the ID-aware raft health snapshot for this process.
type NodeStatus struct {
	Running                bool   `json:"running"`
	NodeID                 string `json:"nodeId"`
	Address                string `json:"address"`
	Bind                   string `json:"bind"`
	State                  string `json:"state"`
	InConfiguration        bool   `json:"inConfiguration"`
	IsVoter                bool   `json:"isVoter"`
	LeaderID               string `json:"leaderId"`
	LeaderAddress          string `json:"leaderAddress"`
	LeaderKnown            bool   `json:"leaderKnown"`
	Ready                  bool   `json:"ready"`
	IsLeader               bool   `json:"isLeader"`
	LeaderVerified         bool   `json:"leaderVerified"`
	ConfigurationIndex     uint64 `json:"configurationIndex"`
	ConfigurationCommitted bool   `json:"configurationCommitted"`
}

func (store *Store) Status() NodeStatus {
	status := NodeStatus{}
	if store == nil || store.raft == nil {
		return status
	}
	status.NodeID = store.nodeID
	status.Address = store.raftAdvertise
	status.Bind = store.raftBind
	state := store.raft.State()
	status.State = state.String()
	status.Running = state != raft.Shutdown
	status.IsLeader = state == raft.Leader

	leaderAddr, leaderID := store.raft.LeaderWithID()
	status.LeaderID = string(leaderID)
	status.LeaderAddress = string(leaderAddr)
	status.LeaderKnown = status.LeaderID != "" || status.LeaderAddress != ""

	cfg, index, err := store.configuration()
	if err == nil {
		status.ConfigurationIndex = index
		status.ConfigurationCommitted = configurationCommitted(len(cfg.Servers) > 0, index, store.raft.CommitIndex())
		if status.ConfigurationCommitted {
			if server, ok := findServerByID(cfg, raft.ServerID(store.nodeID)); ok {
				status.InConfiguration = true
				status.IsVoter = server.Suffrage == raft.Voter
			}
		}
	}

	status.LeaderVerified = store.leaderVerified()
	status.Ready = store.ready(status)
	return status
}

func (store *Store) ready(status NodeStatus) bool {
	if store == nil || store.raft == nil {
		return false
	}
	if store.raft.State() == raft.Shutdown {
		return false
	}
	if !status.InConfiguration || !status.IsVoter {
		return false
	}
	if store.raft.State() == raft.Leader {
		return status.LeaderVerified
	}
	if store.raft.State() != raft.Follower {
		return false
	}
	if !status.LeaderKnown {
		return false
	}
	lastContact := store.raft.LastContact()
	if lastContact.IsZero() {
		return false
	}
	return time.Since(lastContact) <= store.lastContactThreshold()
}

func (store *Store) leaderVerified() bool {
	if store == nil || store.raft == nil {
		return false
	}
	if store.raft.State() != raft.Leader {
		return false
	}
	return store.raft.VerifyLeader().Error() == nil
}

func (store *Store) lastContactThreshold() time.Duration {
	if store.contactThreshold > 0 {
		return store.contactThreshold
	}
	return FollowerLastContactThreshold(store.electionTimeout)
}

// FollowerLastContactThreshold is max(RaftHealthPollSeconds, 3 * electionTimeout).
func FollowerLastContactThreshold(electionTimeout time.Duration) time.Duration {
	if electionTimeout <= 0 {
		electionTimeout = time.Second
	}
	poll := time.Duration(config.RaftHealthPollSeconds) * time.Second
	raftWindow := 3 * electionTimeout
	if poll > raftWindow {
		return poll
	}
	return raftWindow
}

func (store *Store) voterCount() (int, error) {
	cfg, index, err := store.configuration()
	if err != nil {
		return 0, err
	}
	if len(cfg.Servers) == 0 {
		return 0, ErrNotBootstrapped
	}
	if !configurationCommitted(len(cfg.Servers) > 0, index, store.raft.CommitIndex()) {
		return 0, ErrConfigurationInProgress
	}
	count := 0
	for _, server := range cfg.Servers {
		if server.Suffrage == raft.Voter {
			count++
		}
	}
	return count, nil
}
