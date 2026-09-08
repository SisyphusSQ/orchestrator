package orcraft

import "testing"

func TestObservabilityBeforeSetup(t *testing.T) {
	if isRaftSetupComplete() {
		t.Fatal("fixture expects Raft not to be initialized")
	}
	if s := ObservabilityStatus(); s.Ready || s.LeaderVerified || s.IsLeader {
		t.Fatalf("uninitialized Raft reported healthy: %+v", s)
	}
	if last, committed, applied := LogProgress(); last != 0 || committed != 0 || applied != 0 {
		t.Fatalf("uninitialized indices: %d/%d/%d", last, committed, applied)
	}
}
