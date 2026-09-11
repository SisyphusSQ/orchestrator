package request

import (
	"fmt"

	instinventory "github.com/openark/orchestrator/internal/inst/inventory"
	instresolve "github.com/openark/orchestrator/internal/inst/resolve"
)

// ClusterHint returns the first route parameter that identifies a cluster.
func ClusterHint(params map[string]string) string {
	if params["clusterHint"] != "" {
		return params["clusterHint"]
	}
	if params["clusterName"] != "" {
		return params["clusterName"]
	}
	if params["host"] != "" && params["port"] != "" {
		return fmt.Sprintf("%s:%s", params["host"], params["port"])
	}
	return ""
}

// ClusterName resolves a cluster name from a route hint.
func ClusterName(hint string) (string, error) {
	if hint == "" {
		return "", fmt.Errorf("unable to determine cluster name by empty hint")
	}
	instanceKey, _ := instresolve.ParseRawInstanceKey(hint)
	return instinventory.FigureClusterName(hint, instanceKey, nil)
}

// ClusterNameIfExists resolves a cluster only when a hint is present.
func ClusterNameIfExists(params map[string]string) (string, error) {
	clusterHint := ClusterHint(params)
	if clusterHint == "" {
		return "", nil
	}
	return ClusterName(clusterHint)
}
