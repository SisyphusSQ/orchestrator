# orch CLI command reference
**English** · [中文](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-CLI-Reference) · [Wiki home](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

This page indexes all 139 HTTP commands in the current [`catalog.json`](https://github.com/SisyphusSQ/orchestrator/blob/main/tools/orch-cli/internal/cmd/catalog.json). `R` is catalogued read-only, `W` may cause side effects, and `M` is a Raft command with an explicit HTTP method. Exact flags, selectors, and output come from the same version's `orch help <command>`.

## Global flags and selectors

Commands share endpoint, auth/TLS, timeout, and `--output text|json`. Common selectors include `--instance`, `--destination`, mutually exclusive cluster/alias/instance hints, owner, reason, duration, pattern, and body. Automation should use JSON, set timeout explicitly, and record endpoint and client version.

## Safety classification

- `R` may still perform discovery queries, metadata reads, or expensive binlog scans.
- Send `W` once; read back after timeout or disconnect.
- `M` distinguishes local bootstrap/snapshot from POST/DELETE membership and leadership methods.
- `discover` and `snapshot-topologies` change orchestrator state even when they do not directly change MySQL replication.

## Complete command index

| Group | Commands |
| --- | --- |
| `Agent` | `custom-command` (W) |
| `Binary logs` | `flush-binary-logs` (W), `purge-binary-logs` (W), `last-pseudo-gtid` (R), `locate-gtid-errant` (R), `last-executed-relay-entry` (R), `correlate-relaylog-pos` (R), `find-binlog-entry` (R), `correlate-binlog-pos` (R) |
| `Binlog server relocation` | `regroup-replicas-bls` (W) |
| `Classic file:pos relocation` | `move-up` (W), `move-up-replicas` (W), `move-below` (W), `move-equivalent` (W), `repoint` (W), `repoint-replicas` (W), `take-master` (W), `make-co-master` (W), `get-candidate-replica` (R) |
| `GTID` | `gtid-errant-inject-empty` (W) |
| `GTID relocation` | `move-gtid` (W), `move-replicas-gtid` (W), `regroup-replicas-gtid` (W) |
| `Information` | `find` (R), `search` (R), `clusters` (R), `clusters-alias` (R), `all-clusters-masters` (R), `topology` (R), `topology-tabulated` (R), `topology-tags` (R), `all-instances` (R), `which-instance` (R), `which-cluster` (R), `which-cluster-alias` (R), `which-cluster-domain` (R), `which-heuristic-domain-instance` (R), `which-cluster-master` (R), `which-cluster-instances` (R), `which-cluster-osc-replicas` (R), `which-cluster-gh-ost-replicas` (R), `which-master` (R), `which-downtimed-instances` (R), `which-replicas` (R), `which-lost-in-recovery` (R), `instance-status` (R), `get-cluster-heuristic-lag` (R) |
| `Instance` | `set-read-only` (W), `set-writeable` (W) |
| `Instance management` | `discover` (W), `forget` (W), `begin-maintenance` (W), `end-maintenance` (W), `in-maintenance` (R), `begin-downtime` (W), `end-downtime` (W) |
| `Instance, meta` | `register-candidate` (W), `register-hostname-unresolve` (W), `deregister-hostname-unresolve` (W), `set-heuristic-domain-instance` (W) |
| `Key-value` | `submit-masters-to-kv-stores` (W) |
| `Meta` | `snapshot-topologies` (W), `active-nodes` (R), `resolve` (R), `reset-hostname-resolve-cache` (W), `show-resolve-hosts` (R), `show-unresolve-hosts` (R) |
| `Other` | `disable-global-recoveries` (W), `enable-global-recoveries` (W), `check-global-recoveries` (R), `bulk-instances` (R), `bulk-promotion-rules` (R), `async-discover` (W), `forget-cluster` (W), `can-replicate-from-gtid` (R), `stop-replica-nice` (W), `delay-replication` (W), `which-broken-replicas` (R), `which-cluster-osc-running-replicas` (R), `dominant-dc` (R), `raft-leader` (R), `raft-health` (R), `raft-leader-hostname` (R), `raft-configuration` (R) |
| `Pools` | `submit-pool-instances` (W), `cluster-pool-instances` (R), `which-heuristic-cluster-pool-instances` (R) |
| `Pseudo-GTID relocation` | `match` (W), `match-up` (W), `rematch` (W), `match-replicas` (W), `match-up-replicas` (W), `regroup-replicas-pgtid` (W) |
| `Raft` | `raft-bootstrap` (M), `raft-add-member` (M), `raft-remove-member` (M), `raft-transfer-leadership` (M), `raft-snapshot` (M) |
| `Recovery` | `recover` (W), `recover-lite` (W), `force-master-failover` (W), `force-master-takeover` (W), `graceful-master-takeover` (W), `graceful-master-takeover-auto` (W), `replication-analysis` (R), `ack-all-recoveries` (W), `ack-cluster-recoveries` (W), `ack-instance-recoveries` (W) |
| `Replication information` | `can-replicate-from` (R), `is-replicating` (R), `is-replication-stopped` (R) |
| `Replication, general` | `enable-gtid` (W), `disable-gtid` (W), `which-gtid-errant` (R), `gtid-errant-reset-master` (W), `skip-query` (W), `stop-replica` (W), `start-replica` (W), `restart-replica` (W), `reset-replica` (W), `change-master-credentials` (W), `detach-replica-master-host` (W), `reattach-replica-master-host` (W), `master-pos-wait` (R), `enable-semi-sync-master` (W), `disable-semi-sync-master` (W), `enable-semi-sync-replica` (W), `disable-semi-sync-replica` (W), `restart-replica-statements` (R) |
| `Smart relocation` | `relocate` (W), `relocate-replicas` (W), `take-siblings` (W), `regroup-replicas` (W) |
| `tags` | `tags` (R), `tag-value` (R), `tagged` (R), `tag` (W), `untag` (W), `untag-all` (W) |

## Read chain

```sh
orch clusters --output json
orch topology --cluster production --output json
orch instance-status --instance db1.example:3306 --output json
orch replication-analysis --output json
orch raft-configuration --output json
```

For mutations, save source/destination reads, enter maintenance, issue one mechanism-specific command, then repeat reads and inspect audit. Use the planned-switchover runbook for primary moves; recovery commands are not topology-cleanup shortcuts.

Exit codes are 0 confirmed success, 1 HTTP/auth/business/render failure, 2 local usage, 3 unknown mutation, and 4 partial batch. Never handle a nonzero code by blindly repeating the same write.
