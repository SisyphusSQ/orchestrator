# Production deployment
**English** · [中文](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Production-Deployment) · [Wiki home](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

This page is the production baseline from capacity decisions through acceptance. The server runs only in Raft mode; production should use three or five voters. Raft coordination state, each node's metadata database, and the managed MySQL state are separate consistency domains.

## 1. Choose the deployment model

| Decision | Recommendation | Reason |
| --- | --- | --- |
| Voters | Start with 3; use 5 for additional failure-domain coverage | Three tolerate one voter loss; five tolerate two; an even voter adds no fault tolerance |
| Identity | Keep a stable `raft.nodeID` per node | Identity is durable state, not an IP or ephemeral Pod name |
| Raft storage | Durable local storage per node | `raft.db`, `node-id`, and `snapshots/` must stay identity-consistent |
| Metadata | Independent MySQL backend or SQLite file per node | Raft nodes must not share one metadata database |
| HTTP entry | Controlled load balancer or reverse proxy | Centralizes TLS, authentication, URL prefix, and health policy |
| Raft network | Member-to-member only | Do not expose it publicly or place it behind a stateless L7 proxy |

Spread voters so expected failure domains still leave a majority. Three voters across three zones is easy to reason about; two processes on one host are not two independent failure domains.

## 2. Select an artifact

### Source build

```sh
make deps
make web-deps
make build
```

This produces `bin/orchestrator` and `bin/orch`. `make build` prepares and embeds Web assets; plain `go build` is not equivalent. Pin the source revision and toolchain and retain build logs.

### Package and systemd

`make package`, `docker/Dockerfile.packaging`, and `etc/systemd/orchestrator.service` are the executable package path. Verify binary, configuration, state, user, and unit paths after installation instead of assuming the example matches every package manager layout.

### Container image

`docker/Dockerfile` builds the normal runtime image. `docker/Dockerfile.raft` and `docker/Dockerfile.system` support their verification environments. Mount durable Raft storage and, when selected, SQLite storage. Give every replica a stable unique node ID and reachable advertise address; replacing a container must not silently replace member identity.

## 3. Node files and permissions

Run under a dedicated unprivileged user and prepare a protected configuration, a writable per-node `raft.dataDir`, an independent SQLite file or MySQL credentials, an audit directory when configured, TLS material for each trust domain, and least-privilege Hook executables. Verify absolute paths, durable mounts, free space/inodes, and restrictive credential permissions. Hooks run as the server process; do not solve Hook access by running the server as root.

## 4. Network and reverse proxy

Allow client-to-HTTP, bidirectional voter-to-voter Raft, node-to-metadata, node-to-managed-MySQL, and optional Consul/Agent/telemetry traffic. Deny public access to Raft, metrics, and management APIs by default.

When using a prefix, configure the proxy and `server.urlPrefix` identically. Preserve required scheme/host and identity headers. With proxy authentication, only the trusted proxy may set `authentication.proxy.userHeader`; strip any client-supplied copy at the edge.

Use health endpoints deliberately:

- `/health/live` proves the process responds; do not use it for management traffic.
- `/health/ready` proves local backend and Raft readiness; use it when follower proxying is allowed.
- `/health/leader-ready` proves this node is the ready leader; use it for leader-only routing.

## 5. Initialization order

1. Produce per-node configuration and verify unique IDs, addresses, and storage.
2. Start every process and check `/health/live`; uninitialized nodes are not members yet.
3. Run `raft-bootstrap` exactly once on one chosen node.
4. Add remaining voters one at a time through the leader and read `raft-configuration` after each write.
5. Verify identical member IDs, addresses, and configuration index across nodes and a stable leader.
6. Enable load-balancer routing only after health policy is verified.
7. Run one controlled `discover`, then read cluster, topology, and instance details.
8. Keep automatic recovery disabled until classification, promotion, recovery policy, Hooks, fencing, audit, and drills are complete.

See [Raft operations](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Raft-Operations). Never bootstrap multiple empty nodes and attempt to join those independent clusters later.

## 6. systemd baseline

Use fixed `User`/`Group`, an absolute `ExecStart` configuration path, bounded restart policy and startup timeout, and an adequate file-descriptor limit. After a unit change, reload systemd and restart one voter at a time. Wait for membership and catch-up before proceeding.

An `active` unit is not acceptance. Read process liveness, Raft readiness, leader, committed membership, metadata access, MySQL discovery, and Web/API separately.

## 7. Production acceptance

- Versions, configuration source, and build revision are traceable on every node.
- Node IDs, Raft/HTTP addresses, and storage paths are unique and durable.
- Metadata is independent per node and initialized/migrated from [`docs/schema`](https://github.com/SisyphusSQ/orchestrator/tree/main/docs/schema).
- All voters agree on configuration; 3-node clusters have 2 available voters and 5-node clusters have 3.
- The actual proxy path verifies reads, leader writes, TLS/mTLS, authentication, prefix, and timeout behavior.
- A real topology completes discover → cluster → topology → instance readback.
- Metrics, logs, audit, tracing, and alert routing reach their intended sinks.
- Metadata and Raft backups are independent and have been restored in isolation.
- Before automatic recovery, complete a planned switchover and representative failure drill with MySQL, Raft, audit, Hook, and external KV/routing readback.

## 8. Change and rollback

Roll binaries, configuration, and certificates one node at a time: follower, remaining followers, then leader. Configuration reload does not rebuild listeners, Raft identity/addresses, database pools, or the tracing exporter; restart for those changes. Before a binary rollback, read [Upgrading](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Upgrading) and verify metadata-schema and page-managed recovery-setting compatibility.
