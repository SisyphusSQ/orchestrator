# Configuration samples

Use the checked-in [MySQL backend sample](../conf/orchestrator-sample.conf.json) or [SQLite backend sample](../conf/orchestrator-sample-sqlite.conf.json) as the baseline for the current revision. The following redacted example highlights the required Raft identity and current service/observability fields:

```json
{
  "RaftNodeID": "node-1",
  "RaftDataDir": "/var/lib/orchestrator/raft",
  "RaftBind": "10.0.0.1:10008",
  "RaftAdvertise": "10.0.0.1:10008",
  "ListenAddress": ":3000",
  "BackendDB": "mysql",
  "MySQLOrchestratorHost": "127.0.0.1",
  "MySQLOrchestratorPort": 3306,
  "MySQLOrchestratorDatabase": "orchestrator",
  "MySQLOrchestratorCredentialsConfigFile": "/etc/mysql/orchestrator-backend.cnf",
  "MySQLTopologyCredentialsConfigFile": "/etc/mysql/orchestrator-topology.cnf",
  "InstancePollSeconds": 5,
  "RecoverMasterClusterFilters": ["production-*"],
  "RecoverIntermediateMasterClusterFilters": ["production-*"],
  "AuthenticationMethod": "proxy",
  "AuthUserHeader": "X-Authenticated-User",
  "PowerAuthUsers": ["dba-oncall"],
  "AuditToBackendDB": true,
  "OTelTraceEndpoint": "https://collector.example.com/v1/traces",
  "OTelTraceSampleRatio": 0.1
}
```

Do not copy placeholder hosts, users, filters, or credentials into production. Store credential files with service-account-only permissions and validate the effective configuration from the real runtime namespace.

Removed fields such as `RaftEnabled`, `ZkAddress`, Graphite settings, and historical raw-metric retention settings are rejected. Previously documented no-op fields including `BufferBinlogEvents`, `BinlogFileHistoryDays`, `MaintenanceOwner`, `ReadLongRunningQueries`, `ActiveNodeExpireSeconds`, `AuditPageSize`, `SlaveStartPostWaitMilliseconds`, `MySQLTopologyMaxPoolConnections`, `MaintenancePurgeDays`, `MaintenanceExpireMinutes`, and `HttpTimeoutSeconds` are intentionally absent.

The code definition in [`internal/config/config.go`](../internal/config/config.go) is authoritative. See [configuration topics](configuration.md) and [upgrading](upgrading.md) for policy and migration details.
