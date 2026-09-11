package inventory

import (
	"context"

	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/golib/log"
	"github.com/openark/orchestrator/internal/repository/metadata"
	"github.com/patrickmn/go-cache"
)

// RegisterInjectedPseudoGTID records a recent pseudo-GTID injection for a cluster.
func RegisterInjectedPseudoGTID(clusterName string) error {
	writeFunc := func() error {
		err := metadata.RegisterInjectedPseudoGTID(context.Background(), clusterName)
		if err == nil {
			clusterInjectedPseudoGTIDCache.Set(clusterName, true, cache.DefaultExpiration)
		}
		return log.Errore(err)
	}
	return metadata.ExecuteWrite(context.Background(), writeFunc)
}

// ExpireInjectedPseudoGTID
func ExpireInjectedPseudoGTID() error {
	writeFunc := func() error {
		err := metadata.ExpireInjectedPseudoGTID(context.Background(), config.PseudoGTIDExpireMinutes)
		return log.Errore(err)
	}
	return metadata.ExecuteWrite(context.Background(), writeFunc)
}

// IsInjectedPseudoGTID reads recent pseudo-GTID injection state from backend DB or cache.
func IsInjectedPseudoGTID(clusterName string) (injected bool, err error) {
	if injectedValue, found := clusterInjectedPseudoGTIDCache.Get(clusterName); found {
		return injectedValue.(bool), err
	}
	injected, err = metadata.IsInjectedPseudoGTID(context.Background(), clusterName)
	clusterInjectedPseudoGTIDCache.Set(clusterName, injected, cache.DefaultExpiration)
	return injected, log.Errore(err)
}
