package orcraft

import (
	"net"
	"testing"
	"time"

	"github.com/hashicorp/raft"
)

func TestThreeNodeLifecycle(t *testing.T) {
	app1 := &memoryApp{}
	app2 := &memoryApp{}
	app3 := &memoryApp{}
	n1 := startTCPNode(t, "node-1", app1)
	n2 := startTCPNode(t, "node-2", app2)
	n3 := startTCPNode(t, "node-3", app3)

	bootstrapView, err := n1.Bootstrap()
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	leader := waitForLeader(t, n1)
	if leader.nodeID != "node-1" {
		t.Fatalf("leader after bootstrap = %s, want node-1", leader.nodeID)
	}

	node2View, err := n1.AddMember(MemberRequest{ID: n2.nodeID, Address: n2.raftAdvertise, Suffrage: suffrageVoter})
	if err != nil {
		t.Fatalf("add node-2: %v", err)
	}
	if node2View.Index <= bootstrapView.Index {
		t.Fatalf("node-2 configuration index = %d, want greater than bootstrap index %d", node2View.Index, bootstrapView.Index)
	}
	node3View, err := n1.AddMember(MemberRequest{ID: n3.nodeID, Address: n3.raftAdvertise, Suffrage: suffrageVoter})
	if err != nil {
		t.Fatalf("add node-3: %v", err)
	}
	if node3View.Index <= node2View.Index {
		t.Fatalf("node-3 configuration index = %d, want greater than node-2 index %d", node3View.Index, node2View.Index)
	}

	for _, node := range []*Store{n1, n2, n3} {
		view := waitForConfiguration(t, node, 3)
		voters := 0
		for _, server := range view.Servers {
			if server.Suffrage == suffrageVoter {
				voters++
			}
		}
		if voters != 3 {
			t.Fatalf("%s configuration voters = %d, want 3: %+v", node.nodeID, voters, view)
		}
	}

	leader = waitForLeader(t, n1, n2, n3)
	followerForMutation := n1
	if followerForMutation == leader {
		followerForMutation = n2
	}
	if _, err := followerForMutation.AddMember(MemberRequest{ID: n3.nodeID, Address: n3.raftAdvertise, Suffrage: suffrageVoter}); ClassOf(err) != ClassNotLeader {
		t.Fatalf("follower duplicate add class = %s, want not_leader: %v", ClassOf(err), err)
	}
	if _, err := followerForMutation.RemoveMember(n3.nodeID, nil); ClassOf(err) != ClassNotLeader {
		t.Fatalf("follower remove class = %s, want not_leader: %v", ClassOf(err), err)
	}
	if err := followerForMutation.TransferLeadership("missing", ""); ClassOf(err) != ClassNotLeader {
		t.Fatalf("follower directed transfer class = %s, want not_leader: %v", ClassOf(err), err)
	}

	if _, err := leader.AddMember(MemberRequest{ID: n2.nodeID, Address: n2.raftAdvertise, Suffrage: suffrageVoter}); err != nil {
		t.Fatalf("duplicate add node-2: %v", err)
	}
	if _, err := leader.AddMember(MemberRequest{ID: n2.nodeID, Address: n3.raftAdvertise, Suffrage: suffrageVoter}); err == nil {
		t.Fatal("same id different address succeeded")
	} else if ClassOf(err) != ClassConflict {
		t.Fatalf("same id different address class = %s, want conflict: %v", ClassOf(err), err)
	}
	if _, err := leader.AddMember(MemberRequest{ID: "node-x", Address: n2.raftAdvertise, Suffrage: suffrageVoter}); err == nil {
		t.Fatal("same address different id succeeded")
	} else if ClassOf(err) != ClassConflict {
		t.Fatalf("same address different id class = %s, want conflict: %v", ClassOf(err), err)
	}

	staleIndex := bootstrapView.Index
	if _, err := leader.AddMember(MemberRequest{ID: "late", Address: "127.0.0.1:1", Suffrage: suffrageVoter, ExpectedIndex: &staleIndex}); err == nil {
		t.Fatal("stale index succeeded")
	} else if ClassOf(err) != ClassConflict {
		t.Fatalf("stale index class = %s: %v", ClassOf(err), err)
	}

	follower := n2
	if follower.nodeID == leader.nodeID {
		follower = n3
	}
	removedView, err := leader.RemoveMember(follower.nodeID, nil)
	if err != nil {
		t.Fatalf("remove %s: %v", follower.nodeID, err)
	}
	if _, stillPresent := findServerView(removedView, follower.nodeID); stillPresent {
		t.Fatalf("removed member still in leader configuration: %+v", removedView)
	}
	waitFor(t, 10*time.Second, "removed node not ready", func() bool {
		status := follower.Status()
		return !status.InConfiguration && !status.Ready
	})
	waitForConfiguration(t, leader, 2)

	remaining := []*Store{n1, n3}
	if follower == n3 {
		remaining = []*Store{n1, n2}
	}
	leader = waitForLeader(t, remaining...)
	other := remaining[0]
	if other.nodeID == leader.nodeID {
		other = remaining[1]
	}
	if err := leader.TransferLeadership("", other.raftAdvertise); ClassOf(err) != ClassInvalidArgument {
		t.Fatalf("address-only transfer class = %s, want invalid_argument: %v", ClassOf(err), err)
	}
	if err := leader.TransferLeadership(leader.nodeID, leader.raftAdvertise); ClassOf(err) != ClassInvalidArgument {
		t.Fatalf("self transfer class = %s, want invalid_argument: %v", ClassOf(err), err)
	}
	if err := leader.TransferLeadership(other.nodeID, other.raftAdvertise); err != nil {
		t.Fatalf("directed transfer: %v", err)
	}
	waitFor(t, 10*time.Second, "directed leader", func() bool {
		return other.raft.State() == raft.Leader && other.leaderVerified()
	})
	if err := other.TransferLeadership("", ""); err != nil {
		t.Fatalf("undirected transfer: %v", err)
	}
	waitFor(t, 10*time.Second, "undirected leader change", func() bool {
		current := leaderOf(remaining)
		return current != nil && current.nodeID != other.nodeID && current.leaderVerified()
	})

	currentLeader := waitForLeader(t, remaining...)
	if _, err := currentLeader.genericCommand("set", []byte("cluster-data")); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if err := currentLeader.Snapshot(); err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	restarted := remaining[0]
	if restarted.nodeID == currentLeader.nodeID {
		restarted = remaining[1]
	}
	dir, id, addr := restarted.raftDir, restarted.nodeID, restarted.raftAdvertise
	if err := restarted.Close(); err != nil {
		t.Fatalf("close for restart: %v", err)
	}
	reopenedApp := &memoryApp{}
	reopened := NewStore(dir, addr, addr, id, reopenedApp, reopenedApp)
	testStoreConfig(reopened)
	if err := reopened.Open(); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	waitForConfiguration(t, reopened, 2)
	waitFor(t, 10*time.Second, "restarted node membership", func() bool {
		status := reopened.Status()
		return status.NodeID == id && status.InConfiguration && status.IsVoter
	})

	leaderAfterRestart := waitForLeader(t, currentLeader, reopened)
	removedLeaderAddress := leaderAfterRestart.raftBind
	if _, err := leaderAfterRestart.RemoveMember(leaderAfterRestart.nodeID, nil); err != nil {
		t.Fatalf("remove leader: %v", err)
	}
	waitFor(t, 10*time.Second, "removed leader shutdown", func() bool {
		return leaderAfterRestart.raft.State() == raft.Shutdown && !leaderAfterRestart.Status().Ready
	})
	if status := leaderAfterRestart.Status(); status.Running {
		t.Fatalf("removed leader reports running after shutdown: %+v", status)
	}
	if err := leaderAfterRestart.Close(); err != nil {
		t.Fatalf("close removed leader: %v", err)
	}
	listener, err := net.Listen("tcp", removedLeaderAddress)
	if err != nil {
		t.Fatalf("removed leader transport still owns %s: %v", removedLeaderAddress, err)
	}
	if err := listener.Close(); err != nil {
		t.Fatalf("close rebound listener: %v", err)
	}
}

