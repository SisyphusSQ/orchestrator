package orcraft

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/raft"
	raftboltdb "github.com/hashicorp/raft-boltdb/v2"
	"github.com/openark/golib/log"
)

const (
	boltFileName        = "raft.db"
	nodeIDFileName      = "node-id"
	retainSnapshotCount = 10
	snapshotInterval    = 30 * time.Minute
	raftTimeout         = 10 * time.Second
)

// Store owns a single official raft node, its Bolt store, snapshot store,
// and transport. Raft configuration is the only membership source of truth.
type Store struct {
	raftDir       string
	raftBind      string
	raftAdvertise string
	nodeID        string

	raft        *raft.Raft
	boltStore   *raftboltdb.BoltStore
	transport   raft.Transport
	logStore    raft.LogStore
	configLog   *configurationLog
	stableStore raft.StableStore
	snapshots   raft.SnapshotStore

	applier                CommandApplier
	snapshotCreatorApplier SnapshotCreatorApplier

	heartbeatTimeout   time.Duration
	electionTimeout    time.Duration
	leaderLeaseTimeout time.Duration
	contactThreshold   time.Duration
	logger             hclog.Logger
	membershipMu       sync.Mutex
}

type storeCommand struct {
	Op    string `json:"op,omitempty"`
	Value []byte `json:"value,omitempty"`
}

// NewStore inits and returns a new store.
func NewStore(raftDir string, raftBind string, raftAdvertise string, nodeID string, applier CommandApplier, snapshotCreatorApplier SnapshotCreatorApplier) *Store {
	return &Store{
		raftDir:                raftDir,
		raftBind:               raftBind,
		raftAdvertise:          raftAdvertise,
		nodeID:                 nodeID,
		applier:                applier,
		snapshotCreatorApplier: snapshotCreatorApplier,
	}
}

// Open starts raft. It does not bootstrap a cluster.
func (store *Store) Open() error {
	if store.nodeID == "" {
		return invalidArgument("raft node id is required")
	}
	if store.raftBind == "" {
		return invalidArgument("raft bind address is required")
	}
	if store.raftAdvertise == "" {
		return invalidArgument("raft advertise address is required")
	}
	if store.raftDir == "" {
		return invalidArgument("raft data dir is required")
	}

	if err := os.MkdirAll(store.raftDir, 0755); err != nil {
		return fmt.Errorf("RaftDataDir (%s) does not exist and cannot be created: %w", store.raftDir, err)
	}
	nodeIDPersisted, err := store.validateExistingNodeID()
	if err != nil {
		return err
	}
	if store.logStore == nil || store.stableStore == nil {
		bolt, err := raftboltdb.NewBoltStore(filepath.Join(store.raftDir, boltFileName))
		if err != nil {
			return wrapError(ClassFailed, "open raft bolt store", err)
		}
		store.boltStore = bolt
		store.logStore = bolt
		store.stableStore = bolt
	}
	if store.snapshots == nil {
		snapshots, err := raft.NewFileSnapshotStore(store.raftDir, retainSnapshotCount, os.Stderr)
		if err != nil {
			store.closePartial()
			return wrapError(ClassFailed, "open raft snapshot store", err)
		}
		store.snapshots = snapshots
	}
	if !nodeIDPersisted {
		existing, err := store.hasExistingState()
		if err != nil {
			store.closePartial()
			return wrapError(ClassFailed, "inspect raft state before persisting node id", err)
		}
		if existing {
			store.closePartial()
			return newError(ClassFailed, "raft data dir has existing state but its node-id file is missing")
		}
		if err := store.persistNodeID(); err != nil {
			store.closePartial()
			return err
		}
	}
	trackedLog, err := newConfigurationLog(store.logStore, store.snapshots)
	if err != nil {
		store.closePartial()
		return wrapError(ClassFailed, "load raft configuration index", err)
	}
	store.configLog = trackedLog
	store.logStore = trackedLog

	if store.transport == nil {
		advertise, err := net.ResolveTCPAddr("tcp", store.raftAdvertise)
		if err != nil {
			store.closePartial()
			return wrapError(ClassInvalidArgument, "resolve raft advertise address", err)
		}
		transport, err := raft.NewTCPTransport(store.raftBind, advertise, 3, 10*time.Second, os.Stderr)
		if err != nil {
			store.closePartial()
			return wrapError(ClassFailed, "create raft transport", err)
		}
		store.transport = transport
		store.raftBind = string(transport.LocalAddr())
		if store.raftAdvertise == "" {
			store.raftAdvertise = string(transport.LocalAddr())
		}
	}

	raftConfig := raft.DefaultConfig()
	raftConfig.ProtocolVersion = raft.ProtocolVersionMax
	raftConfig.LocalID = raft.ServerID(store.nodeID)
	raftConfig.ShutdownOnRemove = true
	raftConfig.SnapshotInterval = snapshotInterval
	if store.heartbeatTimeout > 0 {
		raftConfig.HeartbeatTimeout = store.heartbeatTimeout
	}
	if store.electionTimeout > 0 {
		raftConfig.ElectionTimeout = store.electionTimeout
	}
	if store.leaderLeaseTimeout > 0 {
		raftConfig.LeaderLeaseTimeout = store.leaderLeaseTimeout
	}
	if store.logger != nil {
		raftConfig.Logger = store.logger
	} else {
		raftConfig.LogLevel = "INFO"
	}

	r, err := raft.NewRaft(raftConfig, (*fsm)(store), store.logStore, store.stableStore, store.snapshots, store.transport)
	if err != nil {
		store.closePartial()
		return wrapError(ClassFailed, "create raft", err)
	}
	store.raft = r
	log.Infof("new raft created: id=%s bind=%s advertise=%s dir=%s", store.nodeID, store.raftBind, store.raftAdvertise, store.raftDir)
	return nil
}

