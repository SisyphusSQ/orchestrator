# Configuration reference
**English** · [中文](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Configuration-Reference) · [Wiki home](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

This is the operator index for [`internal/config/model.go`](https://github.com/SisyphusSQ/orchestrator/blob/main/internal/config/model.go), not a replacement for strict parsing and validation by the same binary version. Defaults come from [`internal/config/config.go`](https://github.com/SisyphusSQ/orchestrator/blob/main/internal/config/config.go); sample files show common combinations rather than every valid field.

## Loading and activation

- Explicit `--config` reads one JSON or YAML document and detects format from content.
- Otherwise locations are `/etc/orchestrator.conf`, `conf/orchestrator.conf`, then `orchestrator.conf`, with `.yaml`, `.yml`, or `.json`; later locations override earlier ones.
- Multiple formats at one location, unknown/legacy/case-mismatched fields, duplicate keys, multiple YAML documents, or trailing content fail.
- Configuration is layered lowerCamelCase. Expanded secrets still enter process memory and `dump-config` output.
- Reload changes only supported runtime values. Listeners, Raft identity/address/storage, database pools, and tracing exporter require restart.

## Top-level domains

| Domain | Children | Main risk | Activation |
| --- | --- | --- | --- |
| `observability` | `tracing.endpoint`, `sampleRatio` | cost and endpoint sensitivity | restart |
| `logging` | `debug`, `syslog.enabled` | sink startup failure | verify restart |
| `server` | listen, advertise, prefix, readOnly, TLS, status, web, identity | exposure and authorization | listener/TLS/prefix restart |
| `raft` | nodeID, bind, advertise, dataDir, defaultPort | durable identity and quorum | restart; never casually change identity |
| `mysql` | connect timeout, lifetime | base pool behavior | pool restart |
| `metadata` | type, sqlite, schema, mysql | metadata consistency/schema | restart; migration is separate |
| `topology` | mysql, replication, discovery, buffer, compatibility, snapshot, hostname, candidate, classification, pools, analysis, operations | discovery and real MySQL writes | pool/identity restart; verify other reloads |
| `authentication` | method, basic, proxy, power, configurationAdmins, accessToken | spoofing and privilege | restart and re-accept |
| `agents` | listener, polling, seed, TLS | extra management listener | listener/TLS restart |
| `pseudoGTID` | marker, query, chunks, skips | relocation capability/cost | verify on every relevant primary |
| `hooks` | `shellCommand` | external process identity | restart and safe test |
| `osc` | hostname ignores | OSC candidate filtering | read after reload |
| `audit` | file, syslog, backend, purge | evidence loss/sensitive output | restart sink verification |
| `consul` | endpoint, ACL, TLS, KV | partial external publication | restart client/TLS changes |

## Code defaults

Unlisted bool/int/string fields use Go zero values, which are not necessarily production-ready.

| Path | Default | Note |
| --- | --- | --- |
| `observability.tracing.sampleRatio` | `0.1` | empty endpoint does not prove export |
| `server.listen.address` | `:3000` | bind explicitly in production |
| `server.status.endpoint` | `/api/status` | status endpoint |
| `raft.bind` / `defaultPort` | `127.0.0.1:10008` / `10008` | replace with peer-reachable values |
| `mysql.connectTimeoutSeconds` | `2` | samples may override code defaults |
| `metadata.type` | `mysql` | pool 128, port 3306, read timeout 30, packet `-1` |
| `topology.mysql.defaultPort` | `3306` | discovery 10s, read 600s, mixed TLS true |
| `topology.discovery.pollSeconds` | `5` | dead max 300s, forget 240h, concurrency 300, queue 100000 |
| `topology.writeBuffer` | size 100, flush 100ms | disabled by default |
| `topology.hostname` | `default`, `@@hostname`, 60m | binlog-server unresolve check skipped |
| `topology.candidate.expireMinutes` | `60` | candidate-registration lifetime |
| `topology.pools.expiryMinutes` | `60` | fuzzy hostnames true |
| `topology.operations` | bulk wait 10s, concurrency 5 | bulk mutation bound |
| `authentication.proxy.userHeader` | `X-Forwarded-User` | trusted proxy only |
| `authentication.power.users` | `[*]` | restrict intentionally with auth |
| `authentication.accessToken` | use 60s, expiry 1440m | token lifecycle |
| `agents` | `:3001`, poll 60m, unseen 6h, stale seed 60m | listener disabled |
| `pseudoGTID.binlogEventsChunkSize` | `10000` | empty marker by default |
| `hooks.shellCommand` | `bash` | profiles are page-managed |
| `audit.purgeDays` | `7` | applies to corresponding retention logic |
| `consul` | `http`, 60s, prefix `mysql/master` | provider consul, 5 KVs per cluster |

## `server`

`listen.address/socket` select TCP or Unix socket. `httpAdvertise` is a peer/client-reachable origin, not the local bind. `urlPrefix` must match proxy, Web, health/metrics, and CLI. `readOnly` guards operations but is not MySQL fencing. TLS fields define server trust; do not normalize `skipVerify` in production. Status, Web display, and response identity affect their named surfaces, not authentication.

## `metadata`

Choose the supported MySQL or SQLite path. SQLite requires an absolute writable `sqlite.dataFile`. MySQL includes host/port/database/user/password/credentials file, key/cert/CA, skip/mutual TLS, read timeout, reject-read-only, packet, and pool fields. Protect direct and file credentials. Schema flags change startup checking but never run `migrate-metadata-id`; migrate explicitly after stopping writers and taking a backup.

## `topology.mysql` and discovery

Topology connection fields include credentials, TLS material/modes/cache, packet, default port, and discovery/read timeouts. Restart pools after changing them.

Discovery fields are `useShowReplicaHosts`, `pollSeconds`, `deadPollSecondsFactor`, `deadPollMaxSeconds`, `deadMaxConcurrency`, `deadLogsEnabled`, `reasonableCheckSeconds`, `unseenForgetHours`, `maxConcurrency`, `queueCapacity`, `seeds`, three hostname ignore lists, `ignoreReplicationUsernames`, and `filterLogsEnabled`.

Change poll/concurrency/queue with MySQL connection, latency, queue, and metadata-write observation. Test filters with positive and negative examples.

## Remaining `topology` policy

- Replication lag/credential queries execute on managed instances; verify permission, type, and latency.
- Write buffer controls batch size, enablement, and flush interval.
- Compatibility flags change safety decisions and must not mask unverified engine differences.
- Snapshot interval controls topology snapshots; verify the semantics of zero in the same version.
- Hostname includes resolution methods, unresolve behavior, expiry, and reject pattern.
- Classification includes alias/domain/instance/promotion/site/environment/semi-sync queries and patterns.
- Pools, analysis reduction, and operations (`useSuperReadOnly`, bulk wait, concurrency) affect views and mutations.

## Authentication, Agent, and secrets

`authentication.method` accepts empty, `basic`, `multi`, `proxy`, or `token`. Treat Basic/access tokens, database passwords, Consul token, and key paths as secrets. `power` controls general actions; `configurationAdmins` controls recovery-setting writes and empty lists deny them.

Agent listener/TLS is a separate exposure. Before `agents.serveHTTP`, verify port, certificate/OU, poll/unseen/stale seed, and remote command privileges.

## Pseudo-GTID, audit, and Consul

Pseudo-GTID fields are `auto`, `pattern`, `patternIsFixedSubstring`, `monotonicHint`, `detectQuery`, `binlogEventsChunkSize`, and `skipBinlogContaining`; accept them with injection and retention.

Audit supports file, syslog, backend, and purge days; sink failures must remain visible. Consul address/scheme/token/datacenter/timeout, six TLS settings, and KV provider/max/prefix/cross-DC form one external-publication contract.

## Change checklist

1. Compare redacted `dump-config` output from the same version.
2. Mark secrets, node-specific fields, and restart requirements; never clone one node config to every voter.
3. Strict-parse, then roll one follower.
4. Read health, Raft, pools, discovery, authentication, telemetry/audit, and integrations.
5. Keep recovery disabled until classification, candidates, policy, and Hooks are reconfirmed.
