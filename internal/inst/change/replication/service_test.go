package replication

import (
	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/golib/log"
	test "github.com/openark/orchestrator/internal/golib/tests"
	instmodel "github.com/openark/orchestrator/internal/inst/instance"
	"testing"
)

func init() {
	config.Config.Topology.Hostname.ResolveMethod = "none"
	config.MarkConfigurationLoaded()
	log.SetLevel(log.ERROR)
}

func newTestReplica(key string, masterKey string, lastCheckValid bool, semiSyncPriority uint, promotionRule instmodel.CandidatePromotionRule, replicationState instmodel.ReplicationThreadState) *instmodel.Instance {
	return &instmodel.Instance{
		Key:                       instmodel.InstanceKey{Hostname: key, Port: 3306},
		MasterKey:                 instmodel.InstanceKey{Hostname: masterKey, Port: 3306},
		ReadBinlogCoordinates:     instmodel.BinlogCoordinates{LogFile: "mysql.000001", LogPos: 10},
		ReplicationSQLThreadState: replicationState,
		ReplicationIOThreadState:  replicationState,
		IsLastCheckValid:          lastCheckValid,
		SemiSyncPriority:          semiSyncPriority,
		PromotionRule:             promotionRule,
	}
}

func expectInstancesMatch(t *testing.T, actual []*instmodel.Instance, expected []*instmodel.Instance) {
	if len(expected) != len(actual) {
		t.Fatalf("Actual instance list %+v does not match expected list %+v", actual, expected)
	}
	for i := range actual {
		if actual[i] != expected[i] {
			t.Fatalf("Actual instance %+v does not match expected %+v", actual[i], expected[i])
		}
	}
}

func TestClassifyAndPrioritizeReplicas_NoPrioritiesSamePromotionRule_NameTiebreaker(t *testing.T) {
	replica1 := newTestReplica("replica1", "master1", true, 1, instmodel.NeutralPromoteRule, instmodel.ReplicationThreadStateRunning)
	replica2 := newTestReplica("replica2", "master1", true, 1, instmodel.NeutralPromoteRule, instmodel.ReplicationThreadStateRunning)
	replica3 := newTestReplica("replica3", "master1", true, 1, instmodel.NeutralPromoteRule, instmodel.ReplicationThreadStateRunning)
	replicas := []*instmodel.Instance{replica3, replica2, replica1} // inverse order!

	possibleSemiSyncReplicas, asyncReplicas, excludedReplicas := classifyAndPrioritizeReplicas(replicas, nil)
	expectInstancesMatch(t, possibleSemiSyncReplicas, []*instmodel.Instance{replica1, replica2, replica3})
	expectInstancesMatch(t, asyncReplicas, []*instmodel.Instance{})
	expectInstancesMatch(t, excludedReplicas, []*instmodel.Instance{})
}

func TestClassifyAndPrioritizeReplicas_WithPriorities(t *testing.T) {
	replica1 := newTestReplica("replica1", "master1", true, 1, instmodel.NeutralPromoteRule, instmodel.ReplicationThreadStateRunning)
	replica2 := newTestReplica("replica2", "master1", true, 3, instmodel.NeutralPromoteRule, instmodel.ReplicationThreadStateRunning)
	replica3 := newTestReplica("replica3", "master1", true, 2, instmodel.NeutralPromoteRule, instmodel.ReplicationThreadStateRunning)
	replicas := []*instmodel.Instance{replica1, replica2, replica3}

	possibleSemiSyncReplicas, asyncReplicas, excludedReplicas := classifyAndPrioritizeReplicas(replicas, nil)
	expectInstancesMatch(t, possibleSemiSyncReplicas, []*instmodel.Instance{replica2, replica3, replica1})
	expectInstancesMatch(t, asyncReplicas, []*instmodel.Instance{})
	expectInstancesMatch(t, excludedReplicas, []*instmodel.Instance{})
}

func TestClassifyAndPrioritizeReplicas_WithPrioritiesAndPromotionRules_PriorityTakesPrecedence(t *testing.T) {
	replica1 := newTestReplica("replica1", "master1", true, 1, instmodel.PreferPromoteRule, instmodel.ReplicationThreadStateRunning)
	replica2 := newTestReplica("replica2", "master1", true, 3, instmodel.MustNotPromoteRule, instmodel.ReplicationThreadStateRunning)
	replica3 := newTestReplica("replica3", "master1", true, 2, instmodel.NeutralPromoteRule, instmodel.ReplicationThreadStateRunning)
	replicas := []*instmodel.Instance{replica1, replica2, replica3}

	possibleSemiSyncReplicas, asyncReplicas, excludedReplicas := classifyAndPrioritizeReplicas(replicas, nil)
	expectInstancesMatch(t, possibleSemiSyncReplicas, []*instmodel.Instance{replica2, replica3, replica1})
	expectInstancesMatch(t, asyncReplicas, []*instmodel.Instance{})
	expectInstancesMatch(t, excludedReplicas, []*instmodel.Instance{})
}

