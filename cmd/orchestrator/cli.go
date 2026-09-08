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

package main

import (
	"fmt"
	"net"
	"os"
	"os/user"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/openark/orchestrator/internal/agent"
	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/golib/log"
	"github.com/openark/orchestrator/internal/golib/util"
	"github.com/openark/orchestrator/internal/inst"
	"github.com/openark/orchestrator/internal/kv"
	"github.com/openark/orchestrator/internal/logic"
	"github.com/openark/orchestrator/internal/process"
)

var thisInstanceKey *inst.InstanceKey

type stringSlice []string

func (a stringSlice) Len() int           { return len(a) }
func (a stringSlice) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a stringSlice) Less(i, j int) bool { return a[i] < a[j] }

// getClusterName will make a best effort to deduce a cluster name using either a given alias
// or an instanceKey. First attempt is at alias, and if that doesn't work, we try instanceKey.
func getClusterName(clusterAlias string, instanceKey *inst.InstanceKey) (clusterName string) {
	clusterName, _ = inst.FigureClusterName(clusterAlias, instanceKey, thisInstanceKey)
	return clusterName
}

func assignThisInstanceKey() *inst.InstanceKey {
	log.Debugf("Assuming instance is this machine, %+v", thisInstanceKey)
	return thisInstanceKey
}

func cliError(message string, args ...interface{}) error {
	for _, arg := range args {
		message += fmt.Sprintf(" %v", arg)
	}
	return fmt.Errorf("%s", message)
}

func validateInstanceIsFound(instanceKey *inst.InstanceKey) (*inst.Instance, error) {
	if instanceKey == nil {
		return nil, fmt.Errorf("instance key is unresolved")
	}
	instance, _, err := inst.ReadInstance(instanceKey)
	if err != nil {
		return nil, err
	}
	if instance == nil {
		return nil, fmt.Errorf("instance not found: %+v", *instanceKey)
	}
	return instance, nil
}

// runCLIWrapper is called from main and allows for the instance parameter
// to take multiple instance names separated by a comma or whitespace.
func runCLIWrapper(command string, strict bool, instances string, destination string, owner string, reason string, duration string, pattern string, clusterAlias string, pool string, hostnameFlag string) error {
	ignoreRaftSetup := config.RuntimeCLIFlags.IgnoreRaftSetup != nil && *config.RuntimeCLIFlags.IgnoreRaftSetup
	if config.Config.RaftEnabled && !ignoreRaftSetup {
		return fmt.Errorf(`orchestrator is configured to run raft ("RaftEnabled": true); all access must go through the web API of the active raft node; use orchestrator-client or override with --ignore-raft-setup`)
	}
	r := regexp.MustCompile(`[ ,\r\n\t]+`)
	tokens := r.Split(instances, -1)
	switch command {
	case "submit-pool-instances":
		{
			// These commands unsplit the tokens (they expect a comma delimited list of instances)
			tokens = []string{instances}
		}
	}
	for _, instance := range tokens {
		if instance != "" || len(tokens) == 1 {
			if err := runCLI(command, strict, instance, destination, owner, reason, duration, pattern, clusterAlias, pool, hostnameFlag); err != nil {
				return err
			}
		}
	}
	return nil
}

