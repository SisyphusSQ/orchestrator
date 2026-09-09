# Configuration

**English** · [中文](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Configuration) · [Wiki home](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

The server accepts JSON or YAML from an explicit `--config` path; the filename extension does not control parsing. Without `--config`, it checks `/etc/orchestrator.conf`, `conf/orchestrator.conf`, then `orchestrator.conf`, accepting one of `.yaml`, `.yml`, or `.json` at each location. Files found at later locations override earlier ones. More than one format at the same location is an error. Configuration must use the lowerCamel layered structure grouped by responsibility. Legacy flat fields, wrong casing, unknown fields, duplicate keys, multiple YAML documents, and trailing content are rejected. `dump-config` emits the same layered structure as JSON. Configuration contains database and authentication secrets; keep it outside public artifacts and restrict filesystem access.

## Required server identity

Every server start requires:

```yaml
raft:
  nodeID: node-1
  dataDir: /var/lib/orchestrator/raft
  bind: 10.0.0.1:10008
  advertise: 10.0.0.1:10008
server:
  listen:
    address: ":3000"
```

`raft.nodeID` is a durable identity, not an address. `raft.advertise` defaults to normalized `raft.bind`; set it explicitly behind NAT. These values, the HTTP listener, backend connection pools, and `observability.tracing` configuration require a restart when changed.

## Metadata backend

Choose one independent backend per Raft node:

- MySQL: configure `metadata.mysql.host`, port, database, user, and password or `metadata.mysql.credentialsConfigFile`.
- SQLite: set `metadata.type` to `sqlite` and provide an absolute writable `metadata.sqlite.dataFile`.

Do not point several Raft nodes at one shared metadata database. Backup both the metadata backend and the Raft data directory according to their distinct consistency requirements.

## Topology access and behavior

Configure `topology.mysql.user` and its password or credentials file on every node. The account must read replication state on every discovered instance and needs additional privileges for requested topology changes. Discovery seeds, hostname resolution, instance filters, promotion rules, recovery filters, hooks, audit sinks, Consul, authentication, TLS, and URL prefixes are independent policy choices.

Use the checked-in MySQL and SQLite samples under [`conf/`](https://github.com/SisyphusSQ/orchestrator/tree/main/conf). The complete field definition is in [`internal/config/model.go`](https://github.com/SisyphusSQ/orchestrator/blob/main/internal/config/model.go); defaults and validation remain in [`internal/config/config.go`](https://github.com/SisyphusSQ/orchestrator/blob/main/internal/config/config.go).

## Backend lifecycle and schema

MySQL-compatible backends include MySQL 5.7–8.0, TiDB, and OceanBase MySQL mode. Credentials may be supplied directly or through `metadata.mysql.credentialsConfigFile`; restrict both files because environment expansion can still materialize secrets in process memory. `metadata.mysql.maxAllowedPacket` and `topology.mysql.maxAllowedPacket` apply independently to backend and managed-instance connections.

The process owns one backend pool and separate topology discovery and operation pools. Changes to endpoints, credentials, TLS, timeouts, packet limits, lifetimes, or pool sizes require restart; reload does not rebuild open pools. SQLite uses one process-owned pool and requires an absolute writable file path.

Empty metadata databases are initialized from the executable [`docs/schema/mysql.sql`](https://github.com/SisyphusSQ/orchestrator/blob/main/docs/schema/mysql.sql) contract. Existing databases continue through the ordered compatibility patch stream. GORM reuses the process-owned pool and does not own schema migration. Read the [`migration guide`](https://github.com/SisyphusSQ/orchestrator/blob/main/docs/schema/migration-guide.md) before replacing a binary or importing DDL.

## Discovery, classification, and filters

- Discovery interval, concurrency, instance expiry, hostname resolution, and seed selection determine what topology state is considered current. Keep DNS and `report_host` behavior stable before enabling recovery.
- `topology.discovery.ignoreReplicaHostnames` and `topology.discovery.ignoreMasterHostnames` exclude matching instances from specific discovery paths. Filters are policy, not connectivity diagnostics; validate them against representative hostnames.
- Cluster aliases, domains, data centers, regions, environment labels, promotion rules, lag limits, and semi-sync state affect candidate classification. Populate them consistently before relying on automated recovery.
- GTID is preferred when topology compatibility permits. Pseudo-GTID requires deliberate injection, retention, and privileges on every relevant writable primary; missing markers reduce relocation and recovery options.

## Recovery, hooks, KV, and logging

Recovery is controlled by the intersection of global controls, cluster filters, ignored-host filters, candidate eligibility, and Raft leader/quorum state. Configure hooks with bounded runtime and explicit failure handling. Maintenance, downtime, audit, and recovery records require deliberate retention policies.

Consul KV publishing remains supported through the official SDK. Configure `consul.address`; HTTPS verifies certificates by default and may use CA, server-name, and paired client certificate settings under `consul.tls`. `consul.tls.skipVerify` is only a temporary compatibility escape hatch. A timed-out or partially successful cross-datacenter write is not automatically retried or rolled back. Built-in ZooKeeper publishing is removed.

Application logs are text on stderr in `time<TAB>[LEVEL]<TAB>[caller]<TAB>message` format. `logging.syslog.enabled` and `audit.toSyslog` startup failures are fatal; audit file/syslog write failures remain visible. Validate parser and sink latency from the real service sandbox.

## Removed settings

Legacy flat configuration is not accepted. For example, migrate `RaftNodeID`, `BackendDB`, `MySQLTopologyUser`, and `ConsulAddress` to `raft.nodeID`, `metadata.type`, `topology.mysql.user`, and `consul.address`. Strict parsing rejects every unmigrated field. Removed settings are rejected as well; replace `SlaveLagQuery` with `topology.replication.lagQuery` and migrate legacy recovery-policy fields according to the recovery configuration guide.

`OAuthClientId`, `OAuthClientSecret`, `OAuthScopes`, `ExpectFailureAnalysisConcensus`, `SeedAcceptableBytesDiff`, and `MasterFailoverLostInstancesDowntimeMinutes` have no replacement and must be deleted. OAuth authentication is not supported; `authentication.method` accepts only `basic`, `multi`, `proxy`, `token`, or an empty value. Also remove `RaftEnabled`, `ZkAddress`, Graphite settings, legacy in-memory metric retention settings, and other fields absent from the current `Configuration` definition.
