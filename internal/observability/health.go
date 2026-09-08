package observability

import (
	"sync/atomic"
	"time"
)

// Health is a cached local health snapshot. It never represents a remote leader's health.
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

var health atomic.Pointer[Health]

// SetHealth publishes a complete snapshot without exposing partially updated fields.
func SetHealth(h Health) { health.Store(&h) }
func CurrentHealth() Health {
	h := health.Load()
	if h == nil {
		return Health{}
	}
	result := *h
	if time.Since(result.CheckedAt) > 15*time.Second {
		result.Ready = false
		result.LeaderReady = false
		result.Backend = false
		result.RaftReady = false
	}
	return result
}
func Bool(value bool) int64 {
	if value {
		return 1
	}
	return 0
}
func init() {
	Gauge("orchestrator_raft_last_index", "Local Raft last log index", func() int64 { return int64(CurrentHealth().LastIndex) })
	Gauge("orchestrator_raft_commit_index", "Local Raft committed index", func() int64 { return int64(CurrentHealth().CommitIndex) })
	Gauge("orchestrator_raft_applied_index", "Local Raft applied index", func() int64 { return int64(CurrentHealth().AppliedIndex) })

	Gauge("orchestrator_ready", "Local service readiness", func() int64 { return Bool(CurrentHealth().Ready) })
	Gauge("orchestrator_backend_ready", "Cached backend connectivity", func() int64 { return Bool(CurrentHealth().Backend) })
	Gauge("orchestrator_raft_ready", "Cached local Raft readiness", func() int64 { return Bool(CurrentHealth().RaftReady) })
	Gauge("orchestrator_leader_ready", "Local node can perform leader work", func() int64 { return Bool(CurrentHealth().LeaderReady) })
	Gauge("orchestrator_active", "Local discovery/recovery active role", func() int64 { return Bool(CurrentHealth().Active) })
}
