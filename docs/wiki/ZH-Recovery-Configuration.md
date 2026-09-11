# 页面化恢复配置

[English](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Recovery-Configuration) · **中文** · [Wiki 首页](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

恢复策略和 pre/post Hook 在 Web 控制台“恢复配置”页面维护，不属于 YAML/JSON 服务配置。数据库只保存用户覆盖项；实际执行值按“集群覆盖 > 全局覆盖 > 代码默认”计算，并通过 Raft 命令同步。每次恢复记录有效策略与 Hook assignment 的指纹，便于事后证明使用了哪一版决策。

## 权限与 scope

- global scope 使用 key `*`，作为所有集群的基础覆盖。
- cluster scope 必须使用 `cluster_alias_override` 中显式且唯一的 alias；空 alias 或随 hostname 漂移的名字不能作为稳定策略键。
- 启用认证后，`authentication.configurationAdmins.users/groups` 决定写权限；空列表拒绝写入。
- 未启用认证的本地部署可以写配置，但仍需要可用 Leader 和多数派。
- 保存使用 `revision` 乐观锁。冲突表示已有他人修改，应重新读取、比较并人工合并。

## 23 项策略参考

未明确列为非零/true 的代码默认值是 false、0 或空列表。默认值不是生产建议；启用自动恢复前应逐集群评审。

| 字段 | 代码默认 | 决策含义与风险 |
| --- | --- | --- |
| `autoMasterRecovery` | `false` | 自动恢复主库；只有 fencing、候选和演练完成后启用 |
| `autoIntermediateMasterRecovery` | `false` | 自动恢复中间主库；会批量调整下游 |
| `recoveryIgnoreHostnameFilters` | `[]` | 恢复过滤，与发现过滤不同 |
| `promotionIgnoreHostnameFilters` | `[]` | 禁止匹配实例被提升，但仍可发现和展示 |
| `problemIgnoreHostnameFilters` | `[]` | 从问题判断中过滤；过宽会隐藏故障 |
| `failureDetectionPeriodBlockMinutes` | `60` | 同一故障检测的阻塞窗口，避免重复检测风暴 |
| `recoveryPeriodBlockSeconds` | `3600` | 恢复后的阻塞窗口，避免连续 failover |
| `reasonableReplicationLagSeconds` | `10` | 正常候选的合理延迟阈值 |
| `reasonableMaintenanceReplicationLagSeconds` | `20` | maintenance 场景允许的合理延迟 |
| `verifyReplicationFilters` | `false` | 提升/重排前校验复制过滤器 |
| `failMasterPromotionOnLagMinutes` | `0` | 候选超过阈值时阻止提升；按同版本确认 0 的边界语义 |
| `sqlThreadPromotionPolicy` | `allow` | `allow`、`wait` 或 `reject` |
| `recoverNonWriteableMaster` | `false` | 是否恢复不可写主库；先排除计划只读和 fencing |
| `coMasterRecoveryMustPromoteOtherCoMaster` | `true` | co-master 故障时要求提升另一 co-master |
| `detachLostReplicasAfterMasterFailover` | `true` | failover 后分离确认丢失的副本 |
| `applyMySQLPromotionAfterMasterFailover` | `true` | 成功恢复后应用 MySQL 提升状态 |
| `preventCrossDataCenterMasterFailover` | `false` | 禁止跨数据中心 failover；依赖分类完整 |
| `preventCrossRegionMasterFailover` | `false` | 禁止跨区域 failover；空 region 不能当同区域 |
| `masterFailoverDetachReplicaMasterHost` | `false` | failover 后 detach 副本 master host |
| `postponeReplicaRecoveryOnLagMinutes` | `0` | 延迟过高时推迟副本恢复 |
| `enforceExactSemiSyncReplicas` | `false` | 要求精确 semi-sync 副本条件 |
| `recoverLockedSemiSyncMaster` | `false` | 是否恢复被 semi-sync 锁住的主库 |
| `reasonableLockedSemiSyncMasterSeconds` | `0` | semi-sync 锁定持续时间阈值 |

所有时间值必须非负；`sqlThreadPromotionPolicy` 只接受 `allow`、`wait`、`reject`。过滤器按当前正则行为使用，保存前以“应匹配/不应匹配”主机各验证一个。

## 覆盖继承示例

假设 global 把 `reasonableReplicationLagSeconds` 改为 15，并启用 `verifyReplicationFilters`；cluster `payments` 只覆盖前者为 5。该集群的有效值为 lag 5、filter 校验 true，其他字段继续来自 global 或代码默认。删除 cluster 覆盖字段意味着重新继承，不是写入零值。页面应同时展示 effective value 与来源；验收必须回读有效策略，不能只看保存请求成功。

## Hook profile

| 字段 | 约束 | 说明 |
| --- | --- | --- |
| `id` / `name` | 非空 | ID 是稳定引用，name 面向人 |
| `commands` | 至少一条，不能空白 | 按列表顺序执行 |
| `timeoutSeconds` | 1–3600 | 每条命令超时 |
| `failurePolicy` | `continue` 或 `abort` | abort 会中止对应恢复阶段 |
| `outputLimitBytes` | 1024–1048576 | 审计输出上限 |
| `enabled` | bool | 禁用 profile 不等于删除 assignment |
| `revision` | 乐观锁 | 防止并发覆盖 |

命令由 `hooks.shellCommand` 指定的 shell、以 orchestrator 进程身份执行。不要依赖交互 shell、用户 home、未固定 PATH 或工作目录。显式设置外部 timeout，使用 recovery UID 做幂等键，并让命令结果可被外部读取。

常见 password/secret/token/API-key 赋值会在审计前脱敏，但秘密不应出现在参数或标准输出。输出被截断时要在外部系统保留完整关联日志。

## 9 个 Hook 阶段

| phase | 触发位置 | 典型用途 | 注意事项 |
| --- | --- | --- | --- |
| `failure_detection` | 确认故障检测后 | 通知、冻结竞争自动化 | 尚未选择成功恢复结果 |
| `pre_failover` | failover 写入前 | fencing、变更门禁 | `abort` 可阻止恢复 |
| `post_master_failover` | 主库 failover 后 | 更新 writer、服务发现 | 必须外部回读 |
| `post_intermediate_master_failover` | 中间主库恢复后 | 更新局部路由/通知 | 区分主库 failover |
| `post_failover` | 任意恢复完成后 | 统一审计和通知 | 不等于业务流量已验证 |
| `post_unsuccessful_failover` | 恢复未成功后 | 告警升级、人工接管 | 容忍不完整上下文 |
| `pre_graceful_takeover` | 计划切换前 | 冻结写入、校验窗口 | 与故障恢复分开 |
| `post_graceful_takeover` | 计划切换后 | 切路由、解冻与验收 | writer 更新应幂等 |
| `post_take_master` | Take Master 后 | 兼容流程后置动作 | 避免与其他 post 重复 |

## assignment 模式

- `inherit`：集群沿用 global assignment；不能带 profile IDs。
- `replace`：按给定 ID 顺序完整替换 global；至少一个 profile。
- `disable`：该集群明确不运行这一阶段；不能带 profile IDs。

“没有 cluster 记录”与显式 `inherit` 可能执行相同，但审计意图不同。删除或禁用 profile 前先搜索 assignment，避免失效引用。

## 安全变更流程

1. 回读 global、目标 cluster 的 raw override、effective policy、revision 和 phase 来源。
2. 写明 reason、预期行为、失败/回滚条件和验证集群。
3. 先修改隔离集群；策略只提交需要覆盖的稀疏 patch。
4. 重新读取 revision、effective value 与来源。
5. Hook 先用无副作用命令测试身份、环境、timeout、脱敏和审计。
6. 在演练中触发 phase，关联 recovery UID、策略/Hook 指纹与外部回读。
7. 逐集群推广。自动恢复开关是单独动作，不与策略保存混在同一验收中。

“测试执行”会真实运行命令。超时后通过 profile revision、operation audit 和外部幂等键确认结果。

## 升级与回滚

旧 YAML/JSON 中的恢复策略和 Hook 字段已被拒绝，不会自动导入。升级前记录旧值，启动新版本后在页面配置并回读；回滚前确认旧版本是否理解新元数据库结构。恢复旧元数据库会回退策略、assignment 与 revision，启用自动恢复前必须重新核对。
