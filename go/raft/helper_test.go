package orcraft

import (
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/raft"
)

type memoryApp struct {
	mu   sync.Mutex
	ops  []string
	data []byte
}

func (m *memoryApp) ApplyCommand(op string, value []byte) interface{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ops = append(m.ops, op)
	if op == "set" {
		m.data = append([]byte(nil), value...)
	}
	return nil
}

func (m *memoryApp) GetData() ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]byte(nil), m.data...), nil
}

func (m *memoryApp) Restore(rc io.ReadCloser) error {
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data = data
	return nil
}

func (m *memoryApp) snapshot() []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]byte(nil), m.data...)
}

func (m *memoryApp) commands() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.ops))
	copy(out, m.ops)
	return out
}

func waitFor(t *testing.T, timeout time.Duration, message string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", message)
}

func localAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}
	return addr
}

func testStoreConfig(store *Store) {
	store.heartbeatTimeout = 200 * time.Millisecond
	store.electionTimeout = 200 * time.Millisecond
	store.leaderLeaseTimeout = 100 * time.Millisecond
	store.contactThreshold = time.Second
	store.logger = hclog.NewNullLogger()
}

func startTCPNode(t *testing.T, id string, app *memoryApp) *Store {
	t.Helper()
	addr := localAddr(t)
	store := NewStore(t.TempDir(), addr, addr, id, app, app)
	testStoreConfig(store)
	if err := store.Open(); err != nil {
		t.Fatalf("open %s: %v", id, err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	return store
}

func leaderOf(nodes []*Store) *Store {
	for _, node := range nodes {
		if node.raft != nil && node.raft.State() == raft.Leader {
			return node
		}
	}
	return nil
}

func waitForLeader(t *testing.T, nodes ...*Store) *Store {
	t.Helper()
	var leader *Store
	waitFor(t, 10*time.Second, "leader", func() bool {
		leader = leaderOf(nodes)
		return leader != nil && leader.leaderVerified()
	})
	return leader
}

func waitForConfiguration(t *testing.T, node *Store, servers int) ConfigurationView {
	t.Helper()
	var view ConfigurationView
	waitFor(t, 10*time.Second, "configuration", func() bool {
		cfg, err := node.currentConfigurationView()
		if err != nil {
			return false
		}
		view = cfg
		return len(cfg.Servers) == servers
	})
	return view
}

func connectInmem(transports ...*raft.InmemTransport) {
	for i, left := range transports {
		for j, right := range transports {
			if i == j {
				continue
			}
			left.Connect(right.LocalAddr(), right)
		}
	}
}

func startInmemNode(t *testing.T, id string, app *memoryApp, addr raft.ServerAddress, trans *raft.InmemTransport) *Store {
	t.Helper()
	store := NewStore(t.TempDir(), string(addr), string(addr), id, app, app)
	testStoreConfig(store)
	store.transport = trans
	store.logStore = raft.NewInmemStore()
	store.stableStore = raft.NewInmemStore()
	store.snapshots = raft.NewInmemSnapshotStore()
	if err := store.Open(); err != nil {
		t.Fatalf("open inmem %s: %v", id, err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	return store
}
