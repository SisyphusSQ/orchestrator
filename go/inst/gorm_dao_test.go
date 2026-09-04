package inst

import (
	"database/sql"
	"testing"
	"time"
)

func TestReadInstanceRowMapsTypedBackendDTO(t *testing.T) {
	row := instanceBackendRow{
		Hostname:                      "db.example",
		Port:                          3307,
		Version:                       "8.0.42",
		MasterHost:                    "primary.example",
		MasterPort:                    3306,
		SlaveSQLRunning:               true,
		SlaveIORunning:                true,
		SecondsBehindMaster:           sql.NullInt64{Int64: 7, Valid: true},
		SlaveLagSeconds:               sql.NullInt64{Int64: 8, Valid: true},
		SlaveHosts:                    `["replica.example:3306"]`,
		ClusterName:                   "cluster-a",
		SecondsSinceLastChecked:       sql.NullInt64{Int64: 0, Valid: true},
		LastSeen:                      sql.NullString{String: "2026-09-03 12:00:00", Valid: true},
		LastCheckValid:                true,
		SecondsSinceLastSeen:          sql.NullInt64{Int64: 3, Valid: true},
		PromotionRule:                 string(PreferPromoteRule),
		ElapsedDowntimeSeconds:        4,
		LastDiscoveryLatency:          int64(2 * time.Millisecond),
		ReplicationGroupName:          "group-a",
		ReplicationGroupSinglePrimary: true,
		ReplicationGroupMemberState:   GroupReplicationMemberStateOnline,
		ReplicationGroupMemberRole:    GroupReplicationMemberRoleSecondary,
		ReplicationGroupPrimaryHost:   "group-primary.example",
		ReplicationGroupPrimaryPort:   3306,
		ReplicationGroupMembers:       `[]`,
	}

	instance := readInstanceRow(row)
	if instance.Key.Hostname != row.Hostname || instance.Key.Port != row.Port {
		t.Fatalf("instance key = %+v; want %s:%d", instance.Key, row.Hostname, row.Port)
	}
	if !instance.IsUpToDate || !instance.IsRecentlyChecked {
		t.Fatalf("freshness = up-to-date:%v recent:%v; want both true", instance.IsUpToDate, instance.IsRecentlyChecked)
	}
	if instance.ReplicationLagSeconds != row.SlaveLagSeconds || instance.SecondsSinceLastSeen != row.SecondsSinceLastSeen {
		t.Fatalf("nullable lag fields were not preserved: %+v", instance)
	}
	if instance.LastSeenTimestamp != row.LastSeen.String {
		t.Fatalf("LastSeenTimestamp = %q; want %q", instance.LastSeenTimestamp, row.LastSeen.String)
	}
	if instance.LastDiscoveryLatency != 2*time.Millisecond || instance.ElapsedDowntime != 4*time.Second {
		t.Fatalf("durations = %s/%s; want 2ms/4s", instance.LastDiscoveryLatency, instance.ElapsedDowntime)
	}
	if instance.ReplicationGroupPrimaryInstanceKey.Hostname != row.ReplicationGroupPrimaryHost {
		t.Fatalf("group primary = %+v; want host %q", instance.ReplicationGroupPrimaryInstanceKey, row.ReplicationGroupPrimaryHost)
	}
}

func TestReadInstanceRowPreservesLegacyNullFreshnessSemantics(t *testing.T) {
	instance := readInstanceRow(instanceBackendRow{})
	if !instance.IsUpToDate || !instance.IsRecentlyChecked {
		t.Fatalf("NULL seconds_since_last_checked changed legacy zero-value freshness semantics")
	}
	if instance.SecondsBehindMaster.Valid || instance.ReplicationLagSeconds.Valid || instance.SecondsSinceLastSeen.Valid {
		t.Fatalf("NULL backend values became valid: %+v", instance)
	}
}
