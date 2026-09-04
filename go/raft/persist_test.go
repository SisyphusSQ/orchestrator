package orcraft

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBoltSnapshotRestartRestoresIDAndData(t *testing.T) {
	dir := t.TempDir()
	addr := localAddr(t)
	app := &memoryApp{}
	store := NewStore(dir, addr, addr, "node-1", app, app)
	testStoreConfig(store)
	if err := store.Open(); err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := store.Bootstrap(); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	waitForLeader(t, store)
	if _, err := store.genericCommand("set", []byte("persisted-value")); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if err := store.Snapshot(); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	restored := &memoryApp{}
	reopened := NewStore(dir, addr, addr, "node-1", restored, restored)
	testStoreConfig(reopened)
	if err := reopened.Open(); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	waitForLeader(t, reopened)
	waitFor(t, 5*time.Second, "restored snapshot data", func() bool {
		return bytes.Equal(restored.snapshot(), []byte("persisted-value"))
	})
	status := reopened.Status()
	if status.NodeID != "node-1" {
		t.Fatalf("restored node id = %s", status.NodeID)
	}
	if !status.InConfiguration || !status.IsVoter {
		t.Fatalf("restored membership: %+v", status)
	}
	view, err := reopened.GetClusterView()
	if err != nil {
		t.Fatalf("configuration: %v", err)
	}
	if len(view.Servers) != 1 || view.Servers[0].ID != "node-1" {
		t.Fatalf("restored configuration: %+v", view)
	}
	if view.Index == 0 || !view.Committed {
		t.Fatalf("restored configuration lost its committed index: %+v", view)
	}

	mismatch := NewStore(dir, addr, addr, "other-id", &memoryApp{}, &memoryApp{})
	testStoreConfig(mismatch)
	if err := mismatch.Open(); err == nil {
		_ = mismatch.Close()
		t.Fatal("opening with mismatched node id succeeded")
	}
}

func TestOpenFailureReleasesBoltAndTransport(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	dir := t.TempDir()
	store := NewStore(dir, addr, addr, "node-1", &memoryApp{}, &memoryApp{})
	testStoreConfig(store)
	if err := store.Open(); err == nil {
		_ = store.Close()
		_ = ln.Close()
		t.Fatal("open succeeded while bind address was already taken")
	}
	_ = ln.Close()
	if store.raft != nil {
		t.Fatal("failed open left raft running")
	}
	if store.boltStore != nil {
		t.Fatal("failed open left bolt store open")
	}
	if store.transport != nil {
		t.Fatal("failed open left transport open")
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("remove data dir while resources should be closed: %v", err)
	}
}

func TestOpenRejectsExistingStateWithoutNodeID(t *testing.T) {
	dir := t.TempDir()
	addr := localAddr(t)
	app := &memoryApp{}
	store := NewStore(dir, addr, addr, "node-1", app, app)
	testStoreConfig(store)
	if err := store.Open(); err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := store.Bootstrap(); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	waitForLeader(t, store)
	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	nodeIDPath := filepath.Join(dir, nodeIDFileName)
	if err := os.Remove(nodeIDPath); err != nil {
		t.Fatalf("remove node id: %v", err)
	}
	reopened := NewStore(dir, addr, addr, "node-1", &memoryApp{}, &memoryApp{})
	testStoreConfig(reopened)
	if err := reopened.Open(); err == nil {
		_ = reopened.Close()
		t.Fatal("opening existing raft state without its persisted node id succeeded")
	}
	if _, err := os.Stat(nodeIDPath); !os.IsNotExist(err) {
		t.Fatalf("failed open recreated node id: %v", err)
	}

	if err := os.WriteFile(nodeIDPath, []byte("node-1\n"), 0644); err != nil {
		t.Fatalf("restore node id: %v", err)
	}
	recovered := NewStore(dir, addr, addr, "node-1", &memoryApp{}, &memoryApp{})
	testStoreConfig(recovered)
	if err := recovered.Open(); err != nil {
		t.Fatalf("open after restoring node id (failed open leaked resources): %v", err)
	}
	if err := recovered.Close(); err != nil {
		t.Fatalf("close recovered store: %v", err)
	}
}
