package orcraft

import (
	"errors"
	"sync"

	"github.com/hashicorp/raft"
)

// configurationLog tracks the real index paired with the latest official
// LogConfiguration. Hashicorp Raft v1.7.3 exposes the latest configuration via
// GetConfiguration but does not populate ConfigurationFuture.Index, so the
// adapter must recover that index from the same persisted log/snapshot facts.
type configurationLog struct {
	raft.LogStore
	snapshots raft.SnapshotStore

	mu            sync.RWMutex
	configuration raft.Configuration
	index         uint64
	snapshotIndex uint64
}

func newConfigurationLog(logs raft.LogStore, snapshots raft.SnapshotStore) (*configurationLog, error) {
	tracked := &configurationLog{LogStore: logs, snapshots: snapshots}
	tracked.mu.Lock()
	defer tracked.mu.Unlock()
	if err := tracked.reloadLocked(); err != nil {
		return nil, err
	}
	return tracked, nil
}

func (tracked *configurationLog) StoreLog(entry *raft.Log) error {
	return tracked.StoreLogs([]*raft.Log{entry})
}

func (tracked *configurationLog) StoreLogs(entries []*raft.Log) error {
	tracked.mu.Lock()
	defer tracked.mu.Unlock()
	if err := tracked.LogStore.StoreLogs(entries); err != nil {
		return err
	}
	for _, entry := range entries {
		if entry != nil && entry.Type == raft.LogConfiguration &&
			entry.Index > tracked.snapshotIndex && entry.Index >= tracked.index {
			configuration := raft.DecodeConfiguration(entry.Data)
			tracked.configuration = configuration.Clone()
			tracked.index = entry.Index
		}
	}
	return nil
}

func (tracked *configurationLog) DeleteRange(min, max uint64) error {
	tracked.mu.Lock()
	defer tracked.mu.Unlock()
	if err := tracked.LogStore.DeleteRange(min, max); err != nil {
		return err
	}
	return tracked.reloadLocked()
}

func (tracked *configurationLog) latest() (raft.Configuration, uint64) {
	tracked.mu.RLock()
	defer tracked.mu.RUnlock()
	return tracked.configuration.Clone(), tracked.index
}

func (tracked *configurationLog) refresh() error {
	tracked.mu.Lock()
	defer tracked.mu.Unlock()
	return tracked.reloadLocked()
}

func (tracked *configurationLog) refreshForSnapshotIndex(snapshotIndex uint64) error {
	tracked.mu.RLock()
	current := tracked.snapshotIndex
	tracked.mu.RUnlock()
	if current == snapshotIndex {
		return nil
	}
	return tracked.refresh()
}

func (tracked *configurationLog) reloadLocked() error {
	configuration := raft.Configuration{}
	configurationIndex := uint64(0)
	snapshotIndex := uint64(0)

	if tracked.snapshots != nil {
		metas, err := tracked.snapshots.List()
		if err != nil {
			return err
		}
		for _, meta := range metas {
			if meta != nil && (meta.Index > snapshotIndex ||
				(meta.Index == snapshotIndex && meta.ConfigurationIndex > configurationIndex)) {
				configuration = meta.Configuration.Clone()
				configurationIndex = meta.ConfigurationIndex
				snapshotIndex = meta.Index
			}
		}
	}

	first, err := tracked.LogStore.FirstIndex()
	if err != nil {
		return err
	}
	last, err := tracked.LogStore.LastIndex()
	if err != nil {
		return err
	}
	firstConfigurationCandidate := first
	if snapshotIndex >= firstConfigurationCandidate {
		firstConfigurationCandidate = snapshotIndex + 1
	}
	if firstConfigurationCandidate > 0 && last >= firstConfigurationCandidate {
		for index := last; ; index-- {
			var entry raft.Log
			err := tracked.LogStore.GetLog(index, &entry)
			found := false
			switch {
			case err == nil && entry.Type == raft.LogConfiguration && entry.Index > configurationIndex:
				configuration = raft.DecodeConfiguration(entry.Data)
				configurationIndex = entry.Index
				found = true
			case err == nil:
			case errors.Is(err, raft.ErrLogNotFound):
			default:
				return err
			}
			if found || index == firstConfigurationCandidate {
				break
			}
		}
	}

	tracked.configuration = configuration
	tracked.index = configurationIndex
	tracked.snapshotIndex = snapshotIndex
	return nil
}

func (tracked *configurationLog) IsMonotonic() bool {
	if logs, ok := tracked.LogStore.(raft.MonotonicLogStore); ok {
		return logs.IsMonotonic()
	}
	return false
}
