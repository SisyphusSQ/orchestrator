package topology

import (
	"testing"

	instmodel "github.com/openark/orchestrator/internal/inst/instance"
)

func TestInstancesAreSiblings(t *testing.T) {
	masterKey := instmodel.InstanceKey{Hostname: "master", Port: 3306}
	first := &instmodel.Instance{
		Key:                   instmodel.InstanceKey{Hostname: "first", Port: 3306},
		MasterKey:             masterKey,
		ReadBinlogCoordinates: instmodel.BinlogCoordinates{LogFile: "mysql-bin.000001"},
	}
	second := &instmodel.Instance{
		Key:                   instmodel.InstanceKey{Hostname: "second", Port: 3306},
		MasterKey:             masterKey,
		ReadBinlogCoordinates: instmodel.BinlogCoordinates{LogFile: "mysql-bin.000001"},
	}

	if !InstancesAreSiblings(first, second) {
		t.Fatal("replicas of the same source should be siblings")
	}
	if InstancesAreSiblings(first, first) {
		t.Fatal("an instance must not be its own sibling")
	}
	if InstancesAreSiblings(first, &instmodel.Instance{Key: masterKey}) {
		t.Fatal("a non-replica must not be considered a sibling")
	}
}

func TestInstanceIsMasterOf(t *testing.T) {
	master := &instmodel.Instance{Key: instmodel.InstanceKey{Hostname: "master", Port: 3306}}
	replica := &instmodel.Instance{
		Key:                   instmodel.InstanceKey{Hostname: "replica", Port: 3306},
		MasterKey:             master.Key,
		ReadBinlogCoordinates: instmodel.BinlogCoordinates{LogFile: "mysql-bin.000001"},
	}

	if !InstanceIsMasterOf(master, replica) {
		t.Fatal("replica source should be recognized as its master")
	}
	if InstanceIsMasterOf(master, master) {
		t.Fatal("an instance must not be its own master")
	}
	if InstanceIsMasterOf(master, &instmodel.Instance{Key: replica.Key}) {
		t.Fatal("an instance without a source must not be considered a replica")
	}
}
