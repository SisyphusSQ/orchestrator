package vo

import "time"

// Health is the HTTP-facing local health snapshot.
type Health struct {
	LastIndex    uint64    `json:"lastIndex"`
	CommitIndex  uint64    `json:"commitIndex"`
	AppliedIndex uint64    `json:"appliedIndex"`
	CheckedAt    time.Time `json:"checkedAt"`
	Backend      bool      `json:"backend"`
	RaftReady    bool      `json:"raftReady"`
	Leader       bool      `json:"leader"`
	LeaderReady  bool      `json:"leaderReady"`
	Active       bool      `json:"active"`
	Ready        bool      `json:"ready"`
}
