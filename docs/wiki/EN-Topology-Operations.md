# Topology operations
**English** · [中文](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Topology-Operations) · [Wiki home](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

Topology operations change MySQL replication or instance state. First prove fresh source and destination state, select a method compatible with GTID/file:position/Pseudo-GTID, then verify in both MySQL and orchestrator. Never automatically retry a timed-out mutation.

## Common execution template

1. Resolve canonical `host:port`, cluster alias, primary, and replication mode.
2. Read source/destination threads, lag, logs, GTID, read-only state, version, filters, maintenance, and downtime.
3. Verify Raft quorum, a writable leader, and that `server.readOnly` is off.
4. Use `can-replicate-from` or its GTID check and state data-loss, cross-site, and traffic impact.
5. Record exact parameters and obtain the required change-window authorization.
6. Issue the operation once. After timeout/disconnect, read source, destination, topology, and audit before any retry.
7. Verify threads, upstream, coordinates/GTID, lag, a single writable primary, and application routing.
8. End maintenance/downtime and record audit and rollback outcome.

## Choosing a relocation method

| Scenario | Preferred capability | Command family | Main constraint |
| --- | --- | --- | --- |
| Move one sibling below another | GTID, then file:pos | `move-gtid`, `move-below`, `relocate` | Destination owns required compatible transactions |
| Move all replicas | GTID/smart relocation | `move-replicas-gtid`, `relocate-replicas` | Bound concurrency and bulk wait |
| Elect a local upstream | GTID/Pseudo-GTID | `regroup-replicas-*` | Review candidate and lagging replicas |
| Move one level up | file:pos/smart | `move-up`, `move-up-replicas` | Coordinates correlate and filters match |
| Change upstream, retain coordinates | file:pos | `repoint` | High risk if coordinates are wrong |
| Exchange primary/replica | planned switchover | `take-master` or graceful takeover | Freeze writes and fence old primary |

`relocate` chooses a mechanism from capabilities; convenience does not remove review. Use a specific mechanism when the change must be predictable and record why it was selected.

## Replication and instance state

- Start/stop/restart commands control replication threads, not application writes.
- `set-read-only` and `set-writeable` change MySQL global state; `super_read_only` behavior depends on configuration and version.
- `skip-query`, errant-GTID reset/injection, and reset replication change the recovery path and require transaction-level diagnosis.
- Flush/purge binlogs only after checking every replica, recovery requirement, and Pseudo-GTID retention.
- Semi-sync and delayed replication changes alter availability or protection guarantees.

## Maintenance, downtime, and tags

Maintenance marks an operator window and prevents automated recovery from racing the change. Downtime records known unavailability and affects problem/recovery handling. Always provide owner, reason, expected duration, and clear it explicitly. Tags aid selection but do not enforce safety; print the affected set before bulk untag, fuzzy pool, or alias changes.

## Failure and unknown outcome

Stop on stale discovery, lost quorum, errant GTID, filter mismatch, growing destination lag, two writable primaries, an aborting Hook, or unknown external routing. A timeout is an unknown result; retry only after readback proves no change occurred.

Rollback is rarely the inverse command. Fence write spread, locate the newest transactions, and plan from current coordinates. Use [Planned switchover](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Planned-Switchover) for primary moves and [Failure recovery](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Failure-Recovery) for failures.
