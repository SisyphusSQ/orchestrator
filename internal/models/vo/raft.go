package vo

type RaftServer struct {
	ID       string `json:"id"`
	Address  string `json:"address"`
	Suffrage string `json:"suffrage"`
}

type RaftConfiguration struct {
	Index     uint64       `json:"index"`
	Committed bool         `json:"committed"`
	Servers   []RaftServer `json:"servers"`
}

type RaftCluster struct {
	RaftConfiguration
	LeaderID      string `json:"leaderId"`
	LeaderAddress string `json:"leaderAddress"`
	LocalID       string `json:"localId"`
	LocalAddress  string `json:"localAddress"`
	LocalState    string `json:"localState"`
}

type RaftNodeStatus struct {
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
