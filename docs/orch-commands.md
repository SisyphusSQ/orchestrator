# orch 能力与 HTTP 路由映射

基线：TOO-425 合并后的 `d6fe13d8`，原 Go 127 个目录项中 121 项迁为同名 HTTP 命令、5 项留在本地，1 个 `relocate-below` 别名并入 `relocate`；原 Shell 13 项补充能力和 5 项当前 Raft 管理操作一并纳入。历史别名不再注册。

路径相对 API 基础地址。`{instance}`/`{destination}` 展开为编码后的 `host/port`，其他占位符来自同名参数，`?` 表示可选。集群路径支持 `--cluster`、`--alias` 或 `-i` 的互斥提示。
查询列写出新参数与实际 HTTP query 名；POST 的 `--body` 为 JSON。每行输出为 Details/直接响应经过列出的投影；未列出投影时保留 Details/直接响应或成功消息。文本与 JSON、失败退出码及批量部分失败统一遵循 [客户端契约](orch.md)。各变更仅发送一次；失败不退回数据库，结果不确定时退出 3。查询命令也可能刷新服务端观测缓存，不代表完全没有服务端持久化。

| 命令 | HTTP 路径 | 查询参数/请求体 | 输出投影 | 类型 |
| --- | --- | --- | --- | --- |
| `relocate` | `GET relocate/{instance}/{destination}` |  |  | 变更 |
| `relocate-replicas` | `GET relocate-replicas/{instance}/{destination}` | `--pattern` → `pattern` |  | 变更 |
| `take-siblings` | `GET take-siblings/{instance}` |  |  | 变更 |
| `regroup-replicas` | `GET regroup-replicas/{instance}` |  |  | 变更 |
| `move-up` | `GET move-up/{instance}` |  |  | 变更 |
| `move-up-replicas` | `GET move-up-replicas/{instance}` | `--pattern` → `pattern` |  | 变更 |
| `move-below` | `GET move-below/{instance}/{destination}` |  |  | 变更 |
| `move-equivalent` | `GET move-equivalent/{instance}/{destination}` |  |  | 变更 |
| `repoint` | `GET repoint/{instance}/{destination?}` |  |  | 变更 |
| `repoint-replicas` | `GET repoint-replicas/{instance}` | `--pattern` → `pattern`；`--destination` → `destination` |  | 变更 |
| `take-master` | `GET take-master/{instance}` |  |  | 变更 |
| `make-co-master` | `GET make-co-master/{instance}` |  |  | 变更 |
| `get-candidate-replica` | `GET cli/get-candidate-replica/{instance}` |  |  | 查询 |
| `regroup-replicas-bls` | `GET regroup-replicas-bls/{instance}` |  |  | 变更 |
| `move-gtid` | `GET move-below-gtid/{instance}/{destination}` |  |  | 变更 |
| `move-replicas-gtid` | `GET move-replicas-gtid/{instance}/{destination}` | `--pattern` → `pattern` |  | 变更 |
| `regroup-replicas-gtid` | `GET regroup-replicas-gtid/{instance}` |  |  | 变更 |
| `match` | `GET match/{instance}/{destination}` |  |  | 变更 |
| `match-up` | `GET match-up/{instance}` |  |  | 变更 |
| `rematch` | `GET cli/rematch/{instance}` |  |  | 变更 |
| `match-replicas` | `GET match-replicas/{instance}/{destination}` | `--pattern` → `pattern` |  | 变更 |
| `match-up-replicas` | `GET match-up-replicas/{instance}` | `--pattern` → `pattern` |  | 变更 |
| `regroup-replicas-pgtid` | `GET regroup-replicas-pgtid/{instance}` |  |  | 变更 |
| `enable-gtid` | `GET enable-gtid/{instance}` |  |  | 变更 |
| `disable-gtid` | `GET disable-gtid/{instance}` |  |  | 变更 |
| `which-gtid-errant` | `GET instance/{instance}` |  | GtidErrant | 查询 |
| `gtid-errant-reset-master` | `GET gtid-errant-reset-master/{instance}` |  |  | 变更 |
| `skip-query` | `GET skip-query/{instance}` |  |  | 变更 |
| `stop-replica` | `GET stop-replica/{instance}` |  |  | 变更 |
| `start-replica` | `GET start-replica/{instance}` |  |  | 变更 |
| `restart-replica` | `GET restart-replica/{instance}` |  |  | 变更 |
| `reset-replica` | `GET reset-replica/{instance}` |  |  | 变更 |
| `change-master-credentials` | `GET change-master-credentials/{instance}` |  |  | 变更 |
| `detach-replica-master-host` | `GET detach-replica-master-host/{instance}` |  |  | 变更 |
| `reattach-replica-master-host` | `GET reattach-replica-master-host/{instance}` |  |  | 变更 |
| `master-pos-wait` | `GET cli/master-pos-wait/{instance}` | `--binlog` → `binlog`（必填） |  | 查询 |
| `enable-semi-sync-master` | `GET enable-semi-sync-master/{instance}` |  |  | 变更 |
| `disable-semi-sync-master` | `GET disable-semi-sync-master/{instance}` |  |  | 变更 |
| `enable-semi-sync-replica` | `GET enable-semi-sync-replica/{instance}` |  |  | 变更 |
| `disable-semi-sync-replica` | `GET disable-semi-sync-replica/{instance}` |  |  | 变更 |
| `restart-replica-statements` | `GET restart-replica-statements/{instance}` | `--statement` → `q`（必填） |  | 查询 |
| `can-replicate-from` | `GET can-replicate-from/{instance}/{destination}` |  |  | 查询 |
| `is-replicating` | `GET instance/{instance}` |  | replicating | 查询 |
| `is-replication-stopped` | `GET instance/{instance}` |  | stopped | 查询 |
| `set-read-only` | `GET set-read-only/{instance}` |  |  | 变更 |
| `set-writeable` | `GET set-writeable/{instance}` |  |  | 变更 |
| `flush-binary-logs` | `GET flush-binary-logs/{instance}` | `--binlog` → `binlog` |  | 变更 |
| `purge-binary-logs` | `GET purge-binary-logs/{instance}/{binlog}` |  |  | 变更 |
| `last-pseudo-gtid` | `GET last-pseudo-gtid/{instance}` | `--strict` → `strict` |  | 查询 |
| `locate-gtid-errant` | `GET locate-gtid-errant/{instance}` |  |  | 查询 |
| `last-executed-relay-entry` | `GET cli/last-executed-relay-entry/{instance}` |  |  | 查询 |
| `correlate-relaylog-pos` | `GET cli/correlate-relaylog-pos/{instance}/{destination}` | `--binlog` → `binlog` |  | 查询 |
| `find-binlog-entry` | `GET cli/find-binlog-entry/{instance}` | `--pattern` → `pattern`（必填） |  | 查询 |
| `correlate-binlog-pos` | `GET cli/correlate-binlog-pos/{instance}/{destination}` | `--binlog` → `binlog` |  | 查询 |
| `submit-pool-instances` | `GET submit-pool-instances/{pool}` | `--instances` → `instances`（必须显式提供，可为空以清空池） |  | 变更 |
| `cluster-pool-instances` | `GET cli/cluster-pool-instances` | `--cluster` → `cluster`；`--pool` → `pool` |  | 查询 |
| `which-heuristic-cluster-pool-instances` | `GET heuristic-cluster-pool-instances/{cluster}/{pool?}` |  |  | 查询 |
| `find` | `GET cli/find` | `--pattern` → `pattern`（必填） |  | 查询 |
| `search` | `GET search` | `--search` → `s`（必填） |  | 查询 |
| `clusters` | `GET clusters` |  |  | 查询 |
| `clusters-alias` | `GET clusters-info` |  | cluster-aliases | 查询 |
| `all-clusters-masters` | `GET masters` |  |  | 查询 |
| `topology` | `GET topology/{cluster}` |  |  | 查询 |
| `topology-tabulated` | `GET topology-tabulated/{cluster}` |  |  | 查询 |
| `topology-tags` | `GET topology-tags/{cluster}` |  |  | 查询 |
| `all-instances` | `GET all-instances` |  |  | 查询 |
| `which-instance` | `GET instance/{instance}` |  | Key | 查询 |
| `which-cluster` | `GET cluster-info/{cluster}` |  | ClusterName | 查询 |
| `which-cluster-alias` | `GET cluster-info/{cluster}` |  | ClusterAlias | 查询 |
| `which-cluster-domain` | `GET cluster-info/{cluster}` |  | ClusterDomain | 查询 |
| `which-heuristic-domain-instance` | `GET cli/which-heuristic-domain-instance/{cluster}` |  |  | 查询 |
| `which-cluster-master` | `GET master/{cluster}` |  |  | 查询 |
| `which-cluster-instances` | `GET cluster/{cluster}` |  |  | 查询 |
| `which-cluster-osc-replicas` | `GET cluster-osc-replicas/{cluster}` |  |  | 查询 |
| `which-cluster-gh-ost-replicas` | `GET cli/which-cluster-gh-ost-replicas/{cluster}` |  |  | 查询 |
| `which-master` | `GET instance/{instance}` |  | MasterKey | 查询 |
| `which-downtimed-instances` | `GET downtimed/{cluster?}` |  |  | 查询 |
| `which-replicas` | `GET instance-replicas/{instance}` |  |  | 查询 |
| `which-lost-in-recovery` | `GET cli/which-lost-in-recovery` |  |  | 查询 |
| `instance-status` | `GET cli/instance-status/{instance}` |  |  | 查询 |
| `get-cluster-heuristic-lag` | `GET cli/get-cluster-heuristic-lag/{cluster}` |  |  | 查询 |
| `submit-masters-to-kv-stores` | `GET submit-masters-to-kv-stores/{cluster?}` |  |  | 变更 |
| `tags` | `GET tags/{instance}` |  |  | 查询 |
| `tag-value` | `GET tag-value/{instance}` | `--tag` → `tag`（必填） |  | 查询 |
| `tagged` | `GET tagged` | `--tag` → `tag`（必填） |  | 查询 |
| `tag` | `GET tag/{instance}` | `--tag` → `tag`（必填） |  | 变更 |
| `untag` | `GET untag/{instance}` | `--tag` → `tag`（必填） |  | 变更 |
| `untag-all` | `GET untag-all` | `--tag` → `tag`（必填） |  | 变更 |
| `discover` | `GET discover/{instance}` |  |  | 变更 |
| `forget` | `GET forget/{instance}` |  |  | 变更 |
| `begin-maintenance` | `GET begin-maintenance/{instance}/{owner}/{reason}` | `--duration` → `duration` |  | 变更 |
| `end-maintenance` | `GET end-maintenance/{instance}` |  |  | 变更 |
| `in-maintenance` | `GET in-maintenance/{instance}` |  |  | 查询 |
| `begin-downtime` | `GET begin-downtime/{instance}/{owner}/{reason}/{duration?}` |  |  | 变更 |
| `end-downtime` | `GET end-downtime/{instance}` |  |  | 变更 |
| `recover` | `GET recover/{instance}/{destination?}` |  |  | 变更 |
| `recover-lite` | `GET recover-lite/{instance}/{destination?}` |  |  | 变更 |
| `force-master-failover` | `GET force-master-failover/{cluster}` |  |  | 变更 |
| `force-master-takeover` | `GET force-master-takeover/{cluster}/{destination}` |  |  | 变更 |
| `graceful-master-takeover` | `GET graceful-master-takeover/{cluster}/{destination?}` |  |  | 变更 |
| `graceful-master-takeover-auto` | `GET graceful-master-takeover-auto/{cluster}/{destination?}` |  |  | 变更 |
| `replication-analysis` | `GET replication-analysis` | `--include-downtimed` → `includeDowntimed` |  | 查询 |
| `ack-all-recoveries` | `GET ack-all-recoveries` | `--reason` → `comment`（必填） |  | 变更 |
| `ack-cluster-recoveries` | `GET ack-recovery/cluster/{cluster}` | `--reason` → `comment`（必填） |  | 变更 |
| `ack-instance-recoveries` | `GET ack-recovery/instance/{instance}` | `--reason` → `comment`（必填） |  | 变更 |
| `register-candidate` | `GET register-candidate/{instance}/{promotion-rule}` |  |  | 变更 |
| `register-hostname-unresolve` | `GET register-hostname-unresolve/{instance}/{hostname}` |  |  | 变更 |
| `deregister-hostname-unresolve` | `GET deregister-hostname-unresolve/{instance}` |  |  | 变更 |
| `set-heuristic-domain-instance` | `GET cli/set-heuristic-domain-instance/{cluster}` |  |  | 变更 |
| `snapshot-topologies` | `GET snapshot-topologies` |  |  | 变更 |
| `active-nodes` | `GET cli/active-nodes` |  |  | 查询 |
| `resolve` | `GET resolve/{instance}` |  |  | 查询 |
| `reset-hostname-resolve-cache` | `GET reset-hostname-resolve-cache` |  |  | 变更；指定单节点 |
| `show-resolve-hosts` | `GET cli/show-resolve-hosts` |  |  | 查询 |
| `show-unresolve-hosts` | `GET cli/show-unresolve-hosts` |  |  | 查询 |
| `custom-command` | `GET agent-custom-command/{hostname}/{pattern}` |  |  | 变更 |
| `disable-global-recoveries` | `GET disable-global-recoveries` |  |  | 变更 |
| `enable-global-recoveries` | `GET enable-global-recoveries` |  |  | 变更 |
| `check-global-recoveries` | `GET check-global-recoveries` |  |  | 查询 |
| `bulk-instances` | `GET bulk-instances` |  |  | 查询 |
| `bulk-promotion-rules` | `GET bulk-promotion-rules` |  |  | 查询 |
| `async-discover` | `GET async-discover/{instance}` |  |  | 变更 |
| `forget-cluster` | `GET forget-cluster/{cluster}` |  |  | 变更 |
| `can-replicate-from-gtid` | `GET can-replicate-from-gtid/{instance}/{destination}` |  |  | 查询 |
| `stop-replica-nice` | `GET stop-replica-nice/{instance}` |  |  | 变更 |
| `delay-replication` | `GET delay-replication/{instance}/{seconds}` |  |  | 变更 |
| `which-broken-replicas` | `GET instance-replicas/{instance}` |  | broken-replicas | 查询 |
| `which-cluster-osc-running-replicas` | `GET cluster-osc-replicas/{cluster}` |  | running-replicas | 查询 |
| `dominant-dc` | `GET masters` |  | dominant-dc | 查询 |
| `raft-leader` | `GET raft-leader` |  |  | 查询 |
| `raft-health` | `GET raft-health` |  |  | 查询 |
| `raft-leader-hostname` | `GET raft-leader` |  | leader-hostname | 查询 |
| `raft-configuration` | `GET raft/configuration` |  |  | 查询；指定单节点 |
| `raft-bootstrap` | `POST raft/bootstrap` | `--body`（可选） |  | 变更；指定单节点 |
| `raft-add-member` | `POST raft/members` | `--body`（必填） |  | 变更 |
| `raft-remove-member` | `DELETE raft/members/{id}` |  |  | 变更 |
| `raft-transfer-leadership` | `POST raft/leadership/transfer` | `--body`（可选） |  | 变更 |
| `raft-snapshot` | `POST raft/snapshot` | `--body`（可选） |  | 变更；指定单节点 |

