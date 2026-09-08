# Upgrading

**English** · [中文](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Upgrading) · [Wiki home](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

Current `main` can contain changes newer than the latest published release. Treat the binary, configuration, service unit, `orch` client, and embedded Web assets as one tested deployment set; a successful source build does not mean a release has been published.

## Preflight

1. Record the exact source/release revision and read all newer entries in [`docs/upgrading.md`](https://github.com/SisyphusSQ/orchestrator/blob/main/docs/upgrading.md).
2. Back up every node's Raft directory and independent metadata backend using procedures that preserve each store's consistency.
3. Inventory all configuration layers, generated service arguments, automation scripts, monitoring rules, reverse proxies, and external hooks/KV consumers.
4. Exercise startup, quorum, discovery, a representative read and write, recovery policy, Web/API authentication, metrics, and rollback in an isolated environment.
5. Define maintenance ownership, traffic cutover, abort criteria, and independent post-change readback.

## Major current breaks

- Server mode is Raft-only. Remove `RaftEnabled`; configure a durable ID, data directory, bind, and advertise address.
- `orchestrator server` replaces historical `http`/`continuous` entry points. `orchestrator admin` is local maintenance only.
- The standalone `orch` HTTP client replaces the database-connected CLI, `-c` commands, and shell client.
- Prometheus/OpenTelemetry replaces Graphite and historical raw metric APIs; removed settings are rejected.
- ZooKeeper publishing and `ZkAddress` are removed. Migrate consumers to Consul KV or an external hook first.
- Web assets are embedded; remove deployment assumptions that require an external frontend resources directory.
- Source packages moved to `cmd/` and `internal/`; historical public Go import paths are not preserved.
- HTTP transport, backend DAO, and logging implementations changed. Validate authentication/proxy behavior, representative database paths, log parsing, and syslog availability.

## Rollout and rollback

Do not create multiple Raft clusters from the same logical deployment or allow old and new systems to recover the same topology concurrently. Existing Raft members reuse their state and are not bootstrapped again. Do not assume arbitrary mixed-version membership is safe; use the compatibility boundary documented for the specific revisions.

A rollback restores the previous server and client binaries together with their matching configuration and service definition. Persistent state handling depends on the changes crossed, so follow the detailed upgrade entry instead of deleting or rewriting Raft/database state ad hoc.