func TestBootstrapConflict(t *testing.T) {
	n1 := startTCPNode(t, "node-1", &memoryApp{})
	view, err := n1.Bootstrap()
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if !view.Committed || view.Index == 0 {
		t.Fatalf("bootstrap returned an uncommitted or unversioned configuration: %+v", view)
	}
	waitForLeader(t, n1)
	if _, err := n1.Bootstrap(); err == nil {
		t.Fatal("second bootstrap succeeded")
	} else if ClassOf(err) != ClassConflict {
		t.Fatalf("second bootstrap class = %s: %v", ClassOf(err), err)
	}
}

func TestBootstrapWaitsForCommittedConfiguration(t *testing.T) {
	addr := localAddr(t)
	store := NewStore(t.TempDir(), addr, addr, "node-1", &memoryApp{}, &memoryApp{})
	testStoreConfig(store)
	store.heartbeatTimeout = 500 * time.Millisecond
	store.electionTimeout = 500 * time.Millisecond
	store.leaderLeaseTimeout = 250 * time.Millisecond
	if err := store.Open(); err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	// Let the unbootstrapped follower enter its normal wait loop before the
	// bootstrap request so BootstrapCluster acknowledgement precedes election.
	time.Sleep(20 * time.Millisecond)
	view, err := store.Bootstrap()
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if !view.Committed || view.Index == 0 {
		t.Fatalf("bootstrap returned an uncommitted or unversioned configuration: %+v", view)
	}
}

func TestLeadershipTransferRejectsNonvoter(t *testing.T) {
	leader := startTCPNode(t, "node-1", &memoryApp{})
	if _, err := leader.Bootstrap(); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	waitForLeader(t, leader)

	nonvoterAddress := localAddr(t)
	if _, err := leader.AddMember(MemberRequest{
		ID:       "node-2",
		Address:  nonvoterAddress,
		Suffrage: suffrageNonvoter,
	}); err != nil {
		t.Fatalf("add nonvoter: %v", err)
	}
	waitForConfiguration(t, leader, 2)

	err := leader.TransferLeadership("node-2", nonvoterAddress)
	if ClassOf(err) != ClassInvalidArgument {
		t.Fatalf("nonvoter transfer class = %s, want invalid_argument: %v", ClassOf(err), err)
	}
}

func TestLeadershipTransferRequiresAnotherVoter(t *testing.T) {
	leader := startTCPNode(t, "node-1", &memoryApp{})
	if _, err := leader.Bootstrap(); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	waitForLeader(t, leader)

	err := leader.TransferLeadership("", "")
	if ClassOf(err) != ClassConflict {
		t.Fatalf("single-voter transfer class = %s, want conflict: %v", ClassOf(err), err)
	}
}
