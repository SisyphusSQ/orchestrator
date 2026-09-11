# HTTP API route reference
**English** · [中文](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-API-Reference) · [Wiki home](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

This page groups the current HTTP surface. [`internal/http/routes.go`](https://github.com/SisyphusSQ/orchestrator/blob/main/internal/http/routes.go) and `internal/http/cli` registration remain authoritative; clients must not guess paths or methods from older documentation.

## Base contract

- The default API prefix is `/api`; with `server.urlPrefix=/orchestrator`, use `/orchestrator/api/...`.
- Declared GET routes support HEAD and both slash forms. Unknown paths/methods retain project 404 behavior without automatic redirect or 405.
- JSON is `application/json; charset=UTF-8`. Many endpoints return `Code`, `Message`, and `Details`; HTTP 200 is not sufficient.
- Historical compatibility includes mutating GETs. Only newer `registerAPIMethod` routes expose explicit POST/DELETE; retry classification still follows side effects.
- Proxyable requests can move from follower to leader. Node-local health, bootstrap/snapshot/configuration, and selected system status do not proxy.
- Protected writes pass `guardWebAction` plus business, Raft-readiness, and authorization checks.

## System, health, and Raft

| Method/path | Purpose | Locality/effect |
| --- | --- | --- |
| `GET /health/live` | process liveness | node-local, no readiness |
| `GET /health/ready` | backend/Raft readiness and freshness | node-local |
| `GET /health/leader-ready` | ready leader | node-local |
| `GET /metrics` | Prometheus | node-local; restrict exposure |
| `GET /api/health`, `/lb-check`, `/_ping`, `/leader-check` | compatibility/LB health | node-local |
| `GET /api/raft/configuration` | members, addresses, leader, index | node-local read |
| `POST /api/raft/bootstrap` | initialize one new node | node-local, exactly once |
| `POST /api/raft/members` | add member | proxyable; id/address/suffrage/optional expectedIndex |
| `DELETE /api/raft/members/:id` | remove member | proxyable; retain quorum |
| `POST /api/raft/leadership/transfer` | transfer leader | proxyable; read after disconnect |
| `POST /api/raft/snapshot` | trigger Raft snapshot | node-local; not metadata backup |
| `GET /api/reload-configuration` | reload supported values | node-local mutation |

## Discovery, instances, and clusters

| Family | Representative paths | Meaning |
| --- | --- | --- |
| Instance | `/instance/:host/:port`, `/instance-replicas/...` | detail and replicas |
| Discovery | `/discover`, `/async-discover`, `/refresh` | updates metadata; not a pure read |
| Forget | `/forget`, `/forget-cluster` | removes view; rediscovery remains possible |
| Cluster | `/cluster`, `/cluster/alias`, `/cluster-info` | supported name/alias/instance hints |
| Overview | `/clusters`, `/clusters-info`, `/masters`, `/all-instances`, `/problems` | monitoring and entry points |
| Text topology | `/topology`, `/topology-tabulated`, `/topology-tags` | human/script views |
| Search/bulk | `/search`, `/bulk-instances`, `/bulk-promotion-rules` | search and batch reads |

URL-encode host, alias, reason, owner, tag value, and search input instead of raw path concatenation.

## Topology mutations

| Family | Prefixes | Preconditions |
| --- | --- | --- |
| Smart | relocate/regroup | server selects mechanism; precheck still required |
| file:pos | move/repoint/take | correlatable coordinates and compatible filters |
| GTID | move/regroup GTID | compatible sets and handled errant GTID |
| Pseudo-GTID | match/regroup pgtid | complete markers and binlogs |
| Replication | start/stop/restart/reset/delay | changes MySQL state |
| Instance | read-only/writeable/semi-sync | not application fencing |
| Binlog/GTID | flush/purge/errant actions | potentially irreversible |

Legacy `slave` synonyms remain for compatibility; new clients should generate canonical `replica` paths.

Canonical examples include `/relocate/:host/:port/:belowHost/:belowPort`, `/move-below-gtid/:host/:port/:belowHost/:belowPort`, and `/match-replicas/:host/:port/:belowHost/:belowPort`. These are mutating routes even where historical compatibility exposes GET.

## Maintenance, downtime, tags, and pools

Maintenance uses `/begin-maintenance/:host/:port/:owner/:reason` and ends by instance or key. Downtime uses `/begin-downtime/:host/:port/:owner/:reason[/:duration]` and ends by instance. Tags support read/value/tagged and tag/untag/bulk clear. Pools support submit, cluster mapping, heuristic selection, and lag. Hostname routes manage resolve/unresolve state. `/submit-masters-to-kv-stores[/:clusterHint]` publishes KV state and requires external readback.

## Analysis, recovery, and audit

| Capability | Representative path | Note |
| --- | --- | --- |
| Analysis | `/replication-analysis` | current decision, not a promise to recover |
| Recovery | recover/lite/force/graceful | real topology effect; candidate may be explicit |
| Global switch | enable/disable/check recoveries | read check after write |
| Recovery audit | `/audit-recovery`, steps by UID | correlate steps, errors, Hooks |
| Detection | failure-detection audit, changelog | separate from recovery outcome |
| Acknowledge | ack routes | acknowledgement is not repair |
| Blocked | `/blocked-recoveries` | explains safety refusal |

## Page-managed recovery settings

Explicit-method JSON routes are `GET /api/recovery-policy/:scopeType/:scopeKey`, `POST /api/recovery-policy`, `GET|POST /api/recovery-hook-profiles`, `POST /api/recovery-hook-test`, `GET /api/recovery-hook-assignments/:scopeType/:scopeKey`, and `POST /api/recovery-hook-assignments`. Writes carry revision for optimistic locking. Scope is global `*` or explicit cluster alias. On conflict, reread and merge; Hook test executes a real server command.

## Agent API

Agent routes cover agents, active/recent seeds, seed detail/abort, and custom commands when enabled. The Agent listener has a separate TLS/OU boundary; main HTTP availability does not prove safe Agent exposure.

## Call and retry template

```sh
curl --fail-with-body \
  --cacert /path/ca.pem \
  -H 'Accept: application/json' \
  'https://orchestrator.example/orchestrator/api/clusters'
```

For writes, record request ID, node, canonical path without secrets, body checksum, time, and response. Failure before connection can select another endpoint; disconnect after possible transmission is unknown and requires resource, MySQL, Raft, or audit readback.

## Acceptance

Verify one read, one controlled write, auth failure, insufficient privilege, prefix, TLS/mTLS, slash/HEAD, follower proxy, business error in 200, unknown timeout, and post-write audit. A route test or curl 200 does not prove live MySQL or external routing state.
