# Upgrading

**English** · [中文](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Upgrading) · [Wiki home](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

Current `main` can contain changes newer than the latest published release. Treat the binary, configuration, service unit, `orch` client, and embedded Web assets as one tested deployment set; a successful source build does not mean a release has been published.

## Preflight

1. Record the exact source/release revision and review every ledger entry below that is newer than the deployed revision.
2. Back up every node's Raft directory and independent metadata backend using procedures that preserve each store's consistency.
3. Inventory all configuration layers, generated service arguments, automation scripts, monitoring rules, reverse proxies, and external hooks/KV consumers.
4. Exercise startup, quorum, discovery, a representative read and write, recovery policy, Web/API authentication, metrics, and rollback in an isolated environment.
5. Define maintenance ownership, traffic cutover, abort criteria, and independent post-change readback.

## Major current breaks

- Configuration files and `dump-config` now use one lowerCamel layered structure. There is no compatibility path for legacy flat fields; migrate configuration, scripts, and generators before startup.
- Server mode is Raft-only. Remove `RaftEnabled`; configure a durable ID, data directory, bind, and advertise address.
- `orchestrator server` replaces historical `http`/`continuous` entry points. `orchestrator admin` is local maintenance only.
- The standalone `orch` HTTP client replaces the database-connected CLI, `-c` commands, and shell client.
- Prometheus/OpenTelemetry replaces Graphite and historical raw metric APIs; removed settings are rejected.
- ZooKeeper publishing and `ZkAddress` are removed. Migrate consumers to Consul KV or an external hook first.
- Web assets are embedded; remove deployment assumptions that require an external frontend resources directory.
- Source packages moved to `cmd/` and `internal/`; historical public Go import paths are not preserved.
- HTTP transport, backend DAO, and logging implementations changed. Validate authentication/proxy behavior, representative database paths, log parsing, and syslog availability.

## Current change ledger

### Layered configuration

Configuration is grouped by responsibility under `server`, `raft`, `metadata`, `topology`, `authentication`, `agents`, `observability`, and related sections, with lowerCamel nested keys. Legacy fields are not converted automatically: for example, migrate `RaftNodeID`, `BackendDB`, `MySQLTopologyUser`, and `ConsulAddress` to `raft.nodeID`, `metadata.type`, `topology.mysql.user`, and `consul.address`. Generate each environment's new configuration from the checked-in `conf/` samples, validate `dump-config` and startup with the matching binary, then roll it out. Rollback must restore both the old binary and its matching flat configuration.

### Metadata schema with uniform auto-increment primary keys

All 50 tables use the single auto-increment primary key `id`. Former business primary keys become unique indexes; existing auto-increment columns are renamed while retaining their values. Empty databases execute [`docs/schema/mysql.sql`](https://github.com/SisyphusSQ/orchestrator/blob/main/docs/schema/mysql.sql) and receive `canonical-v2` only after validation. Normal startup does not migrate `canonical-v1` or `legacy-v1` databases.

This upgrade requires stopped writers. Stop all nodes and external writers, back up each independent metadata database and its matching Raft directory, then run `orchestrator admin migrate-metadata-id --config=/absolute/path/orchestrator.yaml` with the new binary. The migration validates each table, retains a pending marker on failure, and resumes from the actual structure. Read back primary keys, business uniqueness, historical IDs and linked records before starting the cluster. See the [schema migration guide](https://github.com/SisyphusSQ/orchestrator/blob/main/docs/schema/migration-guide.md) for engine boundaries and rollback.

Never import `mysql.sql` into a non-empty database or run mixed binaries that expect different column names. Old Raft snapshots map historical ID column names on restore; new node-local surrogate IDs are excluded. Rollback requires the previous binary and matching metadata/Raft backups, not deleting a migration marker.

### Raft-only server

Non-Raft, shared-backend election, and semi-HA paths are removed. Existing Raft members keep identity, logs, snapshots, and independent backends; change configuration and the start command, restart, and do not bootstrap again. A former non-Raft deployment must stop old discovery/recovery/writes, prepare independent backends and identities, form a new Raft cluster, validate it, then cut traffic. Do not run old and new recovery systems concurrently.

### Standalone HTTP client

The shell client, database-connected business CLI, `-c`/`cli`, aliases, and their environment variables are removed. Install `orch`, configure `ORCH_ENDPOINT`, and replace start scripts with `orchestrator server`; local maintenance belongs under `orchestrator admin`. Rollback restores matching server/client binaries, configuration, and automation together. Do not assume mixed versions are safe for newly added commands or write-result contracts.

### Source, Web, and HTTP transport

Go sources moved from `go/` to `cmd/` and `internal/`; old public import paths are not compatibility APIs. `make binary` embeds the React Web assets, so runtime deployment no longer reads external frontend resources. Gin v1.12.0 is isolated behind the project transport adapter; route synonyms, trailing slash/HEAD behavior, authentication, prefixes, TLS/mTLS, Raft proxy termination, and HTTP/HTTPS/Unix listeners remain contracts that must be exercised through the real proxy.

### Backend DAO and connection lifecycle

GORM handles stable backend DAO reads/writes for MySQL and SQLite while reusing the one process-owned pool. It does not run `AutoMigrate`; ordered SQL remains schema authority. `LastInsertId`, topology, snapshots, Raft, and dynamic result paths retain explicit adapters. Backend, discovery, and topology-operation pools close on shutdown and are not rebuilt by `SIGHUP`; restart after endpoint, credential, TLS, timeout, packet, lifetime, or pool-size changes.

### Observability and logging

Prometheus/OpenTelemetry replaces Graphite and raw/aggregated Collection APIs. Remove all six retired fields listed in [Observability](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Observability), scrape every node, and update dashboards and alert rules. This change adds no schema or Raft format migration.

Zap text output is `time<TAB>[LEVEL]<TAB>[caller]<TAB>message`; update parsers. `logging.syslog.enabled` or `audit.toSyslog` initialization failure now stops startup, and audit sink write failures are visible. Writes are synchronous rather than one goroutine per entry, so validate sink latency under representative load.

### Consul and ZooKeeper

Built-in ZooKeeper publishing and `ZkAddress` are removed. Migrate consumers to Consul or an external recovery hook before upgrade. Consul now uses the official SDK, sends ACL tokens only in `X-Consul-Token`, verifies HTTPS by default, and fails startup on client/TLS construction errors. Configure trusted CAs, server name, and paired mTLS files before replacing a build that relied on skipped verification. Cross-datacenter writes may partially succeed and are not rolled back; timed-out writes are not replayed automatically. Consul settings require restart.

## Deployment cutover checklist

Use this sequence after completing the change-ledger-specific preparation above:

1. Freeze configuration, binaries, service definitions, `orch`, dashboards/rules, and hook versions as one revisioned deployment set. Record checksums and the exact rollback set.
2. Stop or fence every old discovery/recovery writer that could act on the same topology. Confirm one intended Raft cluster, stable member identities, and the expected node-local metadata backend for each node.
3. Apply any required stopped-writer metadata migration exactly once per independent backend, then read back its schema marker and table invariants before starting application nodes.
4. Start or replace one intended member at a time. For every node, read back process health, readiness, Raft identity, advertised address, membership, metadata connectivity, and logs before proceeding.
5. Confirm a single Leader and quorum, then exercise discovery and representative read-only API/Web paths. Keep business mutations and automated recovery fenced until policy, authentication, proxy, metrics, and hook configuration are read back.
6. Enable one controlled write path and verify the resulting metadata and Raft state. Enable discovery/recovery only after ownership is unambiguous; never overlap old and new recovery systems.
7. Switch client/proxy traffic, verify leader-aware routing and real authentication/TLS, then check every node's metrics, traces, logs, dashboards, and alerts.
8. Hold the rollback window until a representative topology operation, application read/write, and restart or failover rehearsal meet the declared acceptance criteria.

Abort when identity, quorum, schema state, credentials, routing ownership, or write results are ambiguous. A client timeout is not proof of rollback: read back state before repeating a mutation. If the deployment change also includes a MySQL primary move, run it separately with the [planned switchover](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Planned-Switchover) procedure.

## Rollout and rollback

Do not create multiple Raft clusters from the same logical deployment or allow old and new systems to recover the same topology concurrently. Existing Raft members reuse their state and are not bootstrapped again. Do not assume arbitrary mixed-version membership is safe; use the compatibility boundary documented for the specific revisions.

A rollback restores the previous server and client binaries together with their matching configuration and service definition. Persistent state handling depends on the changes crossed, so follow the detailed upgrade entry instead of deleting or rewriting Raft/database state ad hoc.
