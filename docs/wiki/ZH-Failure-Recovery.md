# 故障检测与恢复

[English](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Failure-Recovery) · **中文** · [Wiki 首页](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

故障检测与恢复是两个阶段。所有就绪 Raft 节点都会探测 MySQL 拓扑并贡献观测结果；只有获得多数派确认的 Leader 注册和执行恢复。

## 启用自动恢复前先定义策略

- 在 Web 控制台“恢复配置”中按全局或显式集群别名开启自动恢复；配置优先级与 Hook 覆盖规则见 [页面化恢复配置](ZH-Recovery-Configuration.md)。
- 用 `RecoveryIgnoreHostnameFilters` 排除不应参与恢复的主机。
- 配置提升规则、机房/区域限制、复制延迟阈值和 GTID/Pseudo-GTID 策略。
- 把故障前后 hooks 当作生产代码：限制执行时间、显式暴露失败，并避免隐藏的非幂等重试。
- 需要运维可追溯性时，启用并保留后端审计记录。

发现故障不代表一定存在安全的接任者。候选资格、复制状态、过滤规则、多数派和实时拓扑都会参与决策。

## 检测与候选语义

分析会区分主库故障、中间主库故障、双主故障、成员不可达、复制停止或延迟以及结构告警。部分观测只用于提示，设计上不会触发恢复。只有配置的检测窗口、有效拓扑证据和恢复策略同时满足时，故障才可执行。

提升规则（`must`、`prefer`、`neutral`、`prefer_not`、`must_not`）、机房/区域策略、版本兼容、errant GTID、复制过滤、SQL delay 和延迟共同决定候选资格。`must_not` 禁止提升，但不会把实例从发现中删除。应在事故前维护好分类元数据，而不是事故中临时修正。

兼容时优先使用 GTID 调整。Pseudo-GTID 依赖写入 binlog 的等价标记，必须在故障前配置。自动注入需要权限与可写源；手工注入必须在每个可写主库上按稳定周期执行。标记过期或缺失后，file-position 匹配可能无法完成。

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

maintenance 用于抑制计划操作期间的自动动作；downtime 改变已知问题的展示方式，两者本身都不会修改 MySQL 状态。恢复确认只关闭运维关注记录，不会修复拓扑。再次强制恢复前，需要检查 anti-flapping 阻塞、活动恢复记录和 postponed 操作。

标签是 `key` 或 `key=value` 形式的运维元数据，可用于搜索和外部策略。标签写入、候选注册、downtime、maintenance 与恢复都属于业务变更，即使兼容路由使用 GET；自动化必须根据命令语义，而不是只根据 HTTP 方法判断副作用。

恢复 hooks 通过约定的环境变量接收事故上下文。应采用最小权限、有限超时、持久日志和明确负责人。拓扑变更成功但 hook 失败，不能视为外部切换全部成功。

## 验收与结果未知

不能只凭 HTTP 200 判定恢复成功。需要从 MySQL 回读提升节点和复制状态，查看 orchestrator 拓扑与恢复记录，确认 Raft 领导权/多数派，并检查审计与 hook 结果。客户端返回退出码 3 或丢失响应时，不要立即重放；先判断恢复是否已注册或完成。

生产启用前应在有代表性的隔离拓扑中演练。应用流量路由、DNS/KV 消费者、fencing 和外部 hooks 是独立验收面，不能由单元测试或 dry run 代替。
