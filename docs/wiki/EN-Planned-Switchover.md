# Planned primary switchover

**English** · [中文](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Planned-Switchover) · [Wiki home](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

A planned switchover moves writes from a healthy primary to a direct replica without pretending that the primary has failed. It is a coordinated database and application change: orchestrator can reshape replication and promote the target, but traffic routing, connection draining, fencing, and application acceptance remain external responsibilities.

## Choose the command deliberately

| Command | Target selection | State of the old primary after repointing |
| --- | --- | --- |
| `orch graceful-master-takeover --cluster production --destination candidate.example.com:3306` | Uses the named direct replica. Without `--destination`, it proceeds only when the primary has exactly one direct replica. | Repointed below the new primary with replication stopped, so the operator controls when it rejoins. |
| `orch graceful-master-takeover-auto --cluster production` | Chooses a candidate when none is named. A named destination is also accepted and still uses auto completion behavior. | Attempts to start replication after repointing the old primary. |

Use the explicit destination form for a controlled production change. Use `-auto` only when automatic rejoin of the old primary is intended; if `--destination` is omitted, automatic candidate selection must also be acceptable. `force-master-takeover` and `force-master-failover` deliberately discard the current primary and are incident commands, not shortcuts for planned maintenance.

## Preconditions

Before opening the change window, verify all of the following:

1. The cluster resolves to exactly one primary and the intended target is its direct replica.
2. The target is not excluded by `must_not`, another promotion rule, or `PromotionIgnoreHostnameFilters`.
3. Replication is running, the target has reasonable maintenance lag, and version, GTID/Pseudo-GTID, filters, credentials, and TLS behavior are compatible.
4. Sibling replicas can be relocated below the target, or any intentionally unavailable sibling is already marked as downtime and its omission is accepted.
5. Raft has a healthy leader and quorum; no active recovery, anti-flapping block, or conflicting automation is operating on the cluster.
6. `PreGracefulTakeoverProcesses` and `PostGracefulTakeoverProcesses` have bounded runtime, durable logs, least privilege, and clear failure ownership.
7. The application owner has a traffic-drain/fencing plan, an abort point, write-path checks, and a rollback decision tree. Backups are current and restorable.

Collect a before-state record:

```sh
orch which-cluster-master --cluster production
orch topology --cluster production
orch replication-analysis --output json
orch is-replicating --instance candidate.example.com:3306
orch raft-configuration --output json
```

Run `orch help graceful-master-takeover` against the binary being deployed. Do not copy a command from this page without confirming the current endpoint, cluster, instance ports, and maintenance approval.

## What the server actually does

The current implementation executes these phases in order:

1. Resolve exactly one cluster primary, list its direct replicas, and select the designated instance.
2. Validate that a named target is a direct replica, eligible for promotion, still attached to the resolved primary, and within the reasonable maintenance lag threshold. Auto selection also attempts to start the selected replica first.
3. If the primary has siblings, relocate them below the designated replica. Failure to relocate a non-downtimed sibling aborts the takeover; an unavailable downtimed sibling may be left behind.
4. Create the forced analysis context and run `PreGracefulTakeoverProcesses`.
5. Set the old primary `read_only`, record its exact binlog coordinates, and wait for the designated replica to execute through those coordinates.
6. Run the normal recovery machinery with the designated successor, promote it, and record the recovery.
7. Repoint the old primary below the new primary, restore replication credentials when needed, and enable public-key retrieval where the current TLS capability requires it.
8. In `-auto` mode, attempt to start replication on the old primary. Run `PostGracefulTakeoverProcesses` after the topology change; a post-hook failure does not roll back the topology and is not returned as the command's topology result.

Sibling relocation happens before the pre-takeover hook and before the old primary becomes read-only. Therefore, even an early abort can leave a deliberately reshaped replica tree; always read back topology instead of assuming “nothing changed.”

## Execute and read back

After application write draining and the agreed fencing point, run one command once:

```sh
orch graceful-master-takeover \
  --cluster production \
  --destination candidate.example.com:3306 \
  --output json
```

Then independently verify the database and control-plane result:

```sh
orch which-cluster-master --cluster production
orch topology --cluster production
orch is-replicating --instance old-primary.example.com:3306
orch replication-analysis --output json
orch raft-configuration --output json
```

For the non-auto command, inspect the old primary before explicitly rejoining it:

```sh
orch start-replica --instance old-primary.example.com:3306
orch is-replicating --instance old-primary.example.com:3306
```

Also verify from MySQL that the new primary accepts writes, the old primary is read-only and follows the expected source, all retained replicas follow the intended tree, and no errant transactions appeared. Only then update routing and validate representative application writes and reads. Check the recovery record, audit log, both hooks, metrics, and alerts as separate evidence.

## Failure and abort semantics

- A precondition or pre-hook failure stops later phases, but siblings may already have been relocated.
- A catch-up timeout or another error after `read_only` can leave the old primary read-only. The implementation only makes a best-effort writable restoration in the specific branch where recovery returned no successor; there is no general transactional rollback.
- Promotion may succeed while repointing, credential restoration, or starting the old primary fails. A post-hook failure does not undo the change and may require hook/audit logs to detect. Never infer the topology from the client result alone.
- Client exit code 3 or a lost response means the mutation result is unknown. Do not replay immediately; read the primary, topology, recovery, audit, and MySQL state first.
- Do not make both old and new primaries writable to “recover quickly.” Fence application writes until one authoritative primary and its downstream topology have been proven.

If the change must be reversed after promotion, treat it as another planned switchover after the topology is stable and evidence is collected. Do not improvise a partial reversal by editing metadata, deleting Raft state, or running a forced failover.
