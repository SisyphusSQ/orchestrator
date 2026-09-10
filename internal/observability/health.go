package observability

import (
	"sync/atomic"
	"time"

	"github.com/openark/orchestrator/internal/models/vo"
)

var health atomic.Pointer[vo.Health]

// SetHealth publishes a complete snapshot without exposing partially updated fields.
func SetHealth(h vo.Health) { health.Store(&h) }
func CurrentHealth() vo.Health {
	h := health.Load()
	if h == nil {
		return vo.Health{}
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
