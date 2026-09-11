# orch CLI 命令参考
[English](https://github.com/SisyphusSQ/orchestrator/wiki/EN-CLI-Reference) · **中文** · [Wiki 首页](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

本页索引当前 [`catalog.json`](https://github.com/SisyphusSQ/orchestrator/blob/main/tools/orch-cli/internal/cmd/catalog.json) 的 139 个 HTTP 命令。`R` 表示 catalog 声明为读取，`W` 表示声明为可能产生副作用，`M` 表示使用明确 HTTP method 的 Raft 命令。最终参数、必需 selector 与输出以同版本 `orch help <command>` 为准。

## 全局参数和 selector

所有命令共享 endpoint、认证/TLS、timeout 与 `--output text|json`。常见 selector 为 `--instance host[:port]`、`--destination host[:port]`、互斥的 `--cluster`/`--alias`/实例提示、`--owner`、`--reason`、`--duration`、`--pattern` 和 `--body`。自动化固定使用 `--output json`，显式设置 timeout，并记录 endpoint 与命令版本。

## 安全分类

- `R` 仍可能造成发现查询、元数据库读取或较重的 binlog 扫描，不代表无成本。
- `W` 只能发送一次；超时/断连返回未知结果时先回读。
- `M` 中 bootstrap/snapshot 是节点本地操作，成员和领导变更有各自 POST/DELETE 契约。
- `discover`、`snapshot-topologies` 等虽不直接改变 MySQL 复制，也会改变 orchestrator 状态，因此归为 `W`。

## 完整命令索引

| 组 | 命令 |
| --- | --- |
| `Agent` | `custom-command` (W) |
| `Binary logs` | `flush-binary-logs` (W)、`purge-binary-logs` (W)、`last-pseudo-gtid` (R)、`locate-gtid-errant` (R)、`last-executed-relay-entry` (R)、`correlate-relaylog-pos` (R)、`find-binlog-entry` (R)、`correlate-binlog-pos` (R) |
| `Binlog server relocation` | `regroup-replicas-bls` (W) |
| `Classic file:pos relocation` | `move-up` (W)、`move-up-replicas` (W)、`move-below` (W)、`move-equivalent` (W)、`repoint` (W)、`repoint-replicas` (W)、`take-master` (W)、`make-co-master` (W)、`get-candidate-replica` (R) |
| `GTID` | `gtid-errant-inject-empty` (W) |
| `GTID relocation` | `move-gtid` (W)、`move-replicas-gtid` (W)、`regroup-replicas-gtid` (W) |
| `Information` | `find` (R)、`search` (R)、`clusters` (R)、`clusters-alias` (R)、`all-clusters-masters` (R)、`topology` (R)、`topology-tabulated` (R)、`topology-tags` (R)、`all-instances` (R)、`which-instance` (R)、`which-cluster` (R)、`which-cluster-alias` (R)、`which-cluster-domain` (R)、`which-heuristic-domain-instance` (R)、`which-cluster-master` (R)、`which-cluster-instances` (R)、`which-cluster-osc-replicas` (R)、`which-cluster-gh-ost-replicas` (R)、`which-master` (R)、`which-downtimed-instances` (R)、`which-replicas` (R)、`which-lost-in-recovery` (R)、`instance-status` (R)、`get-cluster-heuristic-lag` (R) |
| `Instance` | `set-read-only` (W)、`set-writeable` (W) |
| `Instance management` | `discover` (W)、`forget` (W)、`begin-maintenance` (W)、`end-maintenance` (W)、`in-maintenance` (R)、`begin-downtime` (W)、`end-downtime` (W) |
| `Instance, meta` | `register-candidate` (W)、`register-hostname-unresolve` (W)、`deregister-hostname-unresolve` (W)、`set-heuristic-domain-instance` (W) |
| `Key-value` | `submit-masters-to-kv-stores` (W) |
| `Meta` | `snapshot-topologies` (W)、`active-nodes` (R)、`resolve` (R)、`reset-hostname-resolve-cache` (W)、`show-resolve-hosts` (R)、`show-unresolve-hosts` (R) |
| `Other` | `disable-global-recoveries` (W)、`enable-global-recoveries` (W)、`check-global-recoveries` (R)、`bulk-instances` (R)、`bulk-promotion-rules` (R)、`async-discover` (W)、`forget-cluster` (W)、`can-replicate-from-gtid` (R)、`stop-replica-nice` (W)、`delay-replication` (W)、`which-broken-replicas` (R)、`which-cluster-osc-running-replicas` (R)、`dominant-dc` (R)、`raft-leader` (R)、`raft-health` (R)、`raft-leader-hostname` (R)、`raft-configuration` (R) |
| `Pools` | `submit-pool-instances` (W)、`cluster-pool-instances` (R)、`which-heuristic-cluster-pool-instances` (R) |
| `Pseudo-GTID relocation` | `match` (W)、`match-up` (W)、`rematch` (W)、`match-replicas` (W)、`match-up-replicas` (W)、`regroup-replicas-pgtid` (W) |
| `Raft` | `raft-bootstrap` (M)、`raft-add-member` (M)、`raft-remove-member` (M)、`raft-transfer-leadership` (M)、`raft-snapshot` (M) |
| `Recovery` | `recover` (W)、`recover-lite` (W)、`force-master-failover` (W)、`force-master-takeover` (W)、`graceful-master-takeover` (W)、`graceful-master-takeover-auto` (W)、`replication-analysis` (R)、`ack-all-recoveries` (W)、`ack-cluster-recoveries` (W)、`ack-instance-recoveries` (W) |
| `Replication information` | `can-replicate-from` (R)、`is-replicating` (R)、`is-replication-stopped` (R) |
| `Replication, general` | `enable-gtid` (W)、`disable-gtid` (W)、`which-gtid-errant` (R)、`gtid-errant-reset-master` (W)、`skip-query` (W)、`stop-replica` (W)、`start-replica` (W)、`restart-replica` (W)、`reset-replica` (W)、`change-master-credentials` (W)、`detach-replica-master-host` (W)、`reattach-replica-master-host` (W)、`master-pos-wait` (R)、`enable-semi-sync-master` (W)、`disable-semi-sync-master` (W)、`enable-semi-sync-replica` (W)、`disable-semi-sync-replica` (W)、`restart-replica-statements` (R) |
| `Smart relocation` | `relocate` (W)、`relocate-replicas` (W)、`take-siblings` (W)、`regroup-replicas` (W) |
| `tags` | `tags` (R)、`tag-value` (R)、`tagged` (R)、`tag` (W)、`untag` (W)、`untag-all` (W) |

## 典型读取链

```sh
orch clusters --output json
orch topology --cluster production --output json
orch instance-status --instance db1.example:3306 --output json
orch replication-analysis --output json
orch raft-configuration --output json
```

## 典型变更链

变更前使用读取命令保存源/目标状态，进入 maintenance，再执行一个明确机制的操作，最后重复读取并检查 audit。主库迁移使用计划切换 runbook；恢复命令不作为日常拓扑整理捷径。

退出码：`0` 确认成功；`1` HTTP/认证/业务/输出失败；`2` 本地用法错误；`3` 写结果未知；`4` 批量部分成功。任何非零都不得仅通过重复相同写命令处理。
