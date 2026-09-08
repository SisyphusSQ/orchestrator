package orcraft

// LogProgress returns local indices without querying another node or changing Raft state.
func LogProgress() (last, committed, applied uint64) {
	if !isRaftSetupComplete() {
		return 0, 0, 0
	}
	r := getRaft()
	return r.LastIndex(), r.CommitIndex(), r.AppliedIndex()
}

// ObservabilityStatus waits for Setup's atomic publication before reading store.
// The health monitor is stopped before process resource shutdown.
func ObservabilityStatus() NodeStatus {
	if !isRaftSetupComplete() {
		return NodeStatus{}
	}
	return GetStatus()
}
