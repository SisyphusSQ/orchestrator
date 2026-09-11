# Web console

**English** · [中文](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Web-Console) · [Wiki home](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

The production Web console is a React, TypeScript, and Ant Design application embedded in `bin/orchestrator`. It shares the service listener, URL prefix, authentication, and TLS policy. No `resources/`, `web/`, Node.js, or separate static server is required at runtime.

`make binary` builds the frontend into `web/dist/`, synchronizes it to the embed source under `web/assets/`, then builds the server. `make build` additionally builds `bin/orch`. Direct `go build` does not prepare production Web assets. Docker uses a separate Node build stage and copies the result into the Go stage; the final runtime image does not contain Node. Missing embedded assets return an explicit 503 instead of reading from the working directory.

Open `/web/clusters` under the configured service origin. If `server.urlPrefix` is `/orchestrator`, use `/orchestrator/web/clusters`.

## Operator workflow

The console provides cluster and problem summaries, topology and list views, instance details, discovery/search, maintenance and downtime controls, topology refactoring, recovery views, audit history, and optional Agent/seed pages. Available actions reflect server capabilities and read-only policy.

Topology supports pan/zoom, minimap, collapse, compact and data-center views, instance aliases, list fallback, and smart/classic/GTID/Pseudo-GTID change proposals. Instance details expose replication state, positions, lag, semi-sync, errors, GTID, tags, equivalent coordinates, maintenance, and recent recovery. Recovery, audit, detection, and Agent/seed pages keep stable cluster/alias/instance/ID/UID deep links where the route remains supported.

Mutating operations require confirmation and are disabled while in flight. The UI checks both HTTP status and the API response `Code`. A disconnected or timed-out operation is displayed as unknown rather than automatically replayed. Read back the topology, recovery, maintenance, and audit state before taking another action.

A follower may proxy supported operations to the leader. The leader still performs final authorization, readiness, and topology checks. Browser hints such as drag-and-drop validation never replace server validation.

## Authentication and safety

The console follows the server's anonymous, Basic, multi, proxy, token, read-only, TLS, and mTLS configuration. `/api/web-config` contains UI capabilities, not secrets. Credentials must not be embedded in frontend source or URLs. Reverse proxies must preserve the configured URL prefix, trusted identity headers, and browser origin checks.

## Development preview

Use `make web-dev` for Vite hot reload and `make storybook` for isolated component/business-state scenarios. These development servers are loopback-only by default and are not deployment artifacts. See [Development](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Development) for prerequisites and verification boundaries.

Set `ORCH_API_TARGET` for the Vite proxy and `ORCH_URL_PREFIX` when testing a prefixed deployment. Storybook uses MSW-only fixtures and cannot fall through to a real API. Browser fixtures prove UI behavior, not a real MySQL topology, production Raft, proxy authentication, or recovery outcome. Production acceptance should exercise discover → topology → detail → one controlled operation → API/MySQL/audit readback through the actual proxy and authentication path.

## Page and route map

Paths live under `<server.urlPrefix>/web`. Deep-link refresh must fall back unknown Web paths to the embedded entry without rewriting `/api`, `/health`, or `/metrics`.

| Route | Page | Main reads/actions |
| --- | --- | --- |
| `/` | Default | redirects to `/clusters` |
| `/clusters` | Overview | health, problem-first list, search, topology entry |
| `/cluster/*` | Topology | graph/list, zoom/collapse, selection, proposals |
| `/clusters-analysis` | Analysis | current analysis, candidate, block reason |
| `/search/*` | Search | hostname/version/port query |
| `/discover` | Discover | explicit discovery; changes orchestrator state |
| `/cluster-pools/*` | Pools | mappings and heuristic candidates |
| `/audit/*` | Audit | pages, instance filter, parameters, result |
| `/audit-failure-detection/*` | Detection | history separate from recovery |
| `/audit-recovery/*` | Recoveries | analysis, successor, errors, acknowledgement |
| `/audit-recovery-steps/*` | Steps | execution by recovery UID |
| `/agents`, `/agent/*` | Agent | status, disk/MySQL, remote actions when enabled |
| `/seeds`, `/seed-details/*` | Seed/restore | history, detail, abort when enabled |
| `/recovery-settings` | Settings | 23 policies, Hook profiles/assignments |
| `/status` | Status | node, user, Raft, readiness, leader |
| `/about`, `/home`, `/faq`, `/keep-calm` | Help | compatibility help routes |
| `*` | 404 | unmatched Web route renders an in-app 404 |

## UI examples

These images are generated from current Storybook fixtures and validated as documentation assets. They show layout, not production data or live E2E.

![Cluster overview](https://raw.githubusercontent.com/SisyphusSQ/orchestrator/main/docs/assets/screenshots/cluster-overview.png)

![Topology detail](https://raw.githubusercontent.com/SisyphusSQ/orchestrator/main/docs/assets/screenshots/topology-detail.png)

![Recovery settings](https://raw.githubusercontent.com/SisyphusSQ/orchestrator/main/docs/assets/screenshots/recovery-configuration.png)

## Topology operation flow

1. Check connection and update time, refresh, and resolve query errors.
2. Confirm problem count, primary, and instance count before opening a cluster.
3. Cross-check graph/list and inspect source/destination threads, lag, GTID/coordinates, read-only, semi-sync, tags, maintenance, and recovery.
4. Read proposal and disabled reason. Browser validation never replaces server capability, candidate, and authorization checks.
5. Record owner, reason, destination, and exact parameters; confirm once.
6. Refresh topology, instance, audit, and recovery. Timeout/disconnect is unknown and must not replay.

## Recovery, read-only, and concurrency

Analysis explains the current problem; recovery history explains attempted work; detection records confirmation. Correlate steps, successor, errors, fingerprints, and external results by recovery UID. Acknowledge changes acknowledgement only.

Header capability comes from `/api/web-config`. In-flight disabling does not prevent another browser or CLI writer. Settings use revision protection; topology operations require before/after reads. An enabled global switch still needs leader/quorum, filters, block windows, candidates, site rules, and effective policy.

Startup connection, local query error, confirmed failure, and unknown result are distinct. Preserve request ID, node, time, and audit instead of refreshing and retrying.

## Production browser acceptance

- Use the real domain, proxy, TLS/mTLS, and authentication.
- Refresh root and cluster/audit/recovery deep links; verify prefix/assets.
- Prove read-only cannot write, then run one reversible isolated action with authorization.
- Simulate disconnect and confirm no replay; recover outcome from API/audit.
- Read Web, API, MySQL, Raft, and audit independently. Storybook does not cover them.
