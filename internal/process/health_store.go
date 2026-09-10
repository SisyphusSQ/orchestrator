package process

import (
	"context"
	"fmt"
	"time"

	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/golib/log"
	modeldomain "github.com/openark/orchestrator/internal/models/domain"
	"github.com/openark/orchestrator/internal/repository/metadata"
)

func nodeHealthModel(nodeHealth *NodeHealth) modeldomain.NodeHealth {
	return modeldomain.NodeHealth{
		Hostname:   nodeHealth.Hostname,
		Token:      nodeHealth.Token,
		AppVersion: nodeHealth.AppVersion,
		DBBackend:  nodeHealth.DBBackend,
	}
}

// WriteRegisterNode writes this node's heartbeat through the metadata repository.
func WriteRegisterNode(nodeHealth *NodeHealth) (bool, error) {
	reportedSecondsAgo := int64(time.Since(nodeHealth.LastReported).Seconds())
	if reportedSecondsAgo > config.HealthPollSeconds*2 {
		return false, nil
	}

	row := nodeHealthModel(nodeHealth)
	nodeHealth.onceHistory.Do(func() {
		_ = metadata.InsertNodeHealthHistory(context.Background(), row, nodeHealth.ExtraInfo, nodeHealth.Command)
	})
	updated, err := metadata.UpdateNodeHealth(context.Background(), row, reportedSecondsAgo, nodeHealth.ExtraInfo)
	if err != nil {
		return false, log.Errore(err)
	}
	if updated {
		return true, nil
	}

	if config.Config.IsSQLite() {
		row.DBBackend = config.Config.Metadata.SQLite.DataFile
	} else {
		row.DBBackend = fmt.Sprintf("%s:%d", config.Config.Metadata.MySQL.Host, config.Config.Metadata.MySQL.Port)
	}
	inserted, err := metadata.InsertNodeHealth(
		context.Background(), row, reportedSecondsAgo, nodeHealth.ExtraInfo, nodeHealth.Command,
	)
	if err != nil {
		return false, log.Errore(err)
	}
	return inserted, nil
}

// ExpireAvailableNodes removes nodes that missed their heartbeat window.
func ExpireAvailableNodes() {
	err := metadata.ExpireAvailableNodes(context.Background(), config.HealthPollSeconds*5)
	if err != nil {
		log.Errorf("ExpireAvailableNodes: failed to remove old entries: %+v", err)
	}
}

// ExpireNodesHistory removes old node history rows.
func ExpireNodesHistory() error {
	err := metadata.ExpireNodesHistory(context.Background(), config.Config.Topology.Discovery.UnseenForgetHours)
	return log.Errore(err)
}

// ReadAvailableNodes returns live nodes, optionally constrained to HTTP nodes.
func ReadAvailableNodes(onlyHTTPNodes bool) ([]*NodeHealth, error) {
	extraInfo := ""
	if onlyHTTPNodes {
		extraInfo = string(OrchestratorExecutionHttpMode)
	}
	rows, err := metadata.ReadAvailableNodes(context.Background(), config.HealthPollSeconds, extraInfo)
	nodes := make([]*NodeHealth, 0, len(rows))
	for _, row := range rows {
		nodes = append(nodes, &NodeHealth{
			Hostname:        row.Hostname,
			Token:           row.Token,
			AppVersion:      row.AppVersion,
			FirstSeenActive: row.FirstSeenActive,
			LastSeenActive:  row.LastSeenActive,
			DBBackend:       row.DBBackend,
		})
	}
	return nodes, log.Errore(err)
}

// TokenBelongsToHealthyHttpService checks whether a token belongs to a live HTTP node.
func TokenBelongsToHealthyHttpService(token string) (bool, error) {
	valid, err := metadata.TokenBelongsToHealthyHTTPService(
		context.Background(), token, string(OrchestratorExecutionHttpMode),
	)
	return valid, log.Errore(err)
}