func (store *Store) validateExistingNodeID() (bool, error) {
	data, err := os.ReadFile(filepath.Join(store.raftDir, nodeIDFileName))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, wrapError(ClassFailed, "read raft node-id file", err)
	}
	if err := store.validatePersistedNodeID(data); err != nil {
		return false, err
	}
	return true, nil
}

func (store *Store) persistNodeID() error {
	path := filepath.Join(store.raftDir, nodeIDFileName)
	data, err := os.ReadFile(path)
	if err == nil {
		return store.validatePersistedNodeID(data)
	}
	if !os.IsNotExist(err) {
		return wrapError(ClassFailed, "read raft node-id file", err)
	}

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		if os.IsExist(err) {
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return wrapError(ClassFailed, "read concurrently created raft node-id file", readErr)
			}
			return store.validatePersistedNodeID(data)
		}
		return wrapError(ClassFailed, "create raft node-id file", err)
	}
	payload := []byte(store.nodeID + "\n")
	written, writeErr := file.Write(payload)
	if writeErr == nil && written != len(payload) {
		writeErr = io.ErrShortWrite
	}
	syncErr := file.Sync()
	closeErr := file.Close()
	if persistErr := errors.Join(writeErr, syncErr, closeErr); persistErr != nil {
		_ = os.Remove(path)
		return wrapError(ClassFailed, "write raft node-id file", persistErr)
	}
	return nil
}

func (store *Store) validatePersistedNodeID(data []byte) error {
	existing := strings.TrimSpace(string(data))
	if existing != store.nodeID {
		return invalidArgument("raft node id mismatch: data dir %s has %q, config has %q", store.raftDir, existing, store.nodeID)
	}
	return nil
}

func (store *Store) closePartial() {
	if store.raft != nil {
		return
	}
	if store.transport != nil {
		if closer, ok := store.transport.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
		store.transport = nil
	}
	store.unwrapConfigurationLog()
	if store.boltStore != nil {
		_ = store.boltStore.Close()
		store.boltStore = nil
		store.logStore = nil
		store.stableStore = nil
	}
}

func (store *Store) unwrapConfigurationLog() {
	if store.configLog == nil {
		return
	}
	store.logStore = store.configLog.LogStore
	store.configLog = nil
}

// Close shuts down raft, then the transport if still owned, then BoltDB.
func (store *Store) Close() error {
	if store == nil {
		return nil
	}
	var errs []error
	if store.raft != nil {
		if err := store.raft.Shutdown().Error(); err != nil {
			errs = append(errs, err)
		}
		store.raft = nil
	}
	if store.transport != nil {
		if closer, ok := store.transport.(interface{ Close() error }); ok {
			if err := closer.Close(); err != nil {
				errs = append(errs, err)
			}
		}
		store.transport = nil
	}
	store.unwrapConfigurationLog()
	if store.boltStore != nil {
		if err := store.boltStore.Close(); err != nil {
			errs = append(errs, err)
		}
		store.boltStore = nil
		store.logStore = nil
		store.stableStore = nil
	}
	return errors.Join(errs...)
}

func (store *Store) genericCommand(op string, bytes []byte) (response interface{}, err error) {
	if store == nil || store.raft == nil {
		return nil, ErrNotEnabled
	}
	if store.raft.State() != raft.Leader {
		return nil, ErrNotLeader
	}

	b, marshalErr := json.Marshal(&storeCommand{Op: op, Value: bytes})
	if marshalErr != nil {
		return nil, marshalErr
	}

	f := store.raft.Apply(b, raftTimeout)
	if err = f.Error(); err != nil {
		return nil, classifyRaftError(err)
	}
	r := f.Response()
	if err, ok := r.(error); ok && err != nil {
		return r, err
	}
	return r, nil
}
