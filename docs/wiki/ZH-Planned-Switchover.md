# 计划内主库切换

[English](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Planned-Switchover) · **中文** · [Wiki 首页](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

计划切换是在主库健康时，把写入职责迁到直属副本，而不是伪造主库故障。这是一项数据库与应用协同变更：orchestrator 可以调整复制树并提升目标节点，但流量路由、连接排空、fencing 和应用验收仍由外部系统负责。

## 有意识地选择命令

| 命令 | 目标选择 | 旧主库 repoint 后的状态 |
| --- | --- | --- |
| `orch graceful-master-takeover --cluster production --destination candidate.example.com:3306` | 使用指定直属副本。不传 `--destination` 时，只有主库恰好存在一个直属副本才继续。 | repoint 到新主库下方但保持复制停止，由运维决定何时重新加入。 |
| `orch graceful-master-takeover-auto --cluster production` | 未指定目标时自动选择候选。也可以显式指定 destination，此时仍采用 auto 的收尾行为。 | repoint 后尝试自动启动旧主库复制。 |

生产受控变更优先显式指定 destination。只有接受“旧主库自动加入”时才使用 `-auto`；未传 `--destination` 时，还必须同时接受自动选择候选。`force-master-takeover` 与 `force-master-failover` 会主动放弃当前主库，属于事故命令，不能作为计划维护的快捷方式。

## 前置条件

进入变更窗口前逐项确认：

1. 集群只解析出一个主库，预定目标是它的直属副本。
2. 目标没有被 `must_not`、其他 promotion rule 或 `PromotionIgnoreHostnameFilters` 排除。
3. 目标复制正在运行，维护延迟合理，版本、GTID/Pseudo-GTID、复制过滤、凭据和 TLS 行为兼容。
4. 其他直属副本都能迁到目标下方；如果某个副本按计划不可用，应提前设置 downtime，并明确接受它不会随本次切换迁移。
5. Raft Leader 和多数派健康；集群上没有活动 recovery、anti-flapping 阻塞或互相冲突的自动化。
6. `PreGracefulTakeoverProcesses` 与 `PostGracefulTakeoverProcesses` 有明确超时、持久日志、最小权限和失败负责人。
7. 应用负责人已经准备流量排空/fencing 方案、终止点、写链路检查与回退决策树，且备份可恢复。

保存切换前状态：

```sh
orch which-cluster-master --cluster production
orch topology --cluster production
orch replication-analysis --output json
orch is-replicating --instance candidate.example.com:3306
orch raft-configuration --output json
```

使用实际部署的二进制执行 `orch help graceful-master-takeover`。确认 endpoint、集群、实例端口和变更审批前，不要直接复制本文命令执行。

## 服务端真实执行顺序

当前实现依次执行以下阶段：

1. 解析唯一集群主库，读取它的直属副本并选择 designated instance。
2. 校验显式目标确为直属副本、允许提升、仍挂在解析出的主库下，并满足合理维护延迟阈值。自动选择时还会先尝试启动选中的副本复制。
3. 主库有多个直属副本时，先把其他副本迁到指定目标下方。非 downtime 副本迁移失败会终止切换；已 downtime 的不可用副本允许留在原处。
4. 创建强制分析上下文并运行 `PreGracefulTakeoverProcesses`。
5. 把旧主库设为 `read_only`，记录精确 binlog 坐标，等待目标副本执行到该坐标。
6. 使用指定 successor 进入正常 recovery 机制，完成提升并记录恢复。
7. 把旧主库 repoint 到新主库，必要时恢复复制凭据，并在当前 TLS 能力要求时开启 public-key retrieval。
8. `-auto` 模式会尝试启动旧主库复制；拓扑变化后运行 `PostGracefulTakeoverProcesses`。post-hook 失败不会回滚拓扑，也不会作为命令的拓扑结果返回。

其他副本的 relocation 发生在 pre-takeover hook 之前，也早于旧主库变为 read-only。因此即使流程较早终止，复制树也可能已经按计划重排；必须回读拓扑，不能假设“报错就完全没变化”。

## 执行与回读

应用完成写流量排空并到达约定 fencing 点后，只执行一次命令：

```sh
orch graceful-master-takeover \
  --cluster production \
  --destination candidate.example.com:3306 \
  --output json
```

随后独立验证数据库和控制面结果：

```sh
orch which-cluster-master --cluster production
orch topology --cluster production
orch is-replicating --instance old-primary.example.com:3306
orch replication-analysis --output json
orch raft-configuration --output json
```

使用非 auto 命令时，先检查旧主库，再显式让它加入复制：

```sh
orch start-replica --instance old-primary.example.com:3306
orch is-replicating --instance old-primary.example.com:3306
```

还需要从 MySQL 确认新主库可写、旧主库只读且跟随预期 source、所有保留副本位于预期复制树、没有 errant transaction。完成这些检查后才更新流量路由，并验证代表性应用读写。恢复记录、审计日志、两个 hooks、指标和告警应作为独立证据分别检查。

## 失败与终止语义

- 前置检查或 pre-hook 失败会阻止后续阶段，但其他直属副本可能已经完成 relocation。
- catch-up 超时或 `read_only` 之后的其他错误可能让旧主库保持只读。实现只在“recovery 没有返回 successor”这一特定分支尝试恢复可写，不存在覆盖全流程的事务回滚。
- 提升可能已经成功，而 repoint、凭据恢复或启动旧主库失败。post-hook 失败不会撤销变更，且可能只能从 hook/审计日志发现。不能只根据客户端结果推断当前拓扑。
- 客户端退出码 3 或响应丢失表示写操作结果未知。不要立即重放；先回读主库、拓扑、恢复记录、审计和 MySQL 状态。
- 不要为了“快速恢复”同时把新旧主库设为可写。在证明唯一权威主库及其下游拓扑前，应持续 fence 应用写入。

提升后确实需要切回时，应先稳定当前拓扑、收集证据，再把切回当成另一次计划切换。不要通过修改元数据、删除 Raft 状态或执行强制故障转移来拼凑局部回退。
