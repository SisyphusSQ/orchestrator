package inventory

import (
	"github.com/openark/orchestrator/internal/golib/log"
	instmodel "github.com/openark/orchestrator/internal/inst/instance"
)

// FigureClusterName makes a best effort to deduce a cluster name from an alias or instance key.
func FigureClusterName(clusterHint string, instanceKey *instmodel.InstanceKey, thisInstanceKey *instmodel.InstanceKey) (clusterName string, err error) {
	// Look for exact matches, first.

	if clusterHint != "" {
		// Exact cluster name match:
		if clusterInfo, err := ReadClusterInfo(clusterHint); err == nil && clusterInfo != nil {
			return clusterInfo.ClusterName, nil
		}
		// Exact cluster alias match:
		if clustersInfo, err := ReadClustersInfo(""); err == nil {
			for _, clusterInfo := range clustersInfo {
				if clusterInfo.ClusterAlias == clusterHint {
					return clusterInfo.ClusterName, nil
				}
			}
		}
	}

	clusterByInstanceKey := func(instanceKey *instmodel.InstanceKey) (hasResult bool, clusterName string, err error) {
		if instanceKey == nil {
			return false, "", nil
		}
		instance, _, err := ReadInstance(instanceKey)
		if err != nil {
			return true, clusterName, log.Errore(err)
		}
		if instance != nil {
			if instance.ClusterName == "" {
				return true, clusterName, log.Errorf("Unable to determine cluster name for %+v, empty cluster name. clusterHint=%+v", instance.Key, clusterHint)
			}
			return true, instance.ClusterName, nil
		}
		return false, "", nil
	}
	// exact instance key:
	if hasResult, clusterName, err := clusterByInstanceKey(instanceKey); hasResult {
		return clusterName, err
	}
	// fuzzy instance key:
	if hasResult, clusterName, err := clusterByInstanceKey(ReadFuzzyInstanceKeyIfPossible(instanceKey)); hasResult {
		return clusterName, err
	}
	//  Let's see about _this_ instance
	if hasResult, clusterName, err := clusterByInstanceKey(thisInstanceKey); hasResult {
		return clusterName, err
	}
	return clusterName, log.Errorf("Unable to determine cluster name. clusterHint=%+v", clusterHint)
}

// FigureInstanceKey tries to figure out a key
func FigureInstanceKey(instanceKey *instmodel.InstanceKey, thisInstanceKey *instmodel.InstanceKey) (*instmodel.InstanceKey, error) {
	if figuredKey := ReadFuzzyInstanceKeyIfPossible(instanceKey); figuredKey != nil {
		return figuredKey, nil
	}
	figuredKey := thisInstanceKey
	if figuredKey == nil {
		return nil, log.Errorf("Cannot deduce instance %+v", instanceKey)
	}
	return figuredKey, nil
}
