# 拓扑操作
[English](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Topology-Operations) · **中文** · [Wiki 首页](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

拓扑操作会修改 MySQL 复制关系或实例状态。所有操作都遵循同一原则：先证明输入与目标拓扑新鲜，再选择与 GTID/file:position/Pseudo-GTID 能力匹配的命令；执行后以 MySQL 与 orchestrator 双重回读确认，网络超时绝不自动当作失败重试。

## 通用执行模板

1. 识别实例的规范 `host:port`、集群 alias、当前主库和复制模式。
2. 回读源与目标的 IO/SQL thread、延迟、binlog/relay log、GTID、只读、版本、过滤器和 maintenance/downtime。
3. 检查 Raft 有多数派且请求到达可写 Leader；确认服务端未处于 `server.readOnly`。
4. 使用 `can-replicate-from` 或 GTID 对应检查，明确数据丢失、跨机房和写流量影响。
5. 在 Web 操作预览或 CLI/API 中记录确切参数，获得业务变更窗口确认。
6. 只发起一次操作。断连或超时后先回读源、目标、拓扑和审计，不盲目重试。
7. 验证复制线程、上游、坐标/GTID、延迟、可写节点唯一性及应用路由。
8. 结束 maintenance/downtime，记录审计与回滚结果。

## 如何选择重排方式

| 场景 | 优先能力 | 常用命令族 | 关键限制 |
| --- | --- | --- | --- |
| 同一上游的单个副本下移 | GTID，其次 file:pos | `move-gtid`、`move-below`、`relocate` | 目标必须拥有所需事务且兼容 |
| 某实例的全部副本迁移 | GTID/智能重排 | `move-replicas-gtid`、`relocate-replicas` | 控制并发与批量等待超时 |
| 从多个副本选新上游 | GTID/Pseudo-GTID | `regroup-replicas-*` | 必须审查候选和未追平副本 |
| 上移一层 | file:pos/智能 | `move-up`、`move-up-replicas` | 上级坐标可关联且无过滤冲突 |
| 仅改上游但保留坐标 | file:pos | `repoint` | 高风险；坐标错误会复制错误数据 |
| 主从角色交换 | 计划切换 | `take-master` 或 graceful takeover | 需要写流量冻结与 fencing |

`relocate` 会按能力选择路径，便利不代表无需审查。需要完全可预测的变更时使用具体机制命令并保存选择依据。

## 复制线程与实例状态

- `start-replica`、`stop-replica`、`restart-replica` 控制复制线程；停止线程不等于停止业务写入。
- `set-read-only` 与 `set-writeable` 修改 MySQL 全局只读状态；`super_read_only` 是否联动受配置与 MySQL 版本影响。
- `skip-query`、GTID errant reset/inject、reset replication 会改变恢复轨迹，只能在已定位事务影响后使用。
- binary log flush/purge 必须先确认所有副本、恢复和 Pseudo-GTID 仍需要哪些日志。
- semi-sync enable/disable 与 delay replication 会改变可用性或数据保护等级，需同步监控与业务预期。

## maintenance、downtime 与 tags

maintenance 表示人为操作窗口，避免自动恢复与人工变更互相竞争；downtime 表示已知不可用/降级，并影响问题展示和恢复处理。两者都必须写清 owner、reason、预计结束时间并在完成后显式清除。

tags 用于选择与标记，不是强制安全策略。批量 untag、模糊 hostname pool、集群 alias 变更都要先输出受影响集合再写入。

## 失败与未知结果

以下情况立即停止后续步骤：发现数据超过允许新鲜度、Raft 无多数派、候选出现 errant GTID、源/目标复制过滤不一致、目标延迟持续增长、出现两个可写主库、Hook abort、外部路由无法确认。请求超时属于“结果未知”；只有状态回读证明未执行时才能重试。

回滚通常不是简单执行反向命令。先阻断写入扩散，确定最新事务所在实例，再根据当前而不是原计划的坐标重新制定拓扑。主库迁移请使用[计划切换](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Planned-Switchover)，故障场景请使用[故障恢复](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Failure-Recovery)。
