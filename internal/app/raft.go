package app

import (
	"context"
	"fmt"
	"time"

	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/golib/log"
	"github.com/openark/orchestrator/internal/logic/raftstate"
	"github.com/openark/orchestrator/internal/process"
	orcraft "github.com/openark/orchestrator/internal/raft"
	"github.com/openark/orchestrator/internal/repository"
)

// startRaftRuntime prepares the backend before Raft can restore snapshots or apply logs.
func startRaftRuntime() error {
	if err := repository.InitializeMetadata(context.Background()); err != nil {
		return fmt.Errorf("open raft backend: %w", err)
	}
	if err := orcraft.Setup(raftstate.NewCommandApplier(), raftstate.NewSnapshotDataCreatorApplier(), process.ThisHostname); err != nil {
		return fmt.Errorf("set up raft runtime: %w", err)
	}
	config.LockRaftConfiguration()
	return nil
}

func monitorRaft(parent context.Context) error {
	ctx, cancel := context.WithCancel(parent)
	result := make(chan error, 1)
	exited := make(chan struct{})
	go func() { defer close(exited); result <- orcraft.Monitor(ctx) }()
	defer func() { cancel(); <-exited }()
	tick := time.NewTicker(time.Duration(config.HealthPollSeconds) * time.Second)
	defer tick.Stop()
	for {
		select {
		case err := <-result:
			return err
		case <-ctx.Done():
			return nil
		case <-tick.C:
			unhealthy := process.SinceLastGoodHealthCheck()
			if unhealthy > 30*time.Duration(config.HealthPollSeconds)*time.Second {
				return fmt.Errorf("node cannot register backend health")
			}
			if orcraft.IsLeader() && unhealthy > 5*time.Duration(config.HealthPollSeconds)*time.Second {
				if err := orcraft.TransferLeadership("", ""); err != nil {
					log.Errore(err)
				}
			}
		}
	}
}

// CloseRaftRuntime stops observation before closing Raft; it runs before backend teardown.
func CloseRaftRuntime() error {
	if err := CloseHealthMonitor(); err != nil {
		return err
	}
	return orcraft.Shutdown()
}
