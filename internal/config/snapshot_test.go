package config

import (
	"context"
	"sync"
	"testing"
)

func TestReloadPublishesIsolatedSnapshot(t *testing.T) {
	previous := Current()
	t.Cleanup(func() { TestUpdate(func(c *Configuration) { *c = *previous }) })
	ctx := WithSnapshot(context.Background())
	file := writeConfigFixture(t, `{"server":{"web":{"message":"new"}},"topology":{"classification":{"clusterNameToAlias":{"new":"alias"}}}}`)
	if _, err := ForceRead(file); err != nil {
		t.Fatal(err)
	}
	if Current() == previous {
		t.Fatal("published snapshot reused mutable storage")
	}
	if FromContext(ctx) != previous || previous.Server.Web.Message == "new" {
		t.Fatal("operation snapshot changed during reload")
	}
	if _, ok := previous.Topology.Classification.ClusterNameToAlias["new"]; ok {
		t.Fatal("nested map was shared across snapshots")
	}
}

func TestConcurrentReloadAndReaders(t *testing.T) {
	previous := Current()
	t.Cleanup(func() { TestUpdate(func(c *Configuration) { *c = *previous }) })
	a := writeConfigFixture(t, `{"server":{"web":{"message":"a","removeTextFromHostname":"a"}}}`)
	b := writeConfigFixture(t, `{"server":{"web":{"message":"b","removeTextFromHostname":"b"}}}`)
	if _, err := ForceRead(a); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for range 1000 {
				cfg := Current()
				if cfg.Server.Web.Message != cfg.Server.Web.RemoveTextFromHostname {
					t.Error("mixed configuration revisions")
					return
				}
			}
		})
	}
	for range 2 {
		wg.Go(func() {
			for range 20 {
				if _, err := ForceRead(a); err != nil {
					t.Error(err)
				}
				if _, err := ForceRead(b); err != nil {
					t.Error(err)
				}
			}
		})
	}
	wg.Wait()
}

func TestRunningConfigurationRejectsRestartFieldsAtomically(t *testing.T) {
	if _, err := ForceRead(writeConfigFixture(t, `{}`)); err != nil {
		t.Fatal(err)
	}
	previous := Current()
	oldLock := telemetryConfigurationLocked
	t.Cleanup(func() {
		configurationMu.Lock()
		telemetryConfigurationLocked = oldLock
		configurationMu.Unlock()
		TestUpdate(func(c *Configuration) { *c = *previous })
	})
	configurationMu.Lock()
	telemetryConfigurationLocked = true
	configurationMu.Unlock()
	file := writeConfigFixture(t, `{"metadata":{"mysql":{"host":"changed"}},"server":{"readOnly":true}}`)
	if _, err := ForceRead(file); err == nil {
		t.Fatal("restart-required setting was accepted")
	}
	if Current() != previous {
		t.Fatal("failed reload changed current snapshot")
	}
	file = writeConfigFixture(t, `{"server":{"readOnly":true}}`)
	if _, err := ForceRead(file); err != nil {
		t.Fatal(err)
	}
	if !Current().Server.ReadOnly || previous.Server.ReadOnly {
		t.Fatal("readOnly update did not preserve prior snapshot")
	}
}

func TestPrepareRaftDoesNotMutatePublishedSnapshot(t *testing.T) {
	previous := Current()
	t.Cleanup(func() { TestUpdate(func(c *Configuration) { *c = *previous }) })
	TestUpdate(func(c *Configuration) {
		c.Raft.NodeID = " node "
		c.Raft.DataDir = t.TempDir()
		c.Raft.Bind = "127.0.0.1"
		c.Raft.Advertise = ""
	})
	before := Current()
	if err := PrepareRaft(); err != nil {
		t.Fatal(err)
	}
	if before.Raft.NodeID != " node " || before.Raft.Advertise != "" {
		t.Fatal("normalization mutated prior snapshot")
	}
	if Current().Raft.NodeID != "node" || Current().Raft.Advertise == "" {
		t.Fatal("normalized runtime was not published")
	}
}
