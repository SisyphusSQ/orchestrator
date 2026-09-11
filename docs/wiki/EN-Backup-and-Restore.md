# Backup, restore, and disaster handling
**English** · [中文](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Backup-and-Restore) · [Wiki home](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

There is no single “cluster backup” that covers every failure. Protect Raft state, each node's metadata database, configuration and secret references, audit/recovery evidence, and managed MySQL independently. A Raft snapshot is neither a MySQL backup nor a metadata backup.

## Assets and recovery objectives

| Asset | Contains | Does not contain | Purpose |
| --- | --- | --- | --- |
| `raft.dataDir` | Consensus log, identity, snapshots | Metadata and MySQL data | Recover the same member or preserve evidence |
| Per-node metadata | Discovery, aliases, audit, recovery records, page settings | Membership and live MySQL state | Rebuild application state for that node |
| Config/certificates | Identity, addresses, policy, credential references | Secrets stored elsewhere | Reproduce policy |
| MySQL backup | Business data and replication recovery base | Orchestrator decisions | Database disaster recovery |
| External KV/routing | Application writer entry | MySQL and Raft state | Verify fencing and writer routing |

Define RPO/RTO, retention, encryption, off-site copies, ownership, and restore drills. A green backup job without an actual restore is not sufficient evidence.

## Routine backup

1. Record version, commit, node IDs, configuration index, leader, membership, and health.
2. Request `raft-snapshot` through the leader and verify both the response and snapshot artifact; this advances only the Raft snapshot.
3. Back up each metadata database using an engine-consistent method. Use supported logical/physical MySQL backup. Do not copy a live SQLite file; use the backup API, controlled shutdown, or a consistent volume snapshot.
4. When backing up full Raft directories, use an application-safe filesystem consistency point. Do not copy only `raft.db` or only `snapshots/`.
5. Store redacted configuration, certificate chain, systemd/container/proxy definitions, and dependency inventory. Restore secrets separately from the secret manager.
6. Checksum, encrypt, and replicate backups off-site; document restore permissions.

The domains may have different timestamps. Rediscover live MySQL after restore; never assume a Raft snapshot and metadata dump are transactionally aligned.

## Single-node failure

With a healthy majority, replace the failed member:

1. Verify the remaining voters have quorum and remove traffic from the failed node.
2. Preserve the failed disk for evidence; do not immediately reuse its ID.
3. Start a replacement with a new stable ID, clean Raft directory, and independent metadata.
4. Add it through the leader and verify committed membership, catch-up, and readiness.
5. Remove the old ID and read configuration from every node.

Restore original member state only when the Raft directory, `node-id`, and configuration are proven consistent. Never clone another member's Raft directory; that clones identity.

## Metadata corruption

Isolate the node from management traffic. Restore its independent metadata, verify schema/migrations, then restart and let discovery refresh live topology. Page-managed recovery settings, Hooks, aliases, and audit live in metadata; an older restore rolls them back. Read effective policy before enabling recovery.

Do not online-copy another member's SQLite file. MySQL metadata still requires engine-consistent backup; Raft does not make that optional.

## Lost quorum

Stop topology writes and automatic recovery. Do not bypass Raft with direct metadata writes. Diagnose partition, certificate/address, disk, and permanent-member loss and protect every surviving state copy.

Prefer restoring enough original voters to regain the original majority, confirming a single leader, and validating membership/log state. Rebuild a new cluster only with explicit disaster authorization when the original majority cannot return. Select one authoritative MySQL/metadata state, recreate membership, rediscover, reapply page settings, and fence the old cluster so it cannot reappear as a second control plane.

## Restore-drill acceptance

- Restore on an isolated network with no production write path.
- IDs, addresses, and configuration index match expectations with no duplicate identity.
- Metadata schema and recovery settings, Hooks, aliases, maintenance/downtime, and audit scope are checked.
- Rediscovery yields correct edges, candidates, GTID/coordinates, and read-only state.
- Web/API/CLI reads work; one controlled write executes once and is audited.
- External KV, proxy, and application writer agree with the actual writable MySQL primary.
- Record achieved RPO/RTO, data loss, manual steps, and improvements.