| `gtid-errant-inject-empty` | `GET gtid-errant-inject-empty/{instance}` |  |  | 变更 |

## 本地入口与客户端辅助命令

| 原能力 | 新入口 | 运行边界 |
| --- | --- | --- |
| `continuous` | 已移除，使用 `orchestrator server` | Raft 与 HTTP 管理统一启动，默认启用后台发现 |
| `dump-config` | `orchestrator admin dump-config` | 仅本地输出服务配置，可能包含凭据 |
| `redeploy-internal-db` | `orchestrator admin redeploy-internal-db` | 仅本地部署内部库结构 |
| `access-token` | `orchestrator admin access-token --owner` | 仅本地签发访问凭据 |
| `internal-suggest-promoted-replacement` | 隐藏的 `orchestrator admin suggest-promoted-replacement -i ... -d ...` | 内部测试辅助，不提供远程 API |
| `http` | `orchestrator server` | HTTP/Web 服务，可选择关闭后台发现 |
| Shell `api` | `orch api PATH --method METHOD --body JSON` | 原始 HTTP JSON 响应，只接受一个 endpoint，不自动重放 |
| Shell `which-api` | `orch which-api` | 只读探测，输出选中 API 地址 |

Shell 原名称也逐项收口：`instance` → `which-instance`，`downtimed` → `which-downtimed-instances`，`detach-replica` / `reattach-replica` → `detach-replica-master-host` / `reattach-replica-master-host`，`raft-elect-leader --hostname ID` → `raft-transfer-leadership --body '{"id":"ID"}'`；以上不保留别名。`help` 由 Cobra 提供。

