package app

import (
	"context"
	"sync"
	"time"

	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/db"
	"github.com/openark/orchestrator/internal/logic"
	"github.com/openark/orchestrator/internal/observability"
	orcraft "github.com/openark/orchestrator/internal/raft"
)

// startHealthMonitor owns dependency IO; /metrics and health requests read its atomic snapshot.
func startHealthMonitor() func() error {
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Go(func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			sampleHealth(ctx)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	})
	closeMonitor := func() error { cancel(); wg.Wait(); return nil }
	healthMonitorMu.Lock()
	healthMonitorClose = closeMonitor
	healthMonitorMu.Unlock()
	return closeMonitor
}

func sampleHealth(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	h := observability.Health{RaftEnabled: config.Config.RaftEnabled}
	database, err := db.OpenOrchestratorContext(ctx)
	if err == nil {
		err = database.PingContext(ctx)
	}
	h.Backend = err == nil
	if h.RaftEnabled {
		raftStatus := orcraft.ObservabilityStatus()
		h.LastIndex, h.CommitIndex, h.AppliedIndex = orcraft.LogProgress()
		h.RaftReady = raftStatus.Ready
		h.Leader = raftStatus.IsLeader
		h.LeaderReady = h.Backend && raftStatus.LeaderVerified
		h.Active = h.LeaderReady
	} else {
		h.Active = logic.IsLeader()
		h.LeaderReady = h.Backend && h.Active
	}
	h.Ready = h.Backend && (!h.RaftEnabled || h.RaftReady)
	h.CheckedAt = time.Now()
	observability.SetHealth(h)
}

var healthMonitorMu sync.Mutex
var healthMonitorClose func() error

func CloseHealthMonitor() error {
	healthMonitorMu.Lock()
	closeMonitor := healthMonitorClose
	healthMonitorClose = nil
	healthMonitorMu.Unlock()
	if closeMonitor != nil {
		return closeMonitor()
	}
	return nil
}
