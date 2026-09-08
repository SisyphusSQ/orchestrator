# 故障检测与恢复

[English](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Failure-Recovery) · **中文** · [Wiki 首页](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

故障检测与恢复是两个阶段。所有就绪 Raft 节点都会探测 MySQL 拓扑并贡献观测结果；只有获得多数派确认的 Leader 注册和执行恢复。

## 启用自动恢复前先定义策略

- 用 `RecoverMasterClusterFilters` 与 `RecoverIntermediateMasterClusterFilters` 明确允许恢复的集群。
- 用 `RecoveryIgnoreHostnameFilters` 排除不应参与恢复的主机。
- 配置提升规则、机房/区域限制、复制延迟阈值和 GTID/Pseudo-GTID 策略。
- 把故障前后 hooks 当作生产代码：限制执行时间、显式暴露失败，并避免隐藏的非幂等重试。
- 需要运维可追溯性时，启用并保留后端审计记录。

发现故障不代表一定存在安全的接任者。候选资格、复制状态、过滤规则、多数派和实时拓扑都会参与决策。

## 人工操作

先查看分析结果：

```sh
orch replication-analysis --output json
orch topology --cluster production
```

确认当前状态后，再执行具体恢复：

```sh
orch recover --instance failed-primary.example.com:3306
```

计划内维护应使用 maintenance/downtime 标记，并在适用时做 graceful takeover，而不是模拟崩溃。强制故障转移会主动放弃当前主库，影响范围更大。

## 验收与结果未知

不能只凭 HTTP 200 判定恢复成功。需要从 MySQL 回读提升节点和复制状态，查看 orchestrator 拓扑与恢复记录，确认 Raft 领导权/多数派，并检查审计与 hook 结果。客户端返回退出码 3 或丢失响应时，不要立即重放；先判断恢复是否已注册或完成。

生产启用前应在有代表性的隔离拓扑中演练。应用流量路由、DNS/KV 消费者、fencing 和外部 hooks 是独立验收面，不能由单元测试或 dry run 代替。
