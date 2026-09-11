# Package guide

**English** · [中文](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Package-Guide) · [Wiki home](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

This guide maps every directory that currently contains Go code to its responsibility. It is a placement guide for contributors, not a promise that every internal symbol is a public compatibility API. The root server module is `github.com/openark/orchestrator`; `tools/orch-cli` is a separate module.

## Dependency direction

```text
cmd/orchestrator
    └── internal/app
          ├── internal/http/* ────────┐
          ├── internal/logic/*        │
          └── internal/agent          │
                                      v
internal/logic/recovery ──> internal/inst/change/regroup
                                      │
                                      v
                           internal/inst/change/relocation
                                      │
                                      v
                           internal/inst/change/replication

http/app/logic/inst/agent/process ──> repository ──> models
                 └───────────────────────────────> models
```

Dependencies point from composition and use cases toward capabilities, persistence, and stable models. `repository` must not import business packages. All `internal/inst/*` packages remain independent of `app`, `http`, and `logic`. The `internal/inst`, `internal/inst/change`, and `internal/logic` roots are namespaces only and contain no Go files.

## Entrypoints and shared runtime

| Package directory | Responsibility and placement rule |
| --- | --- |
| `cmd/orchestrator` | Main server binary and local `admin` commands. Keep flag parsing and process wiring here; business behavior belongs under `internal/`. |
| `internal/agent` | Agent polling and instance-topology agent runtime. Put Agent protocol orchestration here, not generic topology discovery. |
| `internal/app` | Process composition for HTTP listeners, Raft, observability, startup, and shutdown. It owns lifecycle wiring rather than domain algorithms. |
| `internal/attributes` | Business access to persisted general attributes. Storage statements stay in `repository/metadata`. |
| `internal/config` | Layered configuration models, defaults, validation, CLI overrides, Raft address normalization, Consul, and observability settings. New user-facing configuration starts here and must be documented. |
| `internal/golib/log` | Project logging facade and formatting. Use it for existing logging contracts; do not add domain behavior. |
| `internal/golib/math` | Small dependency-free numeric helpers retained for internal callers. |
| `internal/golib/tests` | Lightweight test/spec helpers shared by Go tests. It must not become a production utility package. |
| `internal/golib/util` | Small dependency-free text helpers retained from the internal utility layer. Prefer a domain package when behavior has business meaning. |
| `internal/kv` | Consul KV publication, transactions, and client lifecycle. It is separate from metadata repositories because the KV store is an external integration. |
| `internal/observability` | Bounded Prometheus metric and OpenTelemetry trace vocabulary. Instrumentation ownership belongs here; HTTP exposure belongs in `internal/http/observability`. |
| `internal/os` | Operating-system process inspection and Unix checks. Keep portable business logic outside this package. |
| `internal/process` | Process identity, health state, host registration, and access-token store behavior shared by runtime components. |
| `internal/raft` | Package name `orcraft`; Hashicorp Raft runtime, FSM, snapshots, membership, persistence, status, HTTP peer client, and Raft telemetry. Application command semantics live in `logic/raftstate`. |
| `internal/recoverypolicy` | Typed recovery-policy values plus global/cluster override resolution. It defines policy state, not recovery execution. |
| `internal/ssl` | TLS configuration helpers for database and service connections. Keep certificate policy explicit and return construction errors. |
| `internal/util` | Cross-cutting tokens and bounded log cache utilities that have no clearer domain owner. New code should prefer the narrowest owning package. |

## HTTP boundary

| Package directory | Responsibility and placement rule |
| --- | --- |
| `internal/http` | Stable route composition, compatibility synonyms, leader-proxy placement, and Web action guards. Do not put capability handlers back in the root package. |
| `internal/http/agent` | Agent-specific API and management routes. |
| `internal/http/api/cluster` | Cluster lookup, aliases, search, pools, topology projections, tags, and cluster-level API handlers. |
| `internal/http/api/instance` | Instance detail and direct-replica API handlers. |
| `internal/http/api/maintenance` | Maintenance and downtime request handlers and their operator-facing semantics. |
| `internal/http/api/recovery` | Analysis, recovery, acknowledgement, candidate, graceful/forced takeover, and recovery-policy handlers. |
| `internal/http/api/system` | System information, hostname resolution, audit, health, and global-control handlers that do not belong to another capability. |
| `internal/http/api/topology` | Topology mutation and replication-control handlers, from relocation through GTID and binlog operations. Algorithms remain under `inst/change`. |
| `internal/http/authz` | Authorization decisions derived from the authenticated principal and configured policy. Authentication transport remains in the application listener stack. |
| `internal/http/cli` | Registration adapter for the generated HTTP-backed `orch` command surface. The machine-readable catalog remains in the CLI module. |
| `internal/http/contract` | Stable API response envelopes and shared response types. Do not leak persistence rows through this boundary. |
| `internal/http/observability` | Health, readiness, metrics, and tracing-related HTTP routes. These are node-local where the route contract requires it. |
| `internal/http/presenter` | Domain-to-VO mapping and response writing. Keep JSON compatibility fields here rather than in repositories. |
| `internal/http/raft` | Raft management API and follower-to-leader reverse proxy. It terminates proxied mutations so they cannot execute again locally. |
| `internal/http/request` | Parsing and validation of instance and cluster selectors from route parameters. |
| `internal/http/transport` | Project-owned router, handler, params, responder, and principal abstractions plus the Gin adapter. It must not depend on HTTP capabilities, logic, instances, or repositories. |
| `internal/http/web` | Embedded console routes, UI configuration, static assets, and SPA fallback behavior. |

The authoritative route inventory is [`internal/http/routes.go`](https://github.com/SisyphusSQ/orchestrator/blob/main/internal/http/routes.go). Application handlers depend on project transport contracts, not directly on `gin.Context`.

## Instance and topology capabilities

| Package directory | Responsibility and placement rule |
| --- | --- |
| `internal/inst/analysis` | Replication failure analysis models, calculation, and analysis history. Detection evidence belongs here; execution belongs in `logic/recovery`. |
| `internal/inst/audit` | Recording and reading operator and topology audit events. |
| `internal/inst/binlog` | Binlog events, cursors, coordinate lookup, and correlation queries used by matching algorithms. |
| `internal/inst/candidate` | Failover candidate suggestions, expiry, and candidate metadata. |
| `internal/inst/change/regroup` | Candidate ordering and topology regrouping across GTID, Pseudo-GTID, file-position, and binlog-server strategies. It may depend on relocation, never the reverse. |
| `internal/inst/change/relocation` | Move, repoint, Pseudo-GTID alignment, and higher-level relocation algorithms. It may depend on low-level replication operations, never on regroup. |
| `internal/inst/change/replication` | Lowest-level replication and server-state mutations: start/stop, source changes, read-only, GTID controls, credentials, binlogs, and related commands. |
| `internal/inst/cluster` | Cluster identity, aliases, cluster-level projections, heuristics, and persisted cluster metadata. |
| `internal/inst/discovery` | One-instance MySQL topology probes, dead-instance filtering, and group-replication discovery. Continuous scheduling belongs in `logic/discovery`. |
| `internal/inst/downtime` | Operator and recovery downtime windows and their persistence-facing business rules. |
| `internal/inst/equivalence` | Equivalent primary binlog positions used to relocate through known coordinate relationships. |
| `internal/inst/gtid` | Pure parsing and set operations for MySQL Oracle GTIDs. It has no repository or other project-package dependency. |
| `internal/inst/instance` | Canonical `Instance`, `InstanceKey`, binlog coordinates, replication state, and core model behavior. Do not duplicate these models in transport or persistence packages. |
| `internal/inst/inventory` | Persisted instance inventory, lookup, cache, write buffering, and Pseudo-GTID state. It must not import discovery or change packages. |
| `internal/inst/maintenance` | Instance maintenance windows and related operator ownership rules. |
| `internal/inst/mysql` | Version-aware MySQL/MariaDB query and result-field vocabulary, including legacy replication terminology. It remains dependency-free from other project packages. |
| `internal/inst/pool` | Cluster pool submissions, membership, and pool-based selection data. |
| `internal/inst/resolve` | Hostname, address, default-port resolution, and persisted resolution state. |
| `internal/inst/tag` | Instance tags and tag-based selection. Tags are operational metadata and writes are business mutations. |
| `internal/inst/topology` | Topology rendering, relationship inspection, and topology-oriented reads assembled from discovery, inventory, and tags. |

The enforced ordering for mutations is `regroup -> relocation -> replication`. Choose the highest layer that represents the use case; do not call low-level SQL or replication operations from HTTP handlers.

## Long-running logic

| Package directory | Responsibility and placement rule |
| --- | --- |
| `internal/logic/discovery` | Discovery queue, continuous scheduling, and runtime coordination. It may invoke recovery after publishing observations. |
| `internal/logic/raftstate` | Application of replicated commands and construction/restoration of application snapshot data. It bridges business state to the Raft FSM. |
| `internal/logic/recovery` | Failure detection coordination, candidate planning, recovery execution, hooks, postponed work, and recovery history. It must not depend on discovery, HTTP, or app. |

## Models and repositories

| Package directory | Responsibility and placement rule |
| --- | --- |
| `internal/models/do` | Stable database rows and persistence projections with explicit column mappings. DOs never cross repository APIs. |
| `internal/models/domain` | Business-facing objects and value types independent of persistence, transport, configuration, and GORM. |
| `internal/models/dto` | Input contracts accepted at HTTP or command boundaries. Validation fields belong here. |
| `internal/models/vo` | Concrete output contracts for HTTP and observability. Preserve JSON field compatibility here. |
| `internal/repository` | Repository lifecycle and architecture tests. It initializes and closes storage capabilities without exposing raw pools. |
| `internal/repository/database` | Process-owned pools, drivers, GORM adapters, TLS, dynamic result scanning, and database runtime primitives. Only repository packages may import it. |
| `internal/repository/metadata` | Typed reads and writes for the node-local orchestrator metadata database, split by business area. Transactions that make one metadata change atomic belong here. |
| `internal/repository/schema` | Metadata schema detection, initialization, legacy patches, dialect rules, and explicit metadata-ID migration. It never uses `AutoMigrate`. |
| `internal/repository/topology` | Connections, queries, commands, and transactions against managed MySQL instances. Callers receive a client interface, not raw database handles. |

See [Repository and model boundaries](https://github.com/SisyphusSQ/orchestrator/blob/main/docs/architecture/repository-models.md) for the detailed DO/domain/DTO/VO contract.

## Generated assets, clients, and executable checks

| Package directory | Responsibility and placement rule |
| --- | --- |
| `docs/schema` | Go-embedded executable metadata DDL and schema contract checks. SQL files in the same directory remain authoritative. |
| `tests/cli` | End-to-end Go tests for CLI, Raft, and recovery-settings contracts. These tests exercise process/API boundaries rather than reusable runtime code. |
| `tools/orch-cli` | Main package for the independently built `orch` binary and its separate Go module. It must not initialize server runtime or read server configuration. |
| `tools/orch-cli/internal/client` | HTTP endpoint selection, authentication, TLS, request execution, response validation, and unknown-result handling for `orch`. |
| `tools/orch-cli/internal/cmd` | Cobra commands generated from `catalog.json`, shared flags, validation, projections, batch behavior, and exit-code mapping. |
| `web` | Package name `webassets`; embeds the built React console into the server. The TypeScript application, Storybook, and browser tests also live below this directory. |

## Where should new code go?

1. Start from the user-visible capability, not from a convenient existing file.
2. Put external input/output shapes in `models/dto` and `models/vo`; put cross-capability business values in `models/domain`; keep database rows in `models/do`.
3. Add metadata or managed-MySQL access to the corresponding repository. Business packages must not import drivers, GORM, or raw pools.
4. Put one-instance reads in the relevant `inst/*` capability; topology mutations follow `regroup -> relocation -> replication`.
5. Put long-running discovery/recovery/Raft command orchestration in `logic/*`; keep HTTP packages as parsing, authorization, invocation, and presentation boundaries.
6. Wire new capabilities in `app` and `cmd` only after their interfaces are stable.
7. Add both language pages when behavior changes, then run the focused package tests and `make test-docs`.

Architecture tests under `internal/repository` enforce the key directions. The current package-split rationale and remaining large-package priorities are recorded in [`docs/architecture/package-boundaries.md`](https://github.com/SisyphusSQ/orchestrator/blob/main/docs/architecture/package-boundaries.md).
