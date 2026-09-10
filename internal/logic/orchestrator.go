/*
   Copyright 2014 Outbrain Inc.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package logic

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/openark/orchestrator/internal/agent"
	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/discovery"
	"github.com/openark/orchestrator/internal/golib/log"
	"github.com/openark/orchestrator/internal/inst"
	"github.com/openark/orchestrator/internal/kv"
	"github.com/openark/orchestrator/internal/process"
	orcraft "github.com/openark/orchestrator/internal/raft"
	"github.com/openark/orchestrator/internal/util"
	"github.com/patrickmn/go-cache"
	"github.com/sjmudd/stopwatch"

	"github.com/openark/orchestrator/internal/observability"
	"go.opentelemetry.io/otel/codes"
)

// discoveryQueue is a channel of deduplicated instanceKey-s
// that were requested for discovery.  It can be continuously updated
// as discovery process progresses.
var discoveryQueue *discovery.Queue
var deadInstancesDiscoveryQueue *discovery.Queue
var discoveryRunning atomic.Bool
var snapshotDiscoveryKeys chan inst.InstanceKey
var snapshotDiscoveryKeysMutex sync.Mutex

var instancePollSecondsExceededCounter = observability.NewCounter("orchestrator_discoveries_instance_poll_seconds_exceeded_total", "discoveries.instance_poll_seconds_exceeded events")

// Manual HTTP discovery also runs when the background loop is disabled.
// Each Add supplies the current polling interval explicitly.
var recentDiscoveryOperationKeys = cache.New(cache.NoExpiration, time.Second)
var pseudoGTIDPublishCache = cache.New(time.Minute, time.Second)
var kvFoundCache = cache.New(10*time.Minute, time.Minute)

func init() {
	snapshotDiscoveryKeys = make(chan inst.InstanceKey, 10)

}

func IsLeader() bool { return orcraft.IsLeaderReady() }

func IsLeaderOrActive() bool { return orcraft.IsReady() }

// used in several places
func instancePollSecondsDuration() time.Duration {
	return time.Duration(config.Config.Topology.Discovery.PollSeconds) * time.Second
}

// AcceptSignals installs the process signal handler once, including HTTP without discovery.
func AcceptSignals() { acceptSignalsOnce() }

var acceptSignalsOnce = sync.OnceFunc(acceptSignals)

// acceptSignals registers for OS signals
func acceptSignals() {
	c := make(chan os.Signal, 1)

	signal.Notify(c, syscall.SIGHUP)
	signal.Notify(c, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		for sig := range c {
			switch sig {
			case syscall.SIGHUP:
				log.Infof("Received SIGHUP. Reloading configuration")
				inst.AuditOperation("reload-configuration", nil, "Triggered via SIGHUP")
				if _, err := config.Reload(); err != nil {
					log.Sugar().Errorw("configuration reload failed", "trigger", "SIGHUP", "error", err)
				}
			case syscall.SIGTERM, syscall.SIGINT:
				log.Infof("Received SIGTERM. Shutting down orchestrator")
				// probably should poke other go routines to stop cleanly here ...
				inst.AuditOperation("shutdown", nil, "Triggered via SIGTERM")
				exitCode := 0
				if err := log.Close(); err != nil {
					fmt.Fprintf(os.Stderr, "logger close failed: %v\n", err)
					exitCode = 1
				}
				os.Exit(exitCode)
			}
		}
	}()
}

// handleDiscoveryRequests iterates the discoveryQueue channel and calls upon
// instance discovery per entry.
func handleDiscoveryRequests() {
	discoveryQueue = discovery.CreateOrReturnQueue("DEFAULT")

	// create a pool of discovery workers
	for i := uint(0); i < config.Config.Topology.Discovery.MaxConcurrency; i++ {
		go func() {
			for {
				instanceKey := discoveryQueue.Consume()
				// Possibly this used to be the elected node, but has
				// been demoted, while still the queue is full.
				if !IsLeaderOrActive() {
					log.Debugf("Node apparently demoted. Skipping discovery of %+v. "+
						"Remaining queue size: %+v", instanceKey, discoveryQueue.QueueLen())
					discoveryQueue.Release(instanceKey)
					continue
				}

				DiscoverInstance(instanceKey)
				discoveryQueue.Release(instanceKey)
			}
		}()
	}

	if config.Config.Topology.Discovery.DeadMaxConcurrency > 0 {
		deadInstancesDiscoveryQueue = discovery.CreateOrReturnQueue("DEADINSTANCES")

		// Register dead instances queue gauge only if the queue exists

		// create a pool of discovery workers
		for i := uint(0); i < config.Config.Topology.Discovery.DeadMaxConcurrency; i++ {
			go func() {
				for {
					instanceKey := deadInstancesDiscoveryQueue.Consume()
					// Possibly this used to be the elected node, but has
					// been demoted, while still the queue is full.
					if !IsLeaderOrActive() {
						log.Debugf("Node apparently demoted. Skipping discovery of %+v. "+
							"Remaining queue size: %+v", instanceKey, deadInstancesDiscoveryQueue.QueueLen())
						deadInstancesDiscoveryQueue.Release(instanceKey)
						continue
					}

					DiscoverInstance(instanceKey)
					deadInstancesDiscoveryQueue.Release(instanceKey)
				}
			}()
		}
	} else {
		deadInstancesDiscoveryQueue = discoveryQueue
	}
}

// DiscoverInstance will attempt to discover (poll) an instance (unless
// it is already up to date) and will also ensure that its master and
// replicas (if any) are also checked.
func DiscoverInstance(instanceKey inst.InstanceKey) {
	if inst.InstanceIsForgotten(&instanceKey) {
		log.Debugf("discoverInstance: skipping discovery of %+v because it is set to be forgotten", instanceKey)
		return
	}
	if inst.FiltersMatchInstanceKey(&instanceKey, config.Config.Topology.Discovery.IgnoreHostnames) {
		log.Debugf("discoverInstance: skipping discovery of %+v because it matches DiscoveryIgnoreHostnameFilters", instanceKey)
		return
	}

	ctx, span := observability.StartSpan(context.Background(), "discovery")
	observationStarted := false
	observationResult := "failure"
	// create stopwatch entries
	latency := stopwatch.NewNamedStopwatch()
	latency.AddMany([]string{
		"backend",
		"instance",
		"total"})
	latency.Start("total") // start the total stopwatch (not changed anywhere else)

	defer func() {
		defer span.End()
		latency.Stop("total")
		discoveryTime := latency.Elapsed("total")
		if observationStarted {
			observability.RecordDiscovery(ctx, observationResult, discoveryTime, latency.Elapsed("backend"), latency.Elapsed("instance"))
			if observationResult == "failure" {
				span.SetStatus(codes.Error, "discovery failed")
			}
		}
		if discoveryTime > instancePollSecondsDuration() {
			instancePollSecondsExceededCounter.Add(context.Background(), 1)
			log.Warningf("discoverInstance exceeded InstancePollSeconds for %+v, took %.4fs", instanceKey, discoveryTime.Seconds())
		}
	}()

	instanceKey.ResolveHostname()
	if !instanceKey.IsValid() {
		return
	}

	// Calculate the expiry period each time as InstancePollSeconds
	// _may_ change during the run of the process (via SIGHUP) and
	// it is not possible to change the cache's default expiry..
	if existsInCacheError := recentDiscoveryOperationKeys.Add(instanceKey.DisplayString(), true, instancePollSecondsDuration()); existsInCacheError != nil {
		// Just recently attempted
		return
	}

	latency.Start("backend")
	instance, found, err := inst.ReadInstanceContext(ctx, &instanceKey)
	latency.Stop("backend")
	if err != nil {
		log.Errorf("discoverInstance: cannot read cached state for %+v; continuing topology discovery: %v", instanceKey, err)
	}
	if found && instance.IsUpToDate && instance.IsLastCheckValid {
		// we've already discovered this one. Skip!
		return
	}

	observability.DiscoveryStarted.Add(ctx, 1)
	observationStarted = true

	// First we've ever heard of this instance. Continue investigation:
	skipped := false
	instance, skipped, err = inst.ReadTopologyInstanceBufferableContext(ctx, &instanceKey, config.Config.Topology.WriteBuffer.Enabled, latency)
	observationResult = observability.Result(err)
	// panic can occur (IO stuff). Therefore it may happen
	// that instance is nil. Check it, but first get the timing metrics.
	totalLatency := latency.Elapsed("total")
	backendLatency := latency.Elapsed("backend")
	instanceLatency := latency.Elapsed("instance")

	if skipped {
		observationResult = "skipped"
		if config.Config.Topology.Discovery.FilterLogsEnabled {
			log.Infof("discoverInstance: skipping discovery of %+v because its replication user matches DiscoveryIgnoreReplicationUsernameFilters", instanceKey)
		}
		return
	}

	if err != nil {
		observationResult = "failure"
	}
	if instance == nil {
		observationResult = "failure"
		if util.ClearToLog("discoverInstance", instanceKey.StringCode()) {
			log.Warningf("DiscoverInstance(%+v) instance is nil in %.3fs (Backend: %.3fs, Instance: %.3fs), error=%+v",
				instanceKey,
				totalLatency.Seconds(),
				backendLatency.Seconds(),
				instanceLatency.Seconds(),
				err)
		}
		return
	}

	if !IsLeaderOrActive() || !discoveryRunning.Load() {
		// Manual or replicated discovery still updates this instance when background crawling is off.
		return
	}

	// Investigate replicas and members of the same replication group:
	for _, replicaKey := range append(instance.ReplicationGroupMembers.GetInstanceKeys(), instance.Replicas.GetInstanceKeys()...) {

		// Avoid noticing some hosts we would otherwise discover
		if inst.FiltersMatchInstanceKey(&replicaKey, config.Config.Topology.Discovery.IgnoreReplicaHostnames) {
			continue
		}

		if !replicaKey.IsValid() {
			continue
		}

		dead, recheck := inst.DeadInstancesFilter.InstanceRecheckNeeded(&replicaKey)
		if dead {
			if recheck {
				deadInstancesDiscoveryQueue.Push(replicaKey)
			}
			// dead, but not recheck time
			continue
		} else {
			discoveryQueue.Push(replicaKey)
		}
	}
	// Investigate master:
	if instance.MasterKey.IsValid() {
		if !inst.FiltersMatchInstanceKey(&instance.MasterKey, config.Config.Topology.Discovery.IgnoreMasterHostnames) {
			dead, recheck := inst.DeadInstancesFilter.InstanceRecheckNeeded(&instance.MasterKey)

			if dead {
				if recheck {
					deadInstancesDiscoveryQueue.Push(instance.MasterKey)
				}
			} else {
				discoveryQueue.Push(instance.MasterKey)
			}
		}
	}
}

// onHealthTick handles the actions to take to discover/poll instances
func onHealthTick() {
	wasAlreadyElected := IsLeader()

	if !IsLeaderOrActive() {
		return
	}
	instanceKeys, err := inst.ReadOutdatedInstanceKeys()
	if err != nil {
		log.Errore(err)
	}

	if !wasAlreadyElected {
		// Just turned to be leader!
		go process.RegisterNode(process.ThisNodeHealth)
		go inst.ExpireMaintenance()
	}

	func() {
		// Normally onHealthTick() shouldn't run concurrently. It is kicked by a ticker.
		// However it _is_ invoked inside a goroutine. I like to be safe here.
		snapshotDiscoveryKeysMutex.Lock()
		defer snapshotDiscoveryKeysMutex.Unlock()

		countSnapshotKeys := len(snapshotDiscoveryKeys)
		for range countSnapshotKeys {
			instanceKeys = append(instanceKeys, <-snapshotDiscoveryKeys)
		}
	}()
	// avoid any logging unless there's something to be done
	if len(instanceKeys) > 0 {
		for _, instanceKey := range instanceKeys {
			if instanceKey.IsValid() {
				dead, recheck := inst.DeadInstancesFilter.InstanceRecheckNeeded(&instanceKey)
				if dead {
					if recheck {
						// this is a dead instance that needs recheck
						deadInstancesDiscoveryQueue.Push(instanceKey)
					}
					// If the instance is dead, but it is not a time to recheck it
					// it will be filtered out here.
					continue
				} else {
					// this is a healthy instance that needs recheck
					discoveryQueue.Push(instanceKey)
				}
			}
		}
	}
}

// publishDiscoverMasters will publish to raft a discovery request for all known masters.
// This makes for a best-effort keep-in-sync between raft nodes, where some may have
// inconsistent data due to hosts being forgotten, for example.
func publishDiscoverMasters() error {
	instances, err := inst.ReadWriteableClustersMasters()
	if err == nil {
		for _, instance := range instances {
			key := instance.Key
			go orcraft.PublishCommand("discover", key)
		}
	}
	return log.Errore(err)
}

// InjectPseudoGTIDOnWriters will inject a PseudoGTID entry on all writable, accessible,
// supported writers.
func InjectPseudoGTIDOnWriters() error {
	instances, err := inst.ReadWriteableClustersMasters()
	if err != nil {
		return log.Errore(err)
	}
	for i := range rand.Perm(len(instances)) {
		instance := instances[i]
		go func() {
			if injected, _ := inst.CheckAndInjectPseudoGTIDOnWriter(instance); injected {
				clusterName := instance.ClusterName

				// We prefer not saturating our raft communication. Pseudo-GTID information is
				// OK to be cached for a while.
				if _, found := pseudoGTIDPublishCache.Get(clusterName); !found {
					pseudoGTIDPublishCache.Set(clusterName, true, cache.DefaultExpiration)
					orcraft.PublishCommand("injected-pseudo-gtid", clusterName)
				}

			}
		}()
	}
	return nil
}

// Write a cluster's master (or all clusters masters) to kv stores.
// This should generally only happen once in a lifetime of a cluster. Otherwise KV
// stores are updated via failovers.
func SubmitMastersToKvStores(clusterName string, force bool) (kvPairs []*kv.KVPair, submittedCount int, err error) {
	kvPairs, err = inst.GetMastersKVPairs(clusterName)
	log.Debugf("kv.SubmitMastersToKvStores, clusterName: %s, force: %+v: numPairs: %+v", clusterName, force, len(kvPairs))
	if err != nil {
		return kvPairs, submittedCount, log.Errore(err)
	}
	var selectedError error
	var submitKvPairs []*kv.KVPair
	for _, kvPair := range kvPairs {
		if !force {
			// !force: Called periodically to auto-populate KV
			// We'd like to avoid some overhead.
			if _, found := kvFoundCache.Get(kvPair.Key); found {
				// Let's not overload database with queries. Let's not overload raft with events.
				continue
			}
			v, found, err := kv.GetValue(kvPair.Key)
			if err == nil && found && v == kvPair.Value {
				// Already has the right value.
				kvFoundCache.Set(kvPair.Key, true, cache.DefaultExpiration)
				continue
			}
		}
		submitKvPairs = append(submitKvPairs, kvPair)
	}
	log.Debugf("kv.SubmitMastersToKvStores: submitKvPairs: %+v", len(submitKvPairs))

	for _, kvPair := range submitKvPairs {
		_, err := orcraft.PublishCommand("put-key-value", kvPair)
		if err == nil {
			submittedCount++
		} else {
			selectedError = err
		}
	}

	if distributeErr := kv.DistributePairs(kvPairs); distributeErr != nil {
		selectedError = errors.Join(selectedError, distributeErr)
	}
	return kvPairs, submittedCount, log.Errore(selectedError)
}

func injectSeeds(seedOnce *sync.Once) {
	seedOnce.Do(func() {
		for _, seed := range config.Config.Topology.Discovery.Seeds {
			instanceKey, err := inst.ParseRawInstanceKey(seed)
			if err == nil {
				inst.InjectSeed(instanceKey)
			} else {
				log.Errorf("Error parsing seed %s: %+v", seed, err)
			}
		}
	})
}

// ContinuousDiscovery starts an asynchronuous infinite discovery process where instances are
// periodically investigated and their status captured, and long since unseen instances are
// purged and forgotten.
func ContinuousDiscovery(ctx context.Context) error {
	log.Infof("continuous discovery: setting up")
	if err := kv.InitKVStores(); err != nil {
		return fmt.Errorf("initialize KV stores: %w", err)
	}
	continuousDiscoveryStartTime := time.Now()
	checkAndRecoverWaitPeriod := 3 * instancePollSecondsDuration()
	recentCache := recentDiscoveryOperationKeys
	observability.Gauge("orchestrator_discovery_recent_instances", "Recent discovery cache entries", func() int64 { return int64(recentCache.ItemCount()) })

	inst.LoadHostnameResolveCache()
	handleDiscoveryRequests()
	discoveryRunning.Store(true)
	defer discoveryRunning.Store(false)

	healthTicker := time.NewTicker(config.HealthPollSeconds * time.Second)
	instancePollTicker := time.NewTicker(instancePollSecondsDuration())
	caretakingTicker := time.NewTicker(time.Minute)
	raftCaretakingTicker := time.NewTicker(10 * time.Minute)
	recoveryTicker := time.NewTicker(time.Duration(config.RecoveryPollSeconds) * time.Second)
	autoPseudoGTIDTicker := time.NewTicker(time.Duration(config.PseudoGTIDIntervalSeconds) * time.Second)
	defer healthTicker.Stop()
	defer instancePollTicker.Stop()
	defer caretakingTicker.Stop()
	defer raftCaretakingTicker.Stop()
	defer recoveryTicker.Stop()
	defer autoPseudoGTIDTicker.Stop()
	var recoveryEntrance atomic.Int64
	var snapshotTopologiesTick <-chan time.Time
	var snapshotTopologiesTicker *time.Ticker
	if config.Config.Topology.Snapshot.IntervalHours > 0 {
		snapshotTopologiesTicker = time.NewTicker(time.Duration(config.Config.Topology.Snapshot.IntervalHours) * time.Hour)
		defer snapshotTopologiesTicker.Stop()
		snapshotTopologiesTick = snapshotTopologiesTicker.C
	}

	runCheckAndRecoverOperationsTimeRipe := func() bool {
		return time.Since(continuousDiscoveryStartTime) >= checkAndRecoverWaitPeriod
	}

	var seedOnce sync.Once

	AcceptSignals()

	log.Infof("continuous discovery: starting")
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-healthTicker.C:
			go func() {
				onHealthTick()
			}()
		case <-instancePollTicker.C:
			go func() {
				// This tick does NOT do instance poll (these are handled by the oversampling discoveryTick)
				// But rather should invoke such routinely operations that need to be as (or roughly as) frequent
				// as instance poll
				if IsLeaderOrActive() {
					go inst.UpdateClusterAliases()
					go inst.ExpireDowntime()
					go injectSeeds(&seedOnce)
				}
			}()
		case <-autoPseudoGTIDTicker.C:
			go func() {
				if config.Config.PseudoGTID.Auto && IsLeader() {
					go InjectPseudoGTIDOnWriters()
				}
			}()
		case <-caretakingTicker.C:
			// Various periodic internal maintenance tasks
			go func() {
				if IsLeaderOrActive() {
					go inst.RecordInstanceCoordinatesHistory()
					go inst.ReviewUnseenInstances()
					go inst.InjectUnseenMasters()

					go inst.ForgetLongUnseenInstances()
					go inst.ForgetLongUnseenClusterAliases()
					go inst.ForgetUnseenInstancesDifferentlyResolved()
					go inst.ForgetExpiredHostnameResolves()
					go inst.DeleteInvalidHostnameResolves()
					go inst.ResolveUnknownMasterHostnameResolves()
					go inst.ExpireMaintenance()
					go inst.ExpireCandidateInstances()
					go inst.ExpireHostnameUnresolve()
					go inst.ExpireClusterDomainName()
					go inst.ExpireAudit()
					go inst.ExpireMasterPositionEquivalence()
					go inst.ExpirePoolInstances()
					go inst.FlushNontrivialResolveCacheToDatabase()
					go inst.ExpireInjectedPseudoGTID()
					go inst.ExpireStaleInstanceBinlogCoordinates()
					go process.ExpireNodesHistory()
					go process.ExpireAccessTokens()
					go process.ExpireAvailableNodes()
					go ExpireFailureDetectionHistory()
					go ExpireTopologyRecoveryHistory()
					go ExpireTopologyRecoveryStepsHistory()

					if runCheckAndRecoverOperationsTimeRipe() && IsLeader() {
						go SubmitMastersToKvStores("", false)
					}
				} else {
					// Take this opportunity to refresh yourself
					go inst.LoadHostnameResolveCache()
				}
			}()
		case <-raftCaretakingTicker.C:
			if IsLeader() {
				go publishDiscoverMasters()
			}
		case <-recoveryTicker.C:
			go func() {
				if IsLeaderOrActive() {
					go ClearActiveFailureDetections()
					go ClearActiveRecoveries()
					go ExpireBlockedRecoveries()
					go AcknowledgeCrashedRecoveries()
					go inst.ExpireInstanceAnalysisChangelog()

					go func() {
						// This function is non re-entrant (it can only be running once at any point in time)
						if recoveryEntrance.CompareAndSwap(0, 1) {
							defer recoveryEntrance.Store(0)
						} else {
							return
						}
						if runCheckAndRecoverOperationsTimeRipe() {
							CheckAndRecover(nil, nil, false)
						} else {
							log.Debugf("Waiting for %+v seconds to pass before running failure detection/recovery", checkAndRecoverWaitPeriod.Seconds())
						}
					}()
				}
			}()
		case <-snapshotTopologiesTick:
			go func() {
				if IsLeaderOrActive() {
					go inst.SnapshotTopologies()
				}
			}()
		}
	}
}

func pollAgent(hostname string) error {
	polledAgent, err := agent.GetAgent(hostname)
	agent.UpdateAgentLastChecked(hostname)

	if err != nil {
		return log.Errore(err)
	}

	err = agent.UpdateAgentInfo(hostname, polledAgent)
	if err != nil {
		return log.Errore(err)
	}

	return nil
}

// ContinuousAgentsPoll starts an asynchronuous infinite process where agents are
// periodically investigated and their status captured, and long since unseen agents are
// purged and forgotten.
func ContinuousAgentsPoll() {
	log.Infof("Starting continuous agents poll")

	go discoverSeededAgents()

	tick := time.Tick(config.HealthPollSeconds * time.Second)
	caretakingTick := time.Tick(time.Hour)
	for range tick {
		agentsHosts, _ := agent.ReadOutdatedAgentsHosts()
		log.Debugf("outdated agents hosts: %+v", agentsHosts)
		for _, hostname := range agentsHosts {
			go pollAgent(hostname)
		}
		// See if we should also forget agents (lower frequency)
		select {
		case <-caretakingTick:
			agent.ForgetLongUnseenAgents()
			agent.FailStaleSeeds()
		default:
		}
	}
}

func discoverSeededAgents() {
	for seededAgent := range agent.SeededAgents {
		instanceKey := &inst.InstanceKey{Hostname: seededAgent.Hostname, Port: int(seededAgent.MySQLPort)}
		go inst.ReadTopologyInstance(instanceKey)
	}
}
