export interface InstanceKey {
  Hostname: string;
  Port: number;
}
export interface NullableNumber {
  Int64: number;
  Valid: boolean;
}
export interface Coordinates {
  LogFile: string;
  LogPos: number;
  Type?: number;
}
export interface Instance {
  Key: InstanceKey;
  MasterKey: InstanceKey;
  ClusterName: string;
  InstanceAlias?: string;
  Version: string;
  ReadOnly: boolean;
  IsCoMaster: boolean;
  IsLastCheckValid: boolean;
  IsRecentlyChecked: boolean;
  IsUpToDate: boolean;
  ReplicationDepth: number;
  ReplicationSQLThreadRuning: boolean;
  ReplicationIOThreadRuning: boolean;
  ReplicationLagSeconds: NullableNumber;
  SecondsBehindMaster: NullableNumber;
  SecondsSinceLastSeen: NullableNumber;
  SQLDelay: number;
  Uptime: number;
  DataCenter: string;
  PhysicalEnvironment: string;
  LogBinEnabled: boolean;
  LogReplicationUpdatesEnabled: boolean;
  Binlog_format: string;
  UsingOracleGTID: boolean;
  UsingMariaDBGTID: boolean;
  UsingPseudoGTID: boolean;
  SupportsOracleGTID: boolean;
  GtidErrant: string;
  ExecutedGtidSet: string;
  SelfBinlogCoordinates: Coordinates;
  ExecBinlogCoordinates: Coordinates;
  ReadBinlogCoordinates: Coordinates;
  RelaylogCoordinates: Coordinates;
  LastSQLError: string;
  LastIOError: string;
  Replicas?: InstanceKey[];
  IsDowntimed: boolean;
  DowntimeOwner: string;
  DowntimeReason: string;
  DowntimeEndTimestamp: string;
  PromotionRule: string;
  SemiSyncMasterEnabled: boolean;
  SemiSyncReplicaEnabled: boolean;
  Problems?: string[];
}
export interface Cluster {
  ClusterName: string;
  ClusterAlias: string;
  ClusterDomain: string;
  CountInstances: number;
  HasAutomatedMasterRecovery: boolean;
  HasAutomatedIntermediateMasterRecovery: boolean;
}
export interface Analysis {
  Analysis: string;
  AnalyzedInstanceKey: InstanceKey;
  ClusterDetails: Cluster;
  StructureAnalysis?: string[];
  Description?: string;
  IsDowntimed: boolean;
  CountReplicas: number;
  CountValidReplicas: number;
  CountValidReplicatingReplicas: number;
  Replicas?: InstanceKey[];
}
export interface Maintenance {
  MaintenanceId: number;
  Key: InstanceKey;
  Owner: string;
  Reason: string;
  BeginTimestamp: string;
  SecondsElapsed: number;
  IsActive: boolean;
}
export interface Recovery {
  Id: number;
  UID: string;
  AnalysisEntry: Analysis;
  IsActive: boolean;
  IsSuccessful: boolean;
  RecoveryStartTimestamp: string;
  RecoveryEndTimestamp: string;
  ProcessingNodeHostname: string;
  Acknowledged: boolean;
  AcknowledgedBy: string;
  AcknowledgedAt: string;
  AcknowledgedComment: string;
  SuccessorKey: InstanceKey;
  LostReplicas: InstanceKey[];
  ParticipatingInstanceKeys: InstanceKey[];
  AllErrors: string[];
  LastDetectionId: number;
  RelatedRecoveryId?: number;
}
export interface RecoveryStep {
  Id: number;
  RecoveryUID: string;
  AuditAt: string;
  Message: string;
}
export interface Audit {
  AuditId: number;
  AuditTimestamp: string;
  AuditType: string;
  AuditInstanceKey: InstanceKey;
  ClusterName: string;
  Message: string;
}
export interface BlockedRecovery {
  FailedInstanceKey: InstanceKey;
  ClusterName: string;
  Analysis: string;
  BlockingRecoveryId: number;
  LastBlockedTimestamp: string;
}
export interface Seed {
  SeedId: number;
  TargetHostname: string;
  SourceHostname: string;
  IsComplete: boolean;
  IsSuccessful: boolean;
  StartTimestamp: string;
  EndTimestamp: string;
}
export interface SeedState {
  SeedStateId: number;
  StateTimestamp: string;
  Action: string;
  ErrorMessage: string;
}
export interface Agent {
  Hostname: string;
  Port: number;
  MySQLPort: number;
  LastSubmitted: string;
  MySQLRunning: boolean;
  MySQLDiskUsage: number;
  MySQLErrorLogTail: string[];
  AvailableSnapshots: string[];
  AvailableLocalSnapshots: string[];
  LogicalVolumes: {
    Name: string;
    GroupName: string;
    Path: string;
    IsSnapshot: boolean;
    SnapshotPercent: number;
  }[];
  MountPoint: {
    IsMounted: boolean;
    Path: string;
    Device: string;
    DiskUsage: number;
  };
}
export interface WebConfig {
  urlPrefix: string;
  userId: string;
  authorizedForAction: boolean;
  authorizedForConfiguration: boolean;
  agentsEnabled: boolean;
  pseudoGTIDEnabled: boolean;
  removeTextFromHostnameDisplay: string;
  webMessage: string;
  auditPageSize: number;
  auditEnabled: boolean;
}

export interface RecoveryPolicy {
  autoMasterRecovery: boolean;
  autoIntermediateMasterRecovery: boolean;
  recoveryIgnoreHostnameFilters: string[];
  promotionIgnoreHostnameFilters: string[];
  problemIgnoreHostnameFilters: string[];
  failureDetectionPeriodBlockMinutes: number;
  recoveryPeriodBlockSeconds: number;
  reasonableReplicationLagSeconds: number;
  reasonableMaintenanceReplicationLagSeconds: number;
  verifyReplicationFilters: boolean;
  failMasterPromotionOnLagMinutes: number;
  sqlThreadPromotionPolicy: "allow" | "wait" | "reject";
  recoverNonWriteableMaster: boolean;
  coMasterRecoveryMustPromoteOtherCoMaster: boolean;
  detachLostReplicasAfterMasterFailover: boolean;
  applyMySQLPromotionAfterMasterFailover: boolean;
  preventCrossDataCenterMasterFailover: boolean;
  preventCrossRegionMasterFailover: boolean;
  masterFailoverDetachReplicaMasterHost: boolean;
  postponeReplicaRecoveryOnLagMinutes: number;
  enforceExactSemiSyncReplicas: boolean;
  recoverLockedSemiSyncMaster: boolean;
  reasonableLockedSemiSyncMasterSeconds: number;
}
export interface RecoveryPolicyDocument {
  scopeType: "global" | "cluster";
  scopeKey: string;
  revision: number;
  overrides: Partial<RecoveryPolicy>;
  inherited: RecoveryPolicy;
  effective: RecoveryPolicy;
  updatedBy?: string;
  changeReason?: string;
  updatedAt?: string;
}
export interface HookProfile {
  id: string;
  name: string;
  commands: string[];
  timeoutSeconds: number;
  failurePolicy: "abort" | "continue";
  outputLimitBytes: number;
  enabled: boolean;
  revision: number;
  updatedBy?: string;
  changeReason?: string;
}
export interface HookAssignment {
  scopeType: "global" | "cluster";
  scopeKey: string;
  phase: string;
  mode: "inherit" | "replace" | "disable";
  profileIds: string[];
  revision: number;
  changeReason?: string;
}
export interface Envelope<T> {
  Code: "OK" | "ERROR";
  Message: string;
  Details: T;
  ErrorClass?: string;
}
