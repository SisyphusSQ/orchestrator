package recovery

// ApplyTopologyRecovery persists a recovery received from the Raft command log.
func ApplyTopologyRecovery(topologyRecovery *TopologyRecovery) error {
	_, err := writeTopologyRecovery(topologyRecovery)
	return err
}

// ApplyTopologyRecoveryStep persists a recovery step received from the Raft command log.
func ApplyTopologyRecoveryStep(topologyRecoveryStep *TopologyRecoveryStep) error {
	return writeTopologyRecoveryStep(topologyRecoveryStep)
}

// ApplyResolvedTopologyRecovery persists a completed recovery received from the Raft command log.
func ApplyResolvedTopologyRecovery(topologyRecovery *TopologyRecovery) error {
	return writeResolveRecovery(topologyRecovery)
}
