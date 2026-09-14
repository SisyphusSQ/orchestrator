package discovery

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/golib/log"
	instaudit "github.com/openark/orchestrator/internal/inst/audit"
	instdiscovery "github.com/openark/orchestrator/internal/inst/discovery"
	instmodel "github.com/openark/orchestrator/internal/inst/instance"
	orcraft "github.com/openark/orchestrator/internal/raft"
)

// Manual owns explicitly requested asynchronous discoveries independently of
// HTTP requests and of the optional continuous discovery loop.
type Manual struct {
	ctx      context.Context
	cancel   context.CancelFunc
	mu       sync.Mutex
	closed   bool
	wg       sync.WaitGroup
	slots    chan struct{}
	timeout  time.Duration
	discover func(context.Context, *instmodel.InstanceKey) (*instmodel.Instance, error)
	record   func(*instmodel.InstanceKey, error)
}

func NewManual(ctx context.Context) *Manual {
	ctx, cancel := context.WithCancel(ctx)
	cfg := config.FromContext(ctx)
	return &Manual{ctx: ctx, cancel: cancel,
		slots:    make(chan struct{}, max(1, cfg.Topology.Discovery.MaxConcurrency)),
		timeout:  time.Duration(max(1, cfg.MySQL.ConnectTimeoutSeconds+cfg.Topology.MySQL.DiscoveryReadTimeoutSeconds)) * time.Second,
		discover: DiscoverAndPublish,
		record: func(key *instmodel.InstanceKey, err error) {
			message := "completed"
			if err != nil {
				message = fmt.Sprintf("failed: %v", err)
				log.Errorf("asynchronous discovery %s: %v", key.DisplayString(), err)
			}
			if auditErr := instaudit.AuditOperation("async-discover", key, message); auditErr != nil {
				log.Errorf("record asynchronous discovery: %v", auditErr)
			}
		},
	}
}

func DiscoverAndPublish(ctx context.Context, key *instmodel.InstanceKey) (*instmodel.Instance, error) {
	ctx = config.WithSnapshot(ctx)
	instance, err := instdiscovery.ReadTopologyInstanceContext(ctx, key)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Publication has its own Raft outcome semantics. Cancellation after submit
	// must not imply the command was rolled back or permit an automatic replay.
	if _, err := orcraft.PublishCommand("discover", key); err != nil {
		return nil, err
	}
	return instance, nil
}

func (m *Manual) Submit(key instmodel.InstanceKey) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.ctx.Err() != nil {
		return fmt.Errorf("asynchronous discovery is stopping")
	}
	select {
	case m.slots <- struct{}{}:
	default:
		return fmt.Errorf("asynchronous discovery capacity exceeded")
	}
	ctx, cancel := context.WithTimeout(config.WithSnapshot(m.ctx), m.timeout)
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer func() { <-m.slots }()
		defer cancel()
		_, err := m.discover(ctx, &key)
		m.record(&key, err)
	}()
	return nil
}

func (m *Manual) Close() {
	m.mu.Lock()
	m.closed = true
	m.cancel()
	m.mu.Unlock()
	m.wg.Wait()
}