func TestClassifyAndPrioritizeReplicas_LastCheckInvalidAndNotReplication(t *testing.T) {
	replica1 := newTestReplica("replica1", "master1", true, 1, instmodel.MustNotPromoteRule, instmodel.ReplicationThreadStateRunning)
	replica2 := newTestReplica("replica2", "master1", true, 1, instmodel.NeutralPromoteRule, instmodel.ReplicationThreadStateStopped)
	replica3 := newTestReplica("replica3", "master1", false, 1, instmodel.PreferPromoteRule, instmodel.ReplicationThreadStateRunning)
	replicas := []*instmodel.Instance{replica1, replica2, replica3}

	possibleSemiSyncReplicas, asyncReplicas, excludedReplicas := classifyAndPrioritizeReplicas(replicas, nil)
	expectInstancesMatch(t, possibleSemiSyncReplicas, []*instmodel.Instance{replica1})
	expectInstancesMatch(t, asyncReplicas, []*instmodel.Instance{})
	expectInstancesMatch(t, excludedReplicas, []*instmodel.Instance{replica2, replica3})
}

func TestClassifyAndPrioritizeReplicas_NonReplicatingReplica(t *testing.T) {
	replica1 := newTestReplica("replica1", "master1", true, 1, instmodel.NeutralPromoteRule, instmodel.ReplicationThreadStateRunning)
	replica2 := newTestReplica("replica2", "master1", true, 1, instmodel.NeutralPromoteRule, instmodel.ReplicationThreadStateRunning)
	replica3 := newTestReplica("replica3", "master1", true, 1, instmodel.MustNotPromoteRule, instmodel.ReplicationThreadStateStopped)
	replicas := []*instmodel.Instance{replica1, replica2, replica3}

	possibleSemiSyncReplicas, asyncReplicas, excludedReplicas := classifyAndPrioritizeReplicas(replicas, &replica3.Key) // Non-replicating instance
	expectInstancesMatch(t, possibleSemiSyncReplicas, []*instmodel.Instance{replica1, replica2, replica3})
	expectInstancesMatch(t, asyncReplicas, []*instmodel.Instance{})
	expectInstancesMatch(t, excludedReplicas, []*instmodel.Instance{})
}

func TestDetermineSemiSyncReplicaActionsForExactTopology_EnableSomeDisableSome(t *testing.T) {
	master := &instmodel.Instance{SemiSyncMasterWaitForReplicaCount: 2}
	replica1 := &instmodel.Instance{SemiSyncReplicaEnabled: true}
	replica2 := &instmodel.Instance{SemiSyncReplicaEnabled: false}
	replica3 := &instmodel.Instance{SemiSyncReplicaEnabled: true}
	replica4 := &instmodel.Instance{SemiSyncReplicaEnabled: false}
	replica5 := &instmodel.Instance{SemiSyncReplicaEnabled: true}
	possibleSemiSyncReplicas := []*instmodel.Instance{replica1, replica2, replica3, replica4}
	asyncReplicas := []*instmodel.Instance{replica5}
	actions := determineSemiSyncReplicaActionsForExactTopology(master, possibleSemiSyncReplicas, asyncReplicas)
	test.S(t).ExpectTrue(len(actions) == 3)
	test.S(t).ExpectTrue(actions[replica2])
	test.S(t).ExpectFalse(actions[replica3])
	test.S(t).ExpectFalse(actions[replica5])
}

func TestDetermineSemiSyncReplicaActionsForExactTopology_NoActions(t *testing.T) {
	master := &instmodel.Instance{SemiSyncMasterWaitForReplicaCount: 1, SemiSyncMasterClients: 1}
	replica1 := &instmodel.Instance{SemiSyncReplicaEnabled: true}
	replica2 := &instmodel.Instance{SemiSyncReplicaEnabled: false}
	replica3 := &instmodel.Instance{SemiSyncReplicaEnabled: false}
	possibleSemiSyncReplicas := []*instmodel.Instance{replica1, replica2, replica3}
	asyncReplicas := []*instmodel.Instance{}
	actions := determineSemiSyncReplicaActionsForExactTopology(master, possibleSemiSyncReplicas, asyncReplicas)
	test.S(t).ExpectTrue(len(actions) == 0)
}

func TestDetermineSemiSyncReplicaActionsForEnoughTopology_MoreThanWaitCountNoActions(t *testing.T) {
	master := &instmodel.Instance{SemiSyncMasterWaitForReplicaCount: 1, SemiSyncMasterClients: 3}
	replica1 := &instmodel.Instance{SemiSyncReplicaEnabled: true}
	replica2 := &instmodel.Instance{SemiSyncReplicaEnabled: true}
	replica3 := &instmodel.Instance{SemiSyncReplicaEnabled: true}
	possibleSemiSyncReplicas := []*instmodel.Instance{replica1, replica2, replica3}
	actions := determineSemiSyncReplicaActionsForEnoughTopology(master, possibleSemiSyncReplicas)
	test.S(t).ExpectTrue(len(actions) == 0)
}

func TestDetermineSemiSyncReplicaActionsForEnoughTopology_LessThanWaitCountEnableOne(t *testing.T) {
	master := &instmodel.Instance{SemiSyncMasterWaitForReplicaCount: 2, SemiSyncMasterClients: 1}
	replica1 := &instmodel.Instance{SemiSyncReplicaEnabled: false}
	replica2 := &instmodel.Instance{SemiSyncReplicaEnabled: true}
	replica3 := &instmodel.Instance{SemiSyncReplicaEnabled: false}
	possibleSemiSyncReplicas := []*instmodel.Instance{replica1, replica2, replica3}
	actions := determineSemiSyncReplicaActionsForEnoughTopology(master, possibleSemiSyncReplicas)
	test.S(t).ExpectTrue(len(actions) == 1)
	test.S(t).ExpectTrue(actions[replica1])
}