// runCLI initiates a command line interface, executing requested command.
func runCLI(command string, strict bool, instance string, destination string, owner string, reason string, duration string, pattern string, clusterAlias string, pool string, hostnameFlag string) error {
	if synonym, ok := commandSynonyms[command]; ok {
		command = synonym
	}

	skipDatabaseCommands := false
	switch command {
	case "redeploy-internal-db":
		skipDatabaseCommands = true
	case "dump-config":
		skipDatabaseCommands = true
	}

	instanceKey, err := inst.ParseResolveInstanceKey(instance)
	if err != nil {
		instanceKey = nil
	}

	rawInstanceKey, err := inst.ParseRawInstanceKey(instance)
	if err != nil {
		rawInstanceKey = nil
	}

	if destination != "" && !strings.Contains(destination, ":") {
		destination = fmt.Sprintf("%s:%d", destination, config.Config.DefaultInstancePort)
	}
	destinationKey, err := inst.ParseResolveInstanceKey(destination)
	if err != nil {
		destinationKey = nil
	}
	if !skipDatabaseCommands {
		destinationKey = inst.ReadFuzzyInstanceKeyIfPossible(destinationKey)
	}
	if hostname, err := os.Hostname(); err == nil {
		thisInstanceKey = &inst.InstanceKey{Hostname: hostname, Port: int(config.Config.DefaultInstancePort)}
	}
	postponedFunctionsContainer := inst.NewPostponedFunctionsContainer()

	if len(owner) == 0 {
		// get os username as owner
		usr, err := user.Current()
		if err != nil {
			return err
		}
		owner = usr.Username
	}
	inst.SetMaintenanceOwner(owner)

	if !skipDatabaseCommands && !*config.RuntimeCLIFlags.SkipContinuousRegistration {
		process.ContinuousRegistration(string(process.OrchestratorExecutionCliMode), command)
	}
	if err := kv.InitKVStores(); err != nil {
		return fmt.Errorf("initialize KV stores: %w", err)
	}

	// begin commands
	switch command {
	// smart mode
	case "relocate", "relocate-below":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			if destinationKey == nil {
				return cliError("Cannot deduce destination:", destination)
			}
			_, err := inst.RelocateBelow(instanceKey, destinationKey)
			if err != nil {
				return err
			}
			fmt.Println(fmt.Sprintf("%s<%s", instanceKey.DisplayString(), destinationKey.DisplayString()))
		}
	case "relocate-replicas":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			if destinationKey == nil {
				return cliError("Cannot deduce destination:", destination)
			}
			replicas, _, err, errs := inst.RelocateReplicas(instanceKey, destinationKey, pattern)
			if err != nil {
				return err
			} else {
				for _, e := range errs {
					log.Errore(e)
				}
				for _, replica := range replicas {
					fmt.Println(replica.Key.DisplayString())
				}
			}
		}
	case "take-siblings":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			if instanceKey == nil {
				return cliError("Cannot deduce instance:", instance)
			}
			_, _, err := inst.TakeSiblings(instanceKey)
			if err != nil {
				return err
			}
			fmt.Println(instanceKey.DisplayString())
		}
	case "regroup-replicas":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			if instanceKey == nil {
				return cliError("Cannot deduce instance:", instance)
			}
			if _, err := validateInstanceIsFound(instanceKey); err != nil {
				return err
			}

			lostReplicas, equalReplicas, aheadReplicas, cannotReplicateReplicas, promotedReplica, err := inst.RegroupReplicas(instanceKey, false, func(candidateReplica *inst.Instance) { fmt.Println(candidateReplica.Key.DisplayString()) }, postponedFunctionsContainer)
			lostReplicas = append(lostReplicas, cannotReplicateReplicas...)

			postponedFunctionsContainer.Wait()
			if promotedReplica == nil {
				return fmt.Errorf("Could not regroup replicas of %+v; error: %+v", *instanceKey, err)
			}
			fmt.Println(fmt.Sprintf("%s lost: %d, trivial: %d, pseudo-gtid: %d",
				promotedReplica.Key.DisplayString(), len(lostReplicas), len(equalReplicas), len(aheadReplicas)))
			if err != nil {
				return err
			}
		}
		// General replication commands
		// move, binlog file:pos
	case "move-up":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			instance, err := inst.MoveUp(instanceKey)
			if err != nil {
				return err
			}
			fmt.Println(fmt.Sprintf("%s<%s", instanceKey.DisplayString(), instance.MasterKey.DisplayString()))
		}
	case "move-up-replicas":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			if instanceKey == nil {
				return cliError("Cannot deduce instance:", instance)
			}

			movedReplicas, _, err, errs := inst.MoveUpReplicas(instanceKey, pattern)
			if err != nil {
				return err
			} else {
				for _, e := range errs {
					log.Errore(e)
				}
				for _, replica := range movedReplicas {
					fmt.Println(replica.Key.DisplayString())
				}
			}
		}
	case "move-below":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			if destinationKey == nil {
				return cliError("Cannot deduce destination/sibling:", destination)
			}
			_, err := inst.MoveBelow(instanceKey, destinationKey)
			if err != nil {
				return err
			}
			fmt.Println(fmt.Sprintf("%s<%s", instanceKey.DisplayString(), destinationKey.DisplayString()))
		}
	case "move-equivalent":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			if destinationKey == nil {
				return cliError("Cannot deduce destination:", destination)
			}
			_, err := inst.MoveEquivalent(instanceKey, destinationKey)
			if err != nil {
				return err
			}
			fmt.Println(fmt.Sprintf("%s<%s", instanceKey.DisplayString(), destinationKey.DisplayString()))
		}
	case "repoint":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			// destinationKey can be null, in which case the instance repoints to its existing master
			instance, err := inst.Repoint(instanceKey, destinationKey, inst.GTIDHintNeutral)
			if err != nil {
				return err
			}
			fmt.Println(fmt.Sprintf("%s<%s", instanceKey.DisplayString(), instance.MasterKey.DisplayString()))
		}
	case "repoint-replicas":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			repointedReplicas, err, errs := inst.RepointReplicasTo(instanceKey, pattern, destinationKey)
			if err != nil {
				return err
			} else {
				for _, e := range errs {
					log.Errore(e)
				}
				for _, replica := range repointedReplicas {
					fmt.Println(fmt.Sprintf("%s<%s", replica.Key.DisplayString(), instanceKey.DisplayString()))
				}
			}
		}
	case "take-master":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			if instanceKey == nil {
				return cliError("Cannot deduce instance:", instance)
			}
			_, err := inst.TakeMaster(instanceKey, false)
			if err != nil {
				return err
			}
			fmt.Println(instanceKey.DisplayString())
		}
	case "make-co-master":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			_, err := inst.MakeCoMaster(instanceKey)
			if err != nil {
				return err
			}
			fmt.Println(instanceKey.DisplayString())
		}
	case "get-candidate-replica":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			if instanceKey == nil {
				return cliError("Cannot deduce instance:", instance)
			}

			instance, _, _, _, _, err := inst.GetCandidateReplica(instanceKey, false)
			if err != nil {
				return err
			} else {
				fmt.Println(instance.Key.DisplayString())
			}
		}
	case "regroup-replicas-bls":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			if instanceKey == nil {
				return cliError("Cannot deduce instance:", instance)
			}
			if _, err := validateInstanceIsFound(instanceKey); err != nil {
				return err
			}

			_, promotedBinlogServer, err := inst.RegroupReplicasBinlogServers(instanceKey, false)
			if promotedBinlogServer == nil {
				return fmt.Errorf("Could not regroup binlog server replicas of %+v; error: %+v", *instanceKey, err)
			}
			fmt.Println(promotedBinlogServer.Key.DisplayString())
			if err != nil {
				return err
			}
		}
	// move, GTID
	case "move-gtid":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			if destinationKey == nil {
				return cliError("Cannot deduce destination:", destination)
			}
			_, err := inst.MoveBelowGTID(instanceKey, destinationKey)
			if err != nil {
				return err
			}
			fmt.Println(fmt.Sprintf("%s<%s", instanceKey.DisplayString(), destinationKey.DisplayString()))
		}
	case "move-replicas-gtid":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			if destinationKey == nil {
				return cliError("Cannot deduce destination:", destination)
			}
			movedReplicas, _, err, errs := inst.MoveReplicasGTID(instanceKey, destinationKey, pattern)
			if err != nil {
				return err
			} else {
				for _, e := range errs {
					log.Errore(e)
				}
				for _, replica := range movedReplicas {
					fmt.Println(replica.Key.DisplayString())
				}
			}
		}
	case "regroup-replicas-gtid":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			if instanceKey == nil {
				return cliError("Cannot deduce instance:", instance)
			}
			if _, err := validateInstanceIsFound(instanceKey); err != nil {
				return err
			}

			lostReplicas, movedReplicas, cannotReplicateReplicas, promotedReplica, err := inst.RegroupReplicasGTID(instanceKey, false, true, func(candidateReplica *inst.Instance) { fmt.Println(candidateReplica.Key.DisplayString()) }, postponedFunctionsContainer, nil)
			lostReplicas = append(lostReplicas, cannotReplicateReplicas...)

			if promotedReplica == nil {
				return fmt.Errorf("Could not regroup replicas of %+v; error: %+v", *instanceKey, err)
			}
			fmt.Println(fmt.Sprintf("%s lost: %d, moved: %d",
				promotedReplica.Key.DisplayString(), len(lostReplicas), len(movedReplicas)))
			if err != nil {
				return err
			}
		}
		// Pseudo-GTID
	case "match":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			if destinationKey == nil {
				return cliError("Cannot deduce destination:", destination)
			}
			_, _, err := inst.MatchBelow(instanceKey, destinationKey, true)
			if err != nil {
				return err
			}
			fmt.Println(fmt.Sprintf("%s<%s", instanceKey.DisplayString(), destinationKey.DisplayString()))
		}
	case "match-up":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			instance, _, err := inst.MatchUp(instanceKey, true)
			if err != nil {
				return err
			}
			fmt.Println(fmt.Sprintf("%s<%s", instanceKey.DisplayString(), instance.MasterKey.DisplayString()))
		}
	case "rematch":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			instance, _, err := inst.RematchReplica(instanceKey, true)
			if err != nil {
				return err
			}
			fmt.Println(fmt.Sprintf("%s<%s", instanceKey.DisplayString(), instance.MasterKey.DisplayString()))
		}
	case "match-replicas":
		{
			// Move all replicas of "instance" beneath "destination"
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			if instanceKey == nil {
				return cliError("Cannot deduce instance:", instance)
			}
			if destinationKey == nil {
				return cliError("Cannot deduce destination:", destination)
			}

			matchedReplicas, _, err, errs := inst.MultiMatchReplicas(instanceKey, destinationKey, pattern)
			if err != nil {
				return err
			} else {
				for _, e := range errs {
					log.Errore(e)
				}
				for _, replica := range matchedReplicas {
					fmt.Println(replica.Key.DisplayString())
				}
			}
		}
	case "match-up-replicas":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			if instanceKey == nil {
				return cliError("Cannot deduce instance:", instance)
			}

			matchedReplicas, _, err, errs := inst.MatchUpReplicas(instanceKey, pattern)
			if err != nil {
				return err
			} else {
				for _, e := range errs {
					log.Errore(e)
				}
				for _, replica := range matchedReplicas {
					fmt.Println(replica.Key.DisplayString())
				}
			}
		}
	case "regroup-replicas-pgtid":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			if instanceKey == nil {
				return cliError("Cannot deduce instance:", instance)
			}
			if _, err := validateInstanceIsFound(instanceKey); err != nil {
				return err
			}

			onCandidateReplicaChosen := func(candidateReplica *inst.Instance) { fmt.Println(candidateReplica.Key.DisplayString()) }
			lostReplicas, equalReplicas, aheadReplicas, cannotReplicateReplicas, promotedReplica, err := inst.RegroupReplicasPseudoGTID(instanceKey, false, onCandidateReplicaChosen, postponedFunctionsContainer, nil)
			lostReplicas = append(lostReplicas, cannotReplicateReplicas...)
			postponedFunctionsContainer.Wait()
			if promotedReplica == nil {
				return fmt.Errorf("Could not regroup replicas of %+v; error: %+v", *instanceKey, err)
			}
			fmt.Println(fmt.Sprintf("%s lost: %d, trivial: %d, pseudo-gtid: %d",
				promotedReplica.Key.DisplayString(), len(lostReplicas), len(equalReplicas), len(aheadReplicas)))
			if err != nil {
				return err
			}
		}
		// General replication commands
	case "enable-gtid":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			_, err := inst.EnableGTID(instanceKey)
			if err != nil {
				return err
			}
			fmt.Println(instanceKey.DisplayString())
		}
	case "disable-gtid":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			_, err := inst.DisableGTID(instanceKey)
			if err != nil {
				return err
			}
			fmt.Println(instanceKey.DisplayString())
		}
	case "which-gtid-errant":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)

			instance, err := inst.ReadTopologyInstance(instanceKey)
			if err != nil {
				return err
			}
			if instance == nil {
				return fmt.Errorf("Instance not found: %+v", *instanceKey)
			}
			fmt.Println(instance.GtidErrant)
		}
	case "gtid-errant-reset-master":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			_, err := inst.ErrantGTIDResetMaster(instanceKey)
			if err != nil {
				return err
			}
			fmt.Println(instanceKey.DisplayString())
		}
	case "skip-query":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			_, err := inst.SkipQuery(instanceKey)
			if err != nil {
				return err
			}
			fmt.Println(instanceKey.DisplayString())
		}
	case "stop-replica":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			_, err := inst.StopReplication(instanceKey)
			if err != nil {
				return err
			}
			fmt.Println(instanceKey.DisplayString())
		}
	case "start-replica":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			_, err := inst.StartReplication(instanceKey)
			if err != nil {
				return err
			}
			fmt.Println(instanceKey.DisplayString())
		}
	case "restart-replica":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			_, err := inst.RestartReplication(instanceKey)
			if err != nil {
				return err
			}
			fmt.Println(instanceKey.DisplayString())
		}
	case "reset-replica":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			_, err := inst.ResetReplicationOperation(instanceKey)
			if err != nil {
				return err
			}
			fmt.Println(instanceKey.DisplayString())
		}
	case "change-master-credentials":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			if instanceKey == nil {
				return cliError("Cannot deduce instance:", instance)
			}
			creds, err := inst.ReadReplicationCredentials(instanceKey)
			if err != nil {
				return err
			}
			if _, err := inst.ChangeMasterCredentials(instanceKey, creds); err != nil {
				return err
			}
			fmt.Println(instanceKey.DisplayString())
		}
	case "detach-replica-master-host":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			if instanceKey == nil {
				return cliError("Cannot deduce instance:", instance)
			}
			_, err := inst.DetachReplicaMasterHost(instanceKey)
			if err != nil {
				return err
			}
			fmt.Println(instanceKey.DisplayString())
		}
	case "reattach-replica-master-host":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			if instanceKey == nil {
				return cliError("Cannot deduce instance:", instance)
			}
			_, err := inst.ReattachReplicaMasterHost(instanceKey)
			if err != nil {
				return err
			}
			fmt.Println(instanceKey.DisplayString())
		}
	case "master-pos-wait":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			if instanceKey == nil {
				return fmt.Errorf("Unresolved instance")
			}
			instance, err := inst.ReadTopologyInstance(instanceKey)
			if err != nil {
				return err
			}
			if instance == nil {
				return fmt.Errorf("Instance not found: %+v", *instanceKey)
			}
			var binlogCoordinates *inst.BinlogCoordinates

			if binlogCoordinates, err = inst.ParseBinlogCoordinates(*config.RuntimeCLIFlags.BinlogFile); err != nil {
				return fmt.Errorf("Expecing --binlog argument as file:pos")
			}
			_, err = inst.MasterPosWait(instanceKey, binlogCoordinates)
			if err != nil {
				return err
			}
			fmt.Println(instanceKey.DisplayString())
		}
	case "enable-semi-sync-master":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			_, err := inst.SetSemiSyncMaster(instanceKey, true)
			if err != nil {
				return err
			}
			fmt.Println(instanceKey.DisplayString())
		}
	case "disable-semi-sync-master":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			_, err := inst.SetSemiSyncMaster(instanceKey, false)
			if err != nil {
				return err
			}
			fmt.Println(instanceKey.DisplayString())
		}
	case "enable-semi-sync-replica":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			_, err := inst.SetSemiSyncReplica(instanceKey, true)
			if err != nil {
				return err
			}
			fmt.Println(instanceKey.DisplayString())
		}
	case "disable-semi-sync-replica":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			_, err := inst.SetSemiSyncReplica(instanceKey, false)
			if err != nil {
				return err
			}
			fmt.Println(instanceKey.DisplayString())
		}
	case "restart-replica-statements":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			if instanceKey == nil {
				return fmt.Errorf("Unresolved instance")
			}
			statements, err := inst.GetReplicationRestartPreserveStatements(instanceKey, *config.RuntimeCLIFlags.Statement)
			if err != nil {
				return err
			}
			for _, statement := range statements {
				fmt.Println(statement)
			}
		}
		// Replication, information
	case "can-replicate-from":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			if instanceKey == nil {
				return fmt.Errorf("Unresolved instance")
			}
			instance, err := validateInstanceIsFound(instanceKey)
			if err != nil {
				return err
			}
			if destinationKey == nil {
				return cliError("Cannot deduce target instance:", destination)
			}
			otherInstance, err := validateInstanceIsFound(destinationKey)
			if err != nil {
				return err
			}

			if canReplicate, _ := instance.CanReplicateFromEx(otherInstance, "CLI: can-replicate-from"); canReplicate {
				fmt.Println(destinationKey.DisplayString())
			}
		}
	case "is-replicating":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			if instanceKey == nil {
				return fmt.Errorf("Unresolved instance")
			}
			instance, err := validateInstanceIsFound(instanceKey)
			if err != nil {
				return err
			}
			if instance.ReplicaRunning() {
				fmt.Println(instance.Key.DisplayString())
			}
		}
	case "is-replication-stopped":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			if instanceKey == nil {
				return fmt.Errorf("Unresolved instance")
			}
			instance, err := validateInstanceIsFound(instanceKey)
			if err != nil {
				return err
			}
			if instance.ReplicationThreadsStopped() {
				fmt.Println(instance.Key.DisplayString())
			}
		}
		// Instance
	case "set-read-only":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			_, err := inst.SetReadOnly(instanceKey, true)
			if err != nil {
				return err
			}
			fmt.Println(instanceKey.DisplayString())
		}
	case "set-writeable":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			_, err := inst.SetReadOnly(instanceKey, false)
			if err != nil {
				return err
			}
			fmt.Println(instanceKey.DisplayString())
		}
		// Binary log operations
	case "flush-binary-logs":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			var err error
			if *config.RuntimeCLIFlags.BinlogFile == "" {
				_, err = inst.FlushBinaryLogs(instanceKey, 1)
			} else {
				_, err = inst.FlushBinaryLogsTo(instanceKey, *config.RuntimeCLIFlags.BinlogFile)
			}
			if err != nil {
				return err
			}
			fmt.Println(instanceKey.DisplayString())
		}
	case "purge-binary-logs":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			var err error
			if *config.RuntimeCLIFlags.BinlogFile == "" {
				return cliError("expecting --binlog value")
			}

			_, err = inst.PurgeBinaryLogsTo(instanceKey, *config.RuntimeCLIFlags.BinlogFile, false)
			if err != nil {
				return err
			}
			fmt.Println(instanceKey.DisplayString())
		}
	case "last-pseudo-gtid":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			if instanceKey == nil {
				return fmt.Errorf("Unresolved instance")
			}
			instance, err := inst.ReadTopologyInstance(instanceKey)
			if err != nil {
				return err
			}
			if instance == nil {
				return fmt.Errorf("Instance not found: %+v", *instanceKey)
			}
			coordinates, text, err := inst.FindLastPseudoGTIDEntry(instance, instance.RelaylogCoordinates, nil, strict, nil)
			if err != nil {
				return err
			}
			fmt.Println(fmt.Sprintf("%+v:%s", *coordinates, text))
		}
	case "locate-gtid-errant":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			if instanceKey == nil {
				return fmt.Errorf("Unresolved instance")
			}
			errantBinlogs, err := inst.LocateErrantGTID(instanceKey)
			if err != nil {
				return err
			}
			for _, binlog := range errantBinlogs {
				fmt.Println(binlog)
			}
		}
	case "last-executed-relay-entry":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			if instanceKey == nil {
				return fmt.Errorf("Unresolved instance")
			}
			instance, err := inst.ReadTopologyInstance(instanceKey)
			if err != nil {
				return err
			}
			if instance == nil {
				return fmt.Errorf("Instance not found: %+v", *instanceKey)
			}
			minCoordinates, err := inst.GetPreviousKnownRelayLogCoordinatesForInstance(instance)
			if err != nil {
				return fmt.Errorf("Error reading last known coordinates for %+v: %+v", instance.Key, err)
			}
			binlogEvent, err := inst.GetLastExecutedEntryInRelayLogs(instance, minCoordinates, instance.RelaylogCoordinates)
			if err != nil {
				return err
			}
			fmt.Println(fmt.Sprintf("%+v:%d", *binlogEvent, binlogEvent.NextEventPos))
		}
	case "correlate-relaylog-pos":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			if instanceKey == nil {
				return fmt.Errorf("Unresolved instance")
			}
			instance, err := inst.ReadTopologyInstance(instanceKey)
			if err != nil {
				return err
			}
			if instance == nil {
				return fmt.Errorf("Instance not found: %+v", *instanceKey)
			}
			if destinationKey == nil {
				return cliError("Cannot deduce target instance:", destination)
			}
			otherInstance, err := inst.ReadTopologyInstance(destinationKey)
			if err != nil {
				return err
			}
			if otherInstance == nil {
				return fmt.Errorf("Instance not found: %+v", *destinationKey)
			}

			var relaylogCoordinates *inst.BinlogCoordinates
			if *config.RuntimeCLIFlags.BinlogFile != "" {
				if relaylogCoordinates, err = inst.ParseBinlogCoordinates(*config.RuntimeCLIFlags.BinlogFile); err != nil {
					return fmt.Errorf("Expecing --binlog argument as file:pos")
				}
			}
			instanceCoordinates, correlatedCoordinates, nextCoordinates, _, err := inst.CorrelateRelaylogCoordinates(instance, relaylogCoordinates, otherInstance)
			if err != nil {
				return err
			}
			fmt.Println(fmt.Sprintf("%+v;%+v;%+v", *instanceCoordinates, *correlatedCoordinates, *nextCoordinates))
		}
	case "find-binlog-entry":
		{
			if pattern == "" {
				return cliError("No pattern given")
			}
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			if instanceKey == nil {
				return fmt.Errorf("Unresolved instance")
			}
			instance, err := inst.ReadTopologyInstance(instanceKey)
			if err != nil {
				return err
			}
			if instance == nil {
				return fmt.Errorf("Instance not found: %+v", *instanceKey)
			}
			coordinates, err := inst.SearchEntryInInstanceBinlogs(instance, pattern, false, nil)
			if err != nil {
				return err
			}
			fmt.Println(fmt.Sprintf("%+v", *coordinates))
		}
	case "correlate-binlog-pos":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			if instanceKey == nil {
				return fmt.Errorf("Unresolved instance")
			}
			instance, err := inst.ReadTopologyInstance(instanceKey)
			if err != nil {
				return err
			}
			if instance == nil {
				return fmt.Errorf("Instance not found: %+v", *instanceKey)
			}
			if !instance.LogBinEnabled {
				return fmt.Errorf("Instance does not have binary logs: %+v", *instanceKey)
			}
			if destinationKey == nil {
				return cliError("Cannot deduce target instance:", destination)
			}
			otherInstance, err := inst.ReadTopologyInstance(destinationKey)
			if err != nil {
				return err
			}
			if otherInstance == nil {
				return fmt.Errorf("Instance not found: %+v", *destinationKey)
			}
			var binlogCoordinates *inst.BinlogCoordinates
			if *config.RuntimeCLIFlags.BinlogFile == "" {
				binlogCoordinates = &instance.SelfBinlogCoordinates
			} else {
				if binlogCoordinates, err = inst.ParseBinlogCoordinates(*config.RuntimeCLIFlags.BinlogFile); err != nil {
					return fmt.Errorf("Expecing --binlog argument as file:pos")
				}
			}

			coordinates, _, err := inst.CorrelateBinlogCoordinates(instance, binlogCoordinates, otherInstance)
			if err != nil {
				return err
			}
			fmt.Println(fmt.Sprintf("%+v", *coordinates))
		}
		// Pool
	case "submit-pool-instances":
		{
			if pool == "" {
				return cliError("Please submit --pool")
			}
			err := inst.ApplyPoolInstances(inst.NewPoolInstancesSubmission(pool, instance))
			if err != nil {
				return err
			}
		}
	case "cluster-pool-instances":
		{
			clusterPoolInstances, err := inst.ReadAllClusterPoolInstances()
			if err != nil {
				return err
			}
			for _, clusterPoolInstance := range clusterPoolInstances {
				fmt.Println(fmt.Sprintf("%s\t%s\t%s\t%s:%d", clusterPoolInstance.ClusterName, clusterPoolInstance.ClusterAlias, clusterPoolInstance.Pool, clusterPoolInstance.Hostname, clusterPoolInstance.Port))
			}
		}
	case "which-heuristic-cluster-pool-instances":
		{
			clusterName := getClusterName(clusterAlias, instanceKey)

			instances, err := inst.GetHeuristicClusterPoolInstances(clusterName, pool)
			if err != nil {
				return err
			} else {
				for _, instance := range instances {
					fmt.Println(instance.Key.DisplayString())
				}
			}
		}
		// Information
	case "find":
		{
			if pattern == "" {
				return cliError("No pattern given")
			}
			instances, err := inst.FindInstances(pattern)
			if err != nil {
				return err
			} else {
				for _, instance := range instances {
					fmt.Println(instance.Key.DisplayString())
				}
			}
		}
	case "search":
		{
			if pattern == "" {
				return cliError("No pattern given")
			}
			instances, err := inst.SearchInstances(pattern)
			if err != nil {
				return err
			} else {
				for _, instance := range instances {
					fmt.Println(instance.Key.DisplayString())
				}
			}
		}
	case "clusters":
		{
			clusters, err := inst.ReadClusters()
			if err != nil {
				return err
			}
			fmt.Println(strings.Join(clusters, "\n"))
		}
	case "clusters-alias":
		{
			clusters, err := inst.ReadClustersInfo("")
			if err != nil {
				return err
			}
			for _, cluster := range clusters {
				fmt.Println(fmt.Sprintf("%s\t%s", cluster.ClusterName, cluster.ClusterAlias))
			}
		}
	case "all-clusters-masters":
		{
			instances, err := inst.ReadWriteableClustersMasters()
			if err != nil {
				return err
			} else {
				for _, instance := range instances {
					fmt.Println(instance.Key.DisplayString())
				}
			}
		}
	case "topology":
		{
			clusterName := getClusterName(clusterAlias, instanceKey)
			output, err := inst.ASCIITopology(clusterName, pattern, false, false)
			if err != nil {
				return err
			}
			fmt.Println(output)
		}
	case "topology-tabulated":
		{
			clusterName := getClusterName(clusterAlias, instanceKey)
			output, err := inst.ASCIITopology(clusterName, pattern, true, false)
			if err != nil {
				return err
			}
			fmt.Println(output)
		}
	case "topology-tags":
		{
			clusterName := getClusterName(clusterAlias, instanceKey)
			output, err := inst.ASCIITopology(clusterName, pattern, false, true)
			if err != nil {
				return err
			}
			fmt.Println(output)
		}
	case "all-instances":
		{
			instances, err := inst.SearchInstances("")
			if err != nil {
				return err
			} else {
				for _, instance := range instances {
					fmt.Println(instance.Key.DisplayString())
				}
			}
		}
	case "which-instance":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			if instanceKey == nil {
				return fmt.Errorf("Unable to get master: unresolved instance")
			}
			instance, err := validateInstanceIsFound(instanceKey)
			if err != nil {
				return err
			}
			fmt.Println(instance.Key.DisplayString())
		}
	case "which-cluster":
		{
			clusterName := getClusterName(clusterAlias, instanceKey)
			fmt.Println(clusterName)
		}
	case "which-cluster-alias":
		{
			clusterName := getClusterName(clusterAlias, instanceKey)
			clusterInfo, err := inst.ReadClusterInfo(clusterName)
			if err != nil {
				return err
			}
			fmt.Println(clusterInfo.ClusterAlias)
		}
	case "which-cluster-domain":
		{
			clusterName := getClusterName(clusterAlias, instanceKey)
			clusterInfo, err := inst.ReadClusterInfo(clusterName)
			if err != nil {
				return err
			}
			fmt.Println(clusterInfo.ClusterDomain)
		}
	case "which-heuristic-domain-instance":
		{
			clusterName := getClusterName(clusterAlias, instanceKey)
			instanceKey, err := inst.GetHeuristicClusterDomainInstanceAttribute(clusterName)
			if err != nil {
				return err
			}
			fmt.Println(instanceKey.DisplayString())
		}
	case "which-cluster-master":
		{
			clusterName := getClusterName(clusterAlias, instanceKey)
			masters, err := inst.ReadClusterMaster(clusterName)
			if err != nil {
				return err
			}
			if len(masters) == 0 {
				return fmt.Errorf("No writeable masters found for cluster %+v", clusterName)
			}
			fmt.Println(masters[0].Key.DisplayString())
		}
	case "which-cluster-instances":
		{
			clusterName := getClusterName(clusterAlias, instanceKey)
			instances, err := inst.ReadClusterInstances(clusterName)
			if err != nil {
				return err
			}
			for _, clusterInstance := range instances {
				fmt.Println(clusterInstance.Key.DisplayString())
			}
		}
	case "which-cluster-osc-replicas":
		{
			clusterName := getClusterName(clusterAlias, instanceKey)
			instances, err := inst.GetClusterOSCReplicas(clusterName)
			if err != nil {
				return err
			}
			for _, clusterInstance := range instances {
				fmt.Println(clusterInstance.Key.DisplayString())
			}
		}
	case "which-cluster-gh-ost-replicas":
		{
			clusterName := getClusterName(clusterAlias, instanceKey)
			instances, err := inst.GetClusterGhostReplicas(clusterName)
			if err != nil {
				return err
			}
			for _, clusterInstance := range instances {
				fmt.Println(clusterInstance.Key.DisplayString())
			}
		}
	case "which-master":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			if instanceKey == nil {
				return fmt.Errorf("Unable to get master: unresolved instance")
			}
			instance, err := validateInstanceIsFound(instanceKey)
			if err != nil {
				return err
			}
			if instance.MasterKey.IsValid() {
				fmt.Println(instance.MasterKey.DisplayString())
			}
		}
	case "which-downtimed-instances":
		{
			clusterName := getClusterName(clusterAlias, instanceKey)
			instances, err := inst.ReadDowntimedInstances(clusterName)
			if err != nil {
				return err
			}
			for _, clusterInstance := range instances {
				fmt.Println(clusterInstance.Key.DisplayString())
			}
		}
	case "which-replicas":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			if instanceKey == nil {
				return fmt.Errorf("Unable to get replicas: unresolved instance")
			}
			replicas, err := inst.ReadReplicaInstances(instanceKey)
			if err != nil {
				return err
			}
			for _, replica := range replicas {
				fmt.Println(replica.Key.DisplayString())
			}
		}
	case "which-lost-in-recovery":
		{
			instances, err := inst.ReadLostInRecoveryInstances("")
			if err != nil {
				return err
			}
			for _, instance := range instances {
				fmt.Println(instance.Key.DisplayString())
			}
		}
	case "instance-status":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			if instanceKey == nil {
				return fmt.Errorf("Unable to get status: unresolved instance")
			}
			instance, err := validateInstanceIsFound(instanceKey)
			if err != nil {
				return err
			}
			fmt.Println(instance.HumanReadableDescription())
		}
	case "get-cluster-heuristic-lag":
		{
			clusterName := getClusterName(clusterAlias, instanceKey)
			lag, err := inst.GetClusterHeuristicLag(clusterName)
			if err != nil {
				return err
			}
			fmt.Println(lag)
		}
	case "submit-masters-to-kv-stores":
		{
			clusterName := getClusterName(clusterAlias, instanceKey)
			log.Debugf("cluster name is <%s>", clusterName)

			kvPairs, _, err := logic.SubmitMastersToKvStores(clusterName, true)
			if err != nil {
				return err
			}
			for _, kvPair := range kvPairs {
				fmt.Println(fmt.Sprintf("%s:%s", kvPair.Key, kvPair.Value))
			}
		}

	case "tags":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			tags, err := inst.ReadInstanceTags(instanceKey)
			if err != nil {
				return err
			}
			for _, tag := range tags {
				fmt.Println(tag.String())
			}
		}
	case "tag-value":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			tag, err := inst.ParseTag(*config.RuntimeCLIFlags.Tag)
			if err != nil {
				return err
			}

			tagExists, err := inst.ReadInstanceTag(instanceKey, tag)
			if err != nil {
				return err
			}
			if tagExists {
				fmt.Println(tag.TagValue)
			}
		}
	case "tagged":
		{
			tagsString := *config.RuntimeCLIFlags.Tag
			instanceKeyMap, err := inst.GetInstanceKeysByTags(tagsString)
			if err != nil {
				return err
			}
			keysDisplayStrings := []string{}
			for _, key := range instanceKeyMap.GetInstanceKeys() {
				keysDisplayStrings = append(keysDisplayStrings, key.DisplayString())
			}
			sort.Strings(keysDisplayStrings)
			for _, s := range keysDisplayStrings {
				fmt.Println(s)
			}
		}
	case "tag":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			tag, err := inst.ParseTag(*config.RuntimeCLIFlags.Tag)
			if err != nil {
				return err
			}
			inst.PutInstanceTag(instanceKey, tag)
			fmt.Println(instanceKey.DisplayString())
		}
	case "untag":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			tag, err := inst.ParseTag(*config.RuntimeCLIFlags.Tag)
			if err != nil {
				return err
			}
			untagged, err := inst.Untag(instanceKey, tag)
			if err != nil {
				return err
			}
			for _, key := range untagged.GetInstanceKeys() {
				fmt.Println(key.DisplayString())
			}
		}
	case "untag-all":
		{
			tag, err := inst.ParseTag(*config.RuntimeCLIFlags.Tag)
			if err != nil {
				return err
			}
			untagged, err := inst.Untag(nil, tag)
			if err != nil {
				return err
			}
			for _, key := range untagged.GetInstanceKeys() {
				fmt.Println(key.DisplayString())
			}
		}

		// Instance management
	case "discover":
		{
			if instanceKey == nil {
				instanceKey = thisInstanceKey
			}
			if instanceKey == nil {
				return fmt.Errorf("Cannot figure instance key")
			}
			instance, err := inst.ReadTopologyInstance(instanceKey)
			if err != nil {
				return err
			}
			fmt.Println(instance.Key.DisplayString())
		}
	case "forget":
		{
			if rawInstanceKey == nil {
				return cliError("Cannot deduce instance:", instance)
			}
			instanceKey, _ = inst.FigureInstanceKey(rawInstanceKey, nil)
			err := inst.ForgetInstance(instanceKey)
			if err != nil {
				return err
			}
			fmt.Println(instanceKey.DisplayString())
		}
	case "begin-maintenance":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			if reason == "" {
				return cliError("--reason option required")
			}
			var durationSeconds int = 0
			if duration != "" {
				durationSeconds, err = util.SimpleTimeToSeconds(duration)
				if err != nil {
					return err
				}
				if durationSeconds < 0 {
					return fmt.Errorf("Duration value must be non-negative. Given value: %d", durationSeconds)
				}
			}
			maintenanceKey, err := inst.BeginBoundedMaintenance(instanceKey, inst.GetMaintenanceOwner(), reason, uint(durationSeconds), true)
			if err == nil {
				log.Infof("Maintenance key: %+v", maintenanceKey)
				log.Infof("Maintenance duration: %d seconds", durationSeconds)
			}
			if err != nil {
				return err
			}
			fmt.Println(instanceKey.DisplayString())
		}
	case "end-maintenance":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			_, err := inst.EndMaintenanceByInstanceKey(instanceKey)
			if err != nil {
				return err
			}
			fmt.Println(instanceKey.DisplayString())
		}
	case "in-maintenance":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			inMaintenance, err := inst.InMaintenance(instanceKey)
			if err != nil {
				return err
			}
			if inMaintenance {
				fmt.Println(instanceKey.DisplayString())
			}
		}
	case "begin-downtime":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			if reason == "" {
				return cliError("--reason option required")
			}
			var durationSeconds int = 0
			if duration != "" {
				durationSeconds, err = util.SimpleTimeToSeconds(duration)
				if err != nil {
					return err
				}
				if durationSeconds < 0 {
					return fmt.Errorf("Duration value must be non-negative. Given value: %d", durationSeconds)
				}
			}
			duration := time.Duration(durationSeconds) * time.Second
			err := inst.BeginDowntime(inst.NewDowntime(instanceKey, inst.GetMaintenanceOwner(), reason, duration))
			if err == nil {
				log.Infof("Downtime duration: %d seconds", durationSeconds)
			} else {
				return err
			}
			fmt.Println(instanceKey.DisplayString())
		}
	case "end-downtime":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			_, err := inst.EndDowntime(instanceKey)
			if err != nil {
				return err
			}
			fmt.Println(instanceKey.DisplayString())
		}
		// Recovery & analysis
	case "recover", "recover-lite":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			if instanceKey == nil {
				return cliError("Cannot deduce instance:", instance)
			}

			recoveryAttempted, promotedInstanceKey, err := logic.CheckAndRecover(instanceKey, destinationKey, (command == "recover-lite"))
			if err != nil {
				return err
			}
			if recoveryAttempted {
				if promotedInstanceKey == nil {
					return fmt.Errorf("Recovery attempted yet no replica promoted")
				}
				fmt.Println(promotedInstanceKey.DisplayString())
			}
		}
	case "force-master-failover":
		{
			clusterName := getClusterName(clusterAlias, instanceKey)
			topologyRecovery, err := logic.ForceMasterFailover(clusterName)
			if err != nil {
				return err
			}
			fmt.Println(topologyRecovery.SuccessorKey.DisplayString())
		}
	case "force-master-takeover":
		{
			clusterName := getClusterName(clusterAlias, instanceKey)
			if destinationKey == nil {
				return cliError("Cannot deduce destination, the instance to promote in place of the master. Please provide with -d")
			}
			destination, err := validateInstanceIsFound(destinationKey)
			if err != nil {
				return err
			}
			topologyRecovery, err := logic.ForceMasterTakeover(clusterName, destination)
			if err != nil {
				return err
			}
			fmt.Println(topologyRecovery.SuccessorKey.DisplayString())
		}
	case "graceful-master-takeover":
		{
			clusterName := getClusterName(clusterAlias, instanceKey)
			if destinationKey != nil {
				if _, err := validateInstanceIsFound(destinationKey); err != nil {
					return err
				}
			}
			topologyRecovery, promotedMasterCoordinates, err := logic.GracefulMasterTakeover(clusterName, destinationKey, false)
			if err != nil {
				return err
			}
			fmt.Println(topologyRecovery.SuccessorKey.DisplayString())
			fmt.Println(*promotedMasterCoordinates)
			log.Debugf("Promoted %+v as new master. Binlog coordinates at time of promotion: %+v", topologyRecovery.SuccessorKey, *promotedMasterCoordinates)
		}
	case "graceful-master-takeover-auto":
		{
			clusterName := getClusterName(clusterAlias, instanceKey)
			// destinationKey doesn't _have_ to be specified: if unspecified, orchestrator will auto-deduce a replica.
			// but if specified, then that's the replica to promote, and it must be valid.
			if destinationKey != nil {
				if _, err := validateInstanceIsFound(destinationKey); err != nil {
					return err
				}
			}
			topologyRecovery, promotedMasterCoordinates, err := logic.GracefulMasterTakeover(clusterName, destinationKey, true)
			if err != nil {
				return err
			}
			fmt.Println(topologyRecovery.SuccessorKey.DisplayString())
			fmt.Println(*promotedMasterCoordinates)
			log.Debugf("Promoted %+v as new master. Binlog coordinates at time of promotion: %+v", topologyRecovery.SuccessorKey, *promotedMasterCoordinates)
		}
	case "replication-analysis":
		{
			analysis, err := inst.GetReplicationAnalysis("", &inst.ReplicationAnalysisHints{})
			if err != nil {
				return err
			}
			for _, entry := range analysis {
				fmt.Println(fmt.Sprintf("%s (cluster %s): %s", entry.AnalyzedInstanceKey.DisplayString(), entry.ClusterDetails.ClusterName, entry.AnalysisString()))
			}
		}
	case "ack-all-recoveries":
		{
			if reason == "" {
				return cliError("--reason option required (comment your ack)")
			}
			countRecoveries, err := logic.AcknowledgeAllRecoveries(inst.GetMaintenanceOwner(), reason)
			if err != nil {
				return err
			}
			fmt.Println(fmt.Sprintf("%d recoveries acknowledged", countRecoveries))
		}
	case "ack-cluster-recoveries":
		{
			if reason == "" {
				return cliError("--reason option required (comment your ack)")
			}
			clusterName := getClusterName(clusterAlias, instanceKey)
			countRecoveries, err := logic.AcknowledgeClusterRecoveries(clusterName, inst.GetMaintenanceOwner(), reason)
			if err != nil {
				return err
			}
			fmt.Println(fmt.Sprintf("%d recoveries acknowledged", countRecoveries))
		}
	case "ack-instance-recoveries":
		{
			if reason == "" {
				return cliError("--reason option required (comment your ack)")
			}
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)

			countRecoveries, err := logic.AcknowledgeInstanceRecoveries(instanceKey, inst.GetMaintenanceOwner(), reason)
			if err != nil {
				return err
			}
			fmt.Println(fmt.Sprintf("%d recoveries acknowledged", countRecoveries))
		}
	// Instance meta
	case "register-candidate":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			promotionRule, err := inst.ParseCandidatePromotionRule(*config.RuntimeCLIFlags.PromotionRule)
			if err != nil {
				return err
			}
			err = inst.RegisterCandidateInstance(inst.NewCandidateDatabaseInstance(instanceKey, promotionRule).WithCurrentTime())
			if err != nil {
				return err
			}
			fmt.Println(instanceKey.DisplayString())
		}
	case "register-hostname-unresolve":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			err := inst.RegisterHostnameUnresolve(inst.NewHostnameRegistration(instanceKey, hostnameFlag))
			if err != nil {
				return err
			}
			fmt.Println(instanceKey.DisplayString())
		}
	case "deregister-hostname-unresolve":
		{
			instanceKey, _ = inst.FigureInstanceKey(instanceKey, thisInstanceKey)
			err := inst.RegisterHostnameUnresolve(inst.NewHostnameDeregistration(instanceKey))
			if err != nil {
				return err
			}
			fmt.Println(instanceKey.DisplayString())
		}
	case "set-heuristic-domain-instance":
		{
			clusterName := getClusterName(clusterAlias, instanceKey)
			instanceKey, err := inst.HeuristicallyApplyClusterDomainInstanceAttribute(clusterName)
			if err != nil {
				return err
			}
			fmt.Println(instanceKey.DisplayString())
		}

		// meta
	case "snapshot-topologies":
		{
			err := inst.SnapshotTopologies()
			if err != nil {
				return err
			}
		}
	case "continuous":
		{
			if err := logic.ContinuousDiscovery(); err != nil {
				return err
			}
		}
	case "active-nodes":
		{
			nodes, err := process.ReadAvailableNodes(false)
			if err != nil {
				return err
			}
			for _, node := range nodes {
				fmt.Println(node)
			}
		}
	case "access-token":
		{
			publicToken, err := process.GenerateAccessToken(owner)
			if err != nil {
				return err
			}
			fmt.Println(publicToken)
		}
	case "resolve":
		{
			if rawInstanceKey == nil {
				return cliError("Cannot deduce instance:", instance)
			}
			if conn, err := net.Dial("tcp", rawInstanceKey.DisplayString()); err == nil {
				log.Debugf("tcp test is good; got connection %+v", conn)
				conn.Close()
			} else {
				return err
			}
			if cname, err := inst.GetCNAME(rawInstanceKey.Hostname); err == nil {
				log.Debugf("GetCNAME() %+v, %+v", cname, err)
				rawInstanceKey.Hostname = cname
				fmt.Println(rawInstanceKey.DisplayString())
			} else {
				return err
			}
		}
	case "reset-hostname-resolve-cache":
		{
			err := inst.ResetHostnameResolveCache()
			if err != nil {
				return err
			}
			fmt.Println("hostname resolve cache cleared")
		}
	case "dump-config":
		{
			jsonString := config.Config.ToJSONString()
			fmt.Println(jsonString)
		}
	case "show-resolve-hosts":
		{
			resolves, err := inst.ReadAllHostnameResolves()
			if err != nil {
				return err
			}
			for _, r := range resolves {
				fmt.Println(r)
			}
		}
	case "show-unresolve-hosts":
		{
			unresolves, err := inst.ReadAllHostnameUnresolves()
			if err != nil {
				return err
			}
			for _, r := range unresolves {
				fmt.Println(r)
			}
		}
	case "redeploy-internal-db":
		{
			config.RuntimeCLIFlags.ConfiguredVersion = ""
			_, err := inst.ReadClusters()
			if err != nil {
				return err
			}
			fmt.Println("Redeployed internal db")
		}
	case "internal-suggest-promoted-replacement":
		{
			destination, err := validateInstanceIsFound(destinationKey)
			if err != nil {
				return err
			}
			replacement, _, err := logic.SuggestReplacementForPromotedReplica(&logic.TopologyRecovery{}, instanceKey, destination, nil)
			if err != nil {
				return err
			}
			fmt.Println(replacement.Key.DisplayString())
		}
	case "custom-command":
		{
			output, err := agent.CustomCommand(hostnameFlag, pattern)
			if err != nil {
				return err
			}

			fmt.Printf("%v\n", output)
		}
	case "disable-global-recoveries":
		{
			if err := logic.DisableRecovery(); err != nil {
				return fmt.Errorf("ERROR: Failed to disable recoveries globally: %v\n", err)
			}
			fmt.Println("OK: Orchestrator recoveries DISABLED globally")
		}
	case "enable-global-recoveries":
		{
			if err := logic.EnableRecovery(); err != nil {
				return fmt.Errorf("ERROR: Failed to enable recoveries globally: %v\n", err)
			}
			fmt.Println("OK: Orchestrator recoveries ENABLED globally")
		}
	case "check-global-recoveries":
		{
			isDisabled, err := logic.IsRecoveryDisabled()
			if err != nil {
				return fmt.Errorf("ERROR: Failed to determine if recoveries are disabled globally: %v\n", err)
			}
			fmt.Printf("OK: Global recoveries disabled: %v\n", isDisabled)
		}
	case "bulk-instances":
		{
			instances, err := inst.BulkReadInstance()
			if err != nil {
				return fmt.Errorf("Error: Failed to retrieve instances: %v\n", err)
			}
			var asciiInstances stringSlice
			for _, v := range instances {
				asciiInstances = append(asciiInstances, v.String())
			}
			sort.Sort(asciiInstances)
			fmt.Printf("%s\n", strings.Join(asciiInstances, "\n"))
		}
	case "bulk-promotion-rules":
		{
			promotionRules, err := inst.BulkReadCandidateDatabaseInstance()
			if err != nil {
				return fmt.Errorf("Error: Failed to retrieve promotion rules: %v\n", err)
			}
			var asciiPromotionRules stringSlice
			for _, v := range promotionRules {
				asciiPromotionRules = append(asciiPromotionRules, v.String())
			}
			sort.Sort(asciiPromotionRules)

			fmt.Printf("%s\n", strings.Join(asciiPromotionRules, "\n"))
		}
	default:
		return fmt.Errorf("unknown command %q", command)
	}
	return nil
}
