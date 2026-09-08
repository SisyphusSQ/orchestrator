package orcraft

import (
	"testing"

	"github.com/hashicorp/raft"
)

func TestConfigurationLogTracksAndRollsBackIndexes(t *testing.T) {
	logs := raft.NewInmemStore()
	snapshots := raft.NewInmemSnapshotStore()
	tracked, err := newConfigurationLog(logs, snapshots)
	if err != nil {
		t.Fatalf("new configuration log: %v", err)
	}

	first := raft.Configuration{Servers: []raft.Server{{ID: "node-1", Address: "node-1:1", Suffrage: raft.Voter}}}
	second := raft.Configuration{Servers: []raft.Server{
		{ID: "node-1", Address: "node-1:1", Suffrage: raft.Voter},
		{ID: "node-2", Address: "node-2:1", Suffrage: raft.Voter},
	}}
	if err := tracked.StoreLogs([]*raft.Log{
		{Index: 1, Term: 1, Type: raft.LogConfiguration, Data: raft.EncodeConfiguration(first)},
		{Index: 2, Term: 1, Type: raft.LogCommand, Data: []byte("command")},
		{Index: 3, Term: 1, Type: raft.LogConfiguration, Data: raft.EncodeConfiguration(second)},
	}); err != nil {
		t.Fatalf("store logs: %v", err)
	}
	configuration, index := tracked.latest()
	if index != 3 || !configurationsEqual(configuration, second) {
		t.Fatalf("latest configuration = index %d, %+v", index, configuration)
	}

	if err := tracked.DeleteRange(3, 3); err != nil {
		t.Fatalf("delete latest configuration: %v", err)
	}
	configuration, index = tracked.latest()
	if index != 1 || !configurationsEqual(configuration, first) {
		t.Fatalf("rolled-back configuration = index %d, %+v", index, configuration)
	}
}

func TestConfigurationLogRestoresIndexFromSnapshot(t *testing.T) {
	logs := raft.NewInmemStore()
	snapshots := raft.NewInmemSnapshotStore()
	tracked, err := newConfigurationLog(logs, snapshots)
	if err != nil {
		t.Fatalf("new configuration log: %v", err)
	}
	_, transport := raft.NewInmemTransport("")
	t.Cleanup(func() { _ = transport.Close() })

	configuration := raft.Configuration{Servers: []raft.Server{{ID: "node-1", Address: "node-1:1", Suffrage: raft.Voter}}}
	sink, err := snapshots.Create(raft.SnapshotVersionMax, 10, 2, configuration, 7, transport)
	if err != nil {
		t.Fatalf("create snapshot: %v", err)
	}
	if err := sink.Close(); err != nil {
		t.Fatalf("close snapshot: %v", err)
	}
	if err := tracked.refresh(); err != nil {
		t.Fatalf("refresh configuration log: %v", err)
	}
	got, index := tracked.latest()
	if index != 7 || !configurationsEqual(got, configuration) {
		t.Fatalf("snapshot configuration = index %d, %+v", index, got)
	}
}

func TestConfigurationLogRefreshesSameConfigurationAtNewSnapshotIndex(t *testing.T) {
	logs := raft.NewInmemStore()
	snapshots := raft.NewInmemSnapshotStore()
	configuration := raft.Configuration{Servers: []raft.Server{{ID: "node-1", Address: "node-1:1", Suffrage: raft.Voter}}}
	if err := logs.StoreLog(&raft.Log{
		Index: 3,
		Term:  1,
		Type:  raft.LogConfiguration,
		Data:  raft.EncodeConfiguration(configuration),
	}); err != nil {
		t.Fatalf("store old configuration: %v", err)
	}
	tracked, err := newConfigurationLog(logs, snapshots)
	if err != nil {
		t.Fatalf("new configuration log: %v", err)
	}

	_, transport := raft.NewInmemTransport("")
	t.Cleanup(func() { _ = transport.Close() })
	sink, err := snapshots.Create(raft.SnapshotVersionMax, 10, 2, configuration, 7, transport)
	if err != nil {
		t.Fatalf("create snapshot: %v", err)
	}
	if err := sink.Close(); err != nil {
		t.Fatalf("close snapshot: %v", err)
	}
	if err := tracked.refreshForSnapshotIndex(10); err != nil {
		t.Fatalf("refresh snapshot index: %v", err)
	}

	got, index := tracked.latest()
	if index != 7 || !configurationsEqual(got, configuration) {
		t.Fatalf("same snapshot configuration = index %d, %+v", index, got)
	}
}

func TestConfigurationLogIgnoresConfigurationLogsCoveredBySnapshot(t *testing.T) {
	logs := raft.NewInmemStore()
	snapshots := raft.NewInmemSnapshotStore()
	stale := raft.Configuration{Servers: []raft.Server{{ID: "stale", Address: "stale:1", Suffrage: raft.Voter}}}
	if err := logs.StoreLog(&raft.Log{
		Index: 9,
		Term:  1,
		Type:  raft.LogConfiguration,
		Data:  raft.EncodeConfiguration(stale),
	}); err != nil {
		t.Fatalf("store stale configuration: %v", err)
	}

	snapshotConfiguration := raft.Configuration{Servers: []raft.Server{{ID: "node-1", Address: "node-1:1", Suffrage: raft.Voter}}}
	_, transport := raft.NewInmemTransport("")
	t.Cleanup(func() { _ = transport.Close() })
	sink, err := snapshots.Create(raft.SnapshotVersionMax, 10, 2, snapshotConfiguration, 7, transport)
	if err != nil {
		t.Fatalf("create snapshot: %v", err)
	}
	if err := sink.Close(); err != nil {
		t.Fatalf("close snapshot: %v", err)
	}

	tracked, err := newConfigurationLog(logs, snapshots)
	if err != nil {
		t.Fatalf("new configuration log: %v", err)
	}
	got, index := tracked.latest()
	if index != 7 || !configurationsEqual(got, snapshotConfiguration) {
		t.Fatalf("configuration covered by snapshot won: index %d, %+v", index, got)
	}
	if err := tracked.StoreLog(&raft.Log{
		Index: 9,
		Term:  1,
		Type:  raft.LogConfiguration,
		Data:  raft.EncodeConfiguration(stale),
	}); err != nil {
		t.Fatalf("rewrite covered configuration: %v", err)
	}
	got, index = tracked.latest()
	if index != 7 || !configurationsEqual(got, snapshotConfiguration) {
		t.Fatalf("stored configuration covered by snapshot won: index %d, %+v", index, got)
	}

	newer := raft.Configuration{Servers: []raft.Server{
		{ID: "node-1", Address: "node-1:1", Suffrage: raft.Voter},
		{ID: "node-2", Address: "node-2:1", Suffrage: raft.Nonvoter},
	}}
	if err := tracked.StoreLog(&raft.Log{
		Index: 11,
		Term:  2,
		Type:  raft.LogConfiguration,
		Data:  raft.EncodeConfiguration(newer),
	}); err != nil {
		t.Fatalf("store post-snapshot configuration: %v", err)
	}
	got, index = tracked.latest()
	if index != 11 || !configurationsEqual(got, newer) {
		t.Fatalf("post-snapshot configuration = index %d, %+v", index, got)
	}
}
