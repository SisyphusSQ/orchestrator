package orcraft

import "github.com/openark/orchestrator/internal/models/vo"

// LogProgress returns local indices without querying another node or changing Raft state.
func LogProgress() (last, committed, applied uint64) {
	store, release := acquireStore()
	defer release()
	if store == nil {
		return 0, 0, 0
	}
	r := store.raft
	return r.LastIndex(), r.CommitIndex(), r.AppliedIndex()
}

// ObservabilityStatus waits for Setup's atomic publication before reading store.
// The health monitor is stopped before process resource shutdown.
func ObservabilityStatus() vo.RaftNodeStatus { return GetStatus() }
