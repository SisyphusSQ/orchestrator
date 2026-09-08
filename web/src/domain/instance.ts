import type { Instance, InstanceKey, NullableNumber } from "../api/types";

export const instanceID = (key?: InstanceKey | null) =>
  key?.Hostname ? `${key.Hostname}:${key.Port}` : "—";
export const lagValue = (value?: NullableNumber | number) =>
  typeof value === "number" ? value : value?.Valid ? value.Int64 : null;
export const lagText = (instance: Instance) => {
  if (!isReplica(instance)) return "—";
  const lag = lagValue(instance.ReplicationLagSeconds);
  return lag === null ? "未知" : `${lag} 秒`;
};
export const isReplica = (instance: Instance) =>
  !!instance.MasterKey?.Hostname &&
  instance.MasterKey.Hostname !== "_" &&
  instance.MasterKey.Port > 0;
export function instanceState(instance: Instance): {
  label: string;
  color: string;
  severity: number;
} {
  if (!instance.IsLastCheckValid)
    return { label: "连接异常", color: "error", severity: 3 };
  if (!instance.IsRecentlyChecked)
    return { label: "信息过期", color: "warning", severity: 2 };
  if (
    isReplica(instance) &&
    (!instance.ReplicationSQLThreadRuning ||
      !instance.ReplicationIOThreadRuning)
  )
    return { label: "复制停止", color: "error", severity: 3 };
  const lag = lagValue(instance.ReplicationLagSeconds);
  if (isReplica(instance) && lag === null)
    return { label: "延迟未知", color: "warning", severity: 2 };
  if (
    isReplica(instance) &&
    lag !== null &&
    lag > Math.max(10, instance.SQLDelay || 0)
  )
    return { label: "复制延迟", color: "warning", severity: 2 };
  if (instance.IsDowntimed)
    return { label: "停机维护", color: "default", severity: 1 };
  return { label: "正常", color: "success", severity: 0 };
}
export const role = (instance: Instance) =>
  instance.IsCoMaster ? "双主" : isReplica(instance) ? "副本" : "主库";
export const replicationMode = (instance: Instance) =>
  !isReplica(instance) && instance.SupportsOracleGTID
    ? "GTID 可用"
    : instance.UsingOracleGTID || instance.UsingMariaDBGTID
      ? "GTID"
      : instance.UsingPseudoGTID
        ? "Pseudo-GTID"
        : "File / Position";