## 参数与语义迁移

- 原本机实例猜测改为显式 `-i`；实例默认端口固定为 3306，客户端不读取服务配置。
- 原 owner 隐式本机用户名改为操作命令显式 `--owner`；集群提示与实例目的地使用明确参数。
- 保留 `repoint` 的可选目的地、`repoint-replicas --destination`、`flush-binary-logs --binlog`、`last-pseudo-gtid --strict`、`begin-maintenance --duration`；持续时间使用非负整数和 s/m/h/d/w 后缀。
- `cluster-pool-instances` 默认列出所有集群池，支持 `--cluster` / `--pool` 过滤；`replication-analysis` 默认排除停机实例，可用 `--include-downtimed` 纳入。
- `can-replicate-from` 不再吞掉业务拒绝；不存在实例、版本或 SQL 延迟不允许复制均返回失败。`is-replicating`、`is-replication-stopped` 输出布尔值；空字符串的文本输出为空，JSON 仍输出字符串。
- 原 `--noop`、`--skip-unresolve`、`--skip-unresolve-check`、`--skip-binlog-search` 是业务运行参数，统一由服务端启动控制，不由远程请求改写进程全局状态。配置/日志/启动选项属于服务端；旧 `--ignore-raft-setup`、`--skip-continuous-registration` 随直连入口移除，客户端不会启动这些运行时。
- 输出格式统一定义，不保留旧脚本按空行、日志或别名判断结果的约定；原恢复内部算法不移入客户端。
