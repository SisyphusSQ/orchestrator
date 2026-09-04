package logic

import (
	"database/sql"
	"testing"

	"github.com/openark/orchestrator/go/inst"
)

func TestTopologyRecoveryFromTypedRowPreservesNullableFields(t *testing.T) {
	row := topologyRecoveryRow{
		RecoveryID:            42,
		UID:                   "recovery-42",
		Hostname:              "failed.example",
		Port:                  3306,
		Active:                true,
		StartActivePeriod:     "2026-09-03 12:00:00",
		Analysis:              string(inst.DeadMaster),
		ClusterName:           "cluster-a",
		ClusterAlias:          "payments",
		CountAffectedReplicas: 2,
		ReplicaHosts:          "replica-a:3306,replica-b:3306",
		SuccessorHostname:     "successor.example",
		SuccessorPort:         3307,
		AllErrors:             sql.NullString{},
		LostReplicas:          sql.NullString{},
		ParticipatingInstances: sql.NullString{
			String: "successor.example:3307",
			Valid:  true,
		},
		AcknowledgedAt:      sql.NullString{},
		AcknowledgedBy:      sql.NullString{},
		AcknowledgedComment: sql.NullString{},
		LastDetectionID:     9,
	}

	recovery := topologyRecoveryFromRow(row)
	if recovery.Id != row.RecoveryID || recovery.UID != row.UID {
		t.Fatalf("recovery identity = %d/%q; want %d/%q", recovery.Id, recovery.UID, row.RecoveryID, row.UID)
	}
	if recovery.AnalysisEntry.AnalyzedInstanceKey.Hostname != row.Hostname || recovery.AnalysisEntry.Analysis != inst.DeadMaster {
		t.Fatalf("analysis entry = %+v; want failed host and DeadMaster", recovery.AnalysisEntry)
	}
	if recovery.SuccessorKey == nil || recovery.SuccessorKey.Hostname != row.SuccessorHostname {
		t.Fatalf("successor = %+v; want %q", recovery.SuccessorKey, row.SuccessorHostname)
	}
	if recovery.AcknowledgedAt != "" || recovery.AcknowledgedBy != "" || recovery.AcknowledgedComment != "" {
		t.Fatalf("NULL acknowledgement fields were not preserved as empty strings: %+v", recovery)
	}
	if !recovery.ParticipatingInstanceKeys.HasKey(inst.InstanceKey{Hostname: "successor.example", Port: 3307}) {
		t.Fatalf("participating instances = %+v; want successor", recovery.ParticipatingInstanceKeys)
	}
}

func TestFailureAndBlockedRecoveryRowsHandleMissingRelatedIDs(t *testing.T) {
	detection := failureDetectionFromRow(failureDetectionRow{
		DetectionID:       7,
		Hostname:          "failed.example",
		Port:              3306,
		RelatedRecoveryID: sql.NullInt64{},
	})
	if detection.RelatedRecoveryId != 0 {
		t.Fatalf("RelatedRecoveryId = %d; want zero for NULL", detection.RelatedRecoveryId)
	}

	blocked := blockedRecoveryFromRow(blockedRecoveryRow{
		Hostname:           "failed.example",
		Port:               3306,
		BlockingRecoveryID: sql.NullInt64{},
	})
	if blocked.BlockingRecoveryId != 0 {
		t.Fatalf("BlockingRecoveryId = %d; want zero for NULL", blocked.BlockingRecoveryId)
	}
}
