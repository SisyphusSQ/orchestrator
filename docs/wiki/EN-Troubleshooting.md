# Troubleshooting
**English** · [中文](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Troubleshooting) · [Wiki home](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

Identify the failed layer and collect one time window before changing state. Do not repeatedly reload, bootstrap, retry mutations, or edit metadata while the cause is unknown; those actions destroy evidence.

## Five-minute triage

1. Record time, request ID, serving node, version/commit, configuration source, and recent changes.
2. Read `/health/live`, `/health/ready`, `/health/leader-ready`, and `/api/raft/configuration` in order.
3. Confirm actual URL, prefix, proxy, identity, HTTP status, and response `Code`.
4. Read target instance/cluster and check last discovery, threads, lag, GTID/coordinates, read-only, maintenance, and downtime.
5. Correlate application log, audit, recovery steps, Prometheus metrics, and traces. Preserve original errors and redact before sharing.

## Symptom matrix

| Symptom | Check first | Common mistake | Stop when |
| --- | --- | --- | --- |
| live fails | Process, listener, TLS, crash log | Calling refusal a Raft failure | Repeated exit or unreadable certificate |
| live succeeds, ready fails | Metadata, initialization/catch-up, health age | Sending management writes anyway | Quorum unknown or backend unavailable |
| leader-ready fails | Leader, election, member addresses | Forcing a follower to be leader | Partition or multiple suspected leaders |
| Web 503/blank | Embedded assets, prefix, `web-config`, cache | Treating Storybook as production evidence | Asset/backend versions differ |
| CLI 401/403 | Auth, credential, proxy header, power users | Calling authorization a missing route | Identity source is untrusted |
| API 200 but operation fails | JSON `Code`, `Message`, `Details` | Checking HTTP status only | Unknown write or validation failure |
| Instance absent | Network/TLS/account/identity/filters | Loosening regex first | One server has multiple canonical names |
| Stale topology | Queue, concurrency, poll, slow query, clocks | Mutating from stale view | Discovery age exceeds change window |
| Recovery absent | Global switch, policy, quorum/leader, block, candidate | Going directly to force failover | Fencing or candidate is unsafe |
| Hook fails | Revision, command, identity, timeout, output limit | Repeating Test Run in production | Non-idempotent Hook or secret output |

## Configuration startup failure

Parsing is strict. Unknown or legacy fields, case errors, duplicate keys, multiple YAML documents, trailing content, or multiple formats at one search location fail. Use the same binary's `dump-config` and the original startup error; do not delete sections until it starts by accident.

Identity, Raft addresses/storage, HTTP listeners, database pools, and tracing do not change through reload. A successful reload proves only reloadable sections passed.

## Raft problems

- Uninitialized: bootstrap one new node only; join others through the leader.
- No leader: check majority, bidirectional advertise connectivity, clocks, and disk. Never bootstrap again.
- Stale address: use protected membership changes and configuration-index concurrency control.
- ID mismatch: stop and verify `node-id`, configuration, and full-directory provenance. Do not edit files to force startup.
- Timeout: treat as unknown and read membership from multiple nodes.

## Topology and recovery

Read replication directly in MySQL and compare with orchestrator. If MySQL changed but the view did not, investigate discovery. If the view is current but an operation is rejected, inspect candidate, filters, version, GTID, site policy, and recovery-block reason. Fence first when two primaries are writable.

Analyze recovery with failure detection, analysis, successor, all errors, Hook fingerprint, operation audit, and external routing. A recovery that did not run may be a correct safety refusal.

## Support bundle

Retain version/commit, redacted effective config, health endpoints, Raft configuration/state, relevant instance/cluster JSON, failed request/response, same-window logs, audit/recovery UID, metric evidence, and recent deployment/config diff. Exclude database passwords, auth tokens, Consul tokens, private keys, and Hook secrets.

## Escalation

Stop and escalate when quorum is unknown, two primaries are writable, a write outcome is unknown, metadata schema differs, Raft identity conflicts, audit evidence is missing, or direct state-file editing seems necessary. Record every command and raw result so the next operator does not repeat dangerous actions.
