package inst

import "testing"

func TestInstanceWriteRowPreservesDiscoveryFields(t *testing.T) {
	instance := NewInstance()
	instance.Key = InstanceKey{Hostname: "db.example", Port: 3307}
	instance.ServerID = 710
	instance.Version = "8.0.42"
	instance.VersionComment = "MySQL"
	instance.Binlog_format = "ROW"
	instance.BinlogRowImage = "FULL"
	instance.ExecBinlogCoordinates = BinlogCoordinates{LogFile: "mysql.000007", LogPos: 10}
	instance.ReplicationGroupPrimaryInstanceKey = InstanceKey{Hostname: "primary.example", Port: 3306}
	instance.AddReplicaKey(&InstanceKey{Hostname: "replica.example", Port: 3306})

	row := instanceWriteRow(instance)
	if row.Hostname != instance.Key.Hostname || row.Port != instance.Key.Port {
		t.Fatalf("write row key = %s:%d; want %+v", row.Hostname, row.Port, instance.Key)
	}
	if row.MajorVersion != "8.0" || row.NumSlaveHosts != 1 {
		t.Fatalf("write row derived values = major:%q replicas:%d", row.MajorVersion, row.NumSlaveHosts)
	}
	if row.RelayMasterLogFile != instance.ExecBinlogCoordinates.LogFile || row.ExecMasterLogPos != instance.ExecBinlogCoordinates.LogPos {
		t.Fatalf("write row coordinates = %s:%d", row.RelayMasterLogFile, row.ExecMasterLogPos)
	}
	if row.ReplicationGroupPrimaryHost != "primary.example" || row.ReplicationGroupPrimaryPort != 3306 {
		t.Fatalf("write row group primary = %s:%d", row.ReplicationGroupPrimaryHost, row.ReplicationGroupPrimaryPort)
	}
}
