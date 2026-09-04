package orcraft

import (
	"testing"
	"time"

	"github.com/hashicorp/raft"
)

func TestPartitionedMinorityDoesNotStayReady(t *testing.T) {
	app1, app2, app3 := &memoryApp{}, &memoryApp{}, &memoryApp{}
	addr1, trans1 := raft.NewInmemTransport("")
	addr2, trans2 := raft.NewInmemTransport("")
	addr3, trans3 := raft.NewInmemTransport("")
	connectInmem(trans1, trans2, trans3)

	n1 := startInmemNode(t, "node-1", app1, addr1, trans1)
	n2 := startInmemNode(t, "node-2", app2, addr2, trans2)
	n3 := startInmemNode(t, "node-3", app3, addr3, trans3)
	n1.contactThreshold = 800 * time.Millisecond
	n2.contactThreshold = 800 * time.Millisecond
	n3.contactThreshold = 800 * time.Millisecond

	if _, err := n1.Bootstrap(); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	waitForLeader(t, n1)
	if _, err := n1.AddMember(MemberRequest{ID: "node-2", Address: string(addr2), Suffrage: suffrageVoter}); err != nil {
		t.Fatalf("add node-2: %v", err)
	}
	if _, err := n1.AddMember(MemberRequest{ID: "node-3", Address: string(addr3), Suffrage: suffrageVoter}); err != nil {
		t.Fatalf("add node-3: %v", err)
	}
	waitForConfiguration(t, n2, 3)
	waitForConfiguration(t, n3, 3)
	leader := waitForLeader(t, n1, n2, n3)

	var isolated *raft.InmemTransport
	switch leader.nodeID {
	case "node-1":
		isolated = trans1
	case "node-2":
		isolated = trans2
	default:
		isolated = trans3
	}
	isolated.DisconnectAll()
	if leader.nodeID != "node-1" {
		trans1.Disconnect(isolated.LocalAddr())
	}
	if leader.nodeID != "node-2" {
		trans2.Disconnect(isolated.LocalAddr())
	}
	if leader.nodeID != "node-3" {
		trans3.Disconnect(isolated.LocalAddr())
	}

	majority := make([]*Store, 0, 2)
	for _, node := range []*Store{n1, n2, n3} {
		if node != leader {
			majority = append(majority, node)
		}
	}
	newLeader := waitForLeader(t, majority...)
	waitFor(t, 5*time.Second, "isolated leader not ready", func() bool {
		return !leader.Status().Ready
	})
	if !newLeader.Status().Ready {
		t.Fatalf("majority leader %s is not ready", newLeader.nodeID)
	}
	readyCount := 0
	for _, node := range []*Store{n1, n2, n3} {
		if node.Status().Ready {
			readyCount++
		}
	}
	if readyCount > 2 {
		t.Fatalf("ready nodes = %d, both partitions cannot stay ready", readyCount)
	}

	connectInmem(trans1, trans2, trans3)
	waitFor(t, 10*time.Second, "cluster reconverge", func() bool {
		current := leaderOf([]*Store{n1, n2, n3})
		if current == nil || !current.leaderVerified() {
			return false
		}
		ready := 0
		for _, node := range []*Store{n1, n2, n3} {
			if node.Status().Ready {
				ready++
			}
		}
		return ready == 3
	})
}
