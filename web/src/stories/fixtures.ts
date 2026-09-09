import { http, HttpResponse } from "msw";
import type { Cluster, Instance, Maintenance, WebConfig } from "../api/types";

export const webConfig: WebConfig = {
  urlPrefix: "",
  userId: "storybook",
  authorizedForAction: true,
  authorizedForConfiguration: true,
  agentsEnabled: false,
  pseudoGTIDEnabled: true,
  removeTextFromHostnameDisplay: ".example.test",
  webMessage: "",
  auditPageSize: 20,
  auditEnabled: true,
};
export const primary: Instance = {
  Key: { Hostname: "orders-primary.example.test", Port: 3306 },
  MasterKey: { Hostname: "", Port: 0 },
  ClusterName: "orders-primary.example.test:3306",
  InstanceAlias: "订单主库",
  Version: "8.0.46",
  ReadOnly: false,
  IsCoMaster: false,
  IsLastCheckValid: true,
  IsRecentlyChecked: true,
  IsUpToDate: true,
  ReplicationDepth: 0,
  ReplicationSQLThreadRuning: false,
  ReplicationIOThreadRuning: false,
  ReplicationLagSeconds: { Int64: 0, Valid: true },
  SecondsBehindMaster: { Int64: 0, Valid: true },
  SecondsSinceLastSeen: { Int64: 0, Valid: true },
  SQLDelay: 0,
  Uptime: 864000,
  DataCenter: "北京 · AZ1",
  PhysicalEnvironment: "生产",
  LogBinEnabled: true,
  LogReplicationUpdatesEnabled: true,
  Binlog_format: "ROW",
  UsingOracleGTID: false,
  UsingMariaDBGTID: false,
  UsingPseudoGTID: false,
  SupportsOracleGTID: true,
  GtidErrant: "",
  ExecutedGtidSet: "00000000-0000-0000-0000-000000000001:1-2400",
  SelfBinlogCoordinates: { LogFile: "mysql-bin.000024", LogPos: 18200 },
  ExecBinlogCoordinates: { LogFile: "mysql-bin.000024", LogPos: 18200 },
  ReadBinlogCoordinates: { LogFile: "mysql-bin.000024", LogPos: 18200 },
  RelaylogCoordinates: { LogFile: "relay-bin.000003", LogPos: 850 },
  LastSQLError: "",
  LastIOError: "",
  IsDowntimed: false,
  DowntimeOwner: "",
  DowntimeReason: "",
  DowntimeEndTimestamp: "",
  PromotionRule: "neutral",
  SemiSyncMasterEnabled: true,
  SemiSyncReplicaEnabled: false,
};
export const replica: Instance = {
  ...primary,
  Key: { Hostname: "orders-replica-01.example.test", Port: 3306 },
  MasterKey: primary.Key,
  InstanceAlias: "订单副本 01",
  ReadOnly: true,
  ReplicationDepth: 1,
  ReplicationSQLThreadRuning: true,
  ReplicationIOThreadRuning: true,
  UsingOracleGTID: true,
  SemiSyncMasterEnabled: false,
  SemiSyncReplicaEnabled: true,
};
export const replica2: Instance = {
  ...replica,
  Key: { Hostname: "orders-replica-02.example.test", Port: 3306 },
  InstanceAlias: "订单副本 02",
  DataCenter: "北京 · AZ2",
};
export const lagging: Instance = {
  ...replica,
  ReplicationLagSeconds: { Int64: 126, Valid: true },
};
export const stopped: Instance = {
  ...replica2,
  ReplicationSQLThreadRuning: false,
  LastSQLError: "Duplicate entry '2401' for key 'orders.PRIMARY'",
  ReplicationLagSeconds: { Int64: 0, Valid: false },
};
export const healthy = [primary, replica, replica2];
export const problems = [primary, lagging, stopped];
export const cluster: Cluster = {
  ClusterName: primary.ClusterName,
  ClusterAlias: "订单数据库",
  ClusterDomain: "orders.db.example.test",
  CountInstances: 3,
  HasAutomatedMasterRecovery: true,
  HasAutomatedIntermediateMasterRecovery: false,
};
export const maintenance: Maintenance = {
  MaintenanceId: 427,
  Key: replica2.Key,
  Owner: "数据库值班",
  Reason: "副本版本升级",
  BeginTimestamp: "2026-09-08 14:00:00",
  SecondsElapsed: 300,
  IsActive: true,
};

// 固定保留兜底处理器：未声明的操作只返回模拟错误，不接触真实服务。
export const apiHandlers = [
  http.get("/api/*", ({ request }) => {
    const path = decodeURIComponent(new URL(request.url).pathname.slice(4));
    if (path === "/web-config") return HttpResponse.json(webConfig);
    if (path === "/check-global-recoveries")
      return HttpResponse.json({ Code: "OK", Details: "enabled" });
    if (path === "/clusters-info") return HttpResponse.json([cluster]);
    if (path.startsWith("/cluster-info/")) return HttpResponse.json(cluster);
    if (path.startsWith("/cluster/")) return HttpResponse.json(healthy);
    if (path.startsWith("/instance/"))
      return HttpResponse.json(
        healthy.find(
          (item) => path === `/instance/${item.Key.Hostname}/${item.Key.Port}`,
        ) || replica,
      );
    if (path.startsWith("/tags/"))
      return HttpResponse.json(["业务=订单", "owner=数据库团队"]);
    if (path.startsWith("/cluster-pool-instances/"))
      return HttpResponse.json({ 只读流量: [replica2.Key] });
    if (
      [
        "/replication-analysis",
        "/problems",
        "/maintenance",
        "/blocked-recoveries",
      ].includes(path) ||
      /^\/(replication-analysis\/|blocked-recoveries\/|recently-active-|active-cluster-recovery|master-equivalent|heuristic-cluster-pool-instances)/.test(
        path,
      )
    )
      return HttpResponse.json([]);
    return HttpResponse.json(
      { Code: "ERROR", Message: `此示例未配置读取：${path}` },
      { status: 501 },
    );
  }),
  http.all("/api/*", () =>
    HttpResponse.json(
      { Code: "ERROR", Message: "此示例未配置该操作；未向后端发送请求。" },
      { status: 501 },
    ),
  ),
];
