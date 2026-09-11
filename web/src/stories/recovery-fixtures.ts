import { http, HttpResponse } from "msw";
import type { RecoveryPolicy } from "../api/types";

export const recoveryPolicy: RecoveryPolicy = {
  autoMasterRecovery: false,
  autoIntermediateMasterRecovery: false,
  recoveryIgnoreHostnameFilters: [],
  promotionIgnoreHostnameFilters: ["backup-.*"],
  problemIgnoreHostnameFilters: [],
  failureDetectionPeriodBlockMinutes: 60,
  recoveryPeriodBlockSeconds: 3600,
  reasonableReplicationLagSeconds: 10,
  reasonableMaintenanceReplicationLagSeconds: 20,
  verifyReplicationFilters: false,
  failMasterPromotionOnLagMinutes: 0,
  sqlThreadPromotionPolicy: "allow",
  recoverNonWriteableMaster: false,
  coMasterRecoveryMustPromoteOtherCoMaster: true,
  detachLostReplicasAfterMasterFailover: true,
  applyMySQLPromotionAfterMasterFailover: true,
  preventCrossDataCenterMasterFailover: false,
  preventCrossRegionMasterFailover: false,
  masterFailoverDetachReplicaMasterHost: false,
  postponeReplicaRecoveryOnLagMinutes: 0,
  enforceExactSemiSyncReplicas: false,
  recoverLockedSemiSyncMaster: false,
  reasonableLockedSemiSyncMasterSeconds: 0,
};

export const recoverySettingsHandlers = [
  http.get("/api/recovery-policy/global/*", () =>
    HttpResponse.json({
      Code: "OK",
      Message: "Recovery policy",
      Details: {
        scopeType: "global",
        scopeKey: "*",
        revision: 3,
        overrides: recoveryPolicy,
        inherited: recoveryPolicy,
        effective: recoveryPolicy,
        updatedBy: "dba",
        changeReason: "生产基线",
      },
    }),
  ),
  http.get("/api/recovery-hook-profiles", () =>
    HttpResponse.json({
      Code: "OK",
      Message: "Recovery hook profiles",
      Details: [
        {
          id: "notify-dba",
          name: "通知 DBA",
          commands: ["/opt/hooks/notify-dba"],
          timeoutSeconds: 30,
          failurePolicy: "continue",
          outputLimitBytes: 65536,
          enabled: true,
          revision: 2,
        },
      ],
    }),
  ),
  http.get("/api/recovery-hook-assignments/global/*", () =>
    HttpResponse.json({
      Code: "OK",
      Message: "Recovery hook assignments",
      Details: [
        {
          scopeType: "global",
          scopeKey: "*",
          phase: "post_failover",
          mode: "replace",
          profileIds: ["notify-dba"],
          revision: 1,
        },
      ],
    }),
  ),
];
