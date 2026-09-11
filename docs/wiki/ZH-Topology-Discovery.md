# 拓扑发现与分类
[English](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Topology-Discovery) · **中文** · [Wiki 首页](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

发现流程把 MySQL 的实时复制状态转换为 orchestrator 可分析的拓扑模型。它不是一次性导入：调度器会持续轮询已知实例、沿复制关系扩展、规范化主机名、应用过滤规则，并把结果写入每个节点自己的元数据库。

## 发现前契约

拓扑账号至少要能连接目标实例并读取版本、全局变量、复制状态、binary log 与 GTID 相关信息。执行拓扑变更需要额外写权限，不应为了方便让只读发现账号拥有全局管理权限。所有 Raft 节点都要使用能覆盖同一实例集合的凭据和 TLS 信任配置。

先验证：DNS 或自定义解析结果稳定；MySQL `report_host`/`report_port` 与实际可达地址一致；防火墙允许每个 orchestrator 节点访问实例；时钟、server UUID 和复制 channel 符合环境约定。

## 种子与扩展

- `topology.discovery.seeds` 在启动后提供初始入口，适合稳定且少量的锚点。
- `orch discover --instance host:port` 立即读取一个实例，适合上线验收和临时补发现。
- 已知实例会按 `pollSeconds` 周期刷新；失败实例使用 dead poll 节奏，避免持续热循环。
- `useShowReplicaHosts` 允许使用主库报告的副本入口，但仍需验证这些地址真实可达。
- 忘记实例不会阻止它再次从复制关系被发现；要长期排除需使用有明确意图的过滤或修复上游报告信息。

## 主机名规范化

`topology.hostname.resolveMethod` 决定输入名的解析方式，`mysqlResolveMethod` 可从 MySQL（默认 `@@hostname`）获取规范名。`resolveExpiryMinutes` 控制缓存有效期，`rejectResolvePattern` 拒绝不可信结果。修改解析策略前先导出当前映射；同一物理实例被不同名称识别会造成重复节点、错误别名和危险候选判断。

排查顺序应为：网络连接 → TLS/账号 → MySQL 返回的身份 → DNS/解析缓存 → ignore 过滤器 → 元数据库写入。不要看到实例消失就先放宽过滤器。

## 过滤器语义

| 配置 | 作用 |
| --- | --- |
| `ignoreHostnames` | 从通用发现路径排除匹配主机 |
| `ignoreReplicaHostnames` | 排除作为副本发现的匹配主机 |
| `ignoreMasterHostnames` | 排除作为上游发现的匹配主机 |
| `ignoreReplicationUsernames` | 忽略使用匹配复制账号的关系 |
| `recoveryIgnoreHostnameFilters` | 只影响恢复策略，不等于发现过滤 |
| `promotionIgnoreHostnameFilters` | 禁止候选提升，不等于从拓扑隐藏 |

过滤值按当前实现的正则语义使用。上线前用应匹配与不应匹配的代表性 hostname 做双向验证，并在变更后观察 filter log。过宽规则可能把整个机房从拓扑中移除。

## 集群、位置与候选分类

集群名通常来自拓扑主库身份；稳定的人类入口应使用显式唯一的 cluster alias。分类查询和正则可以提供 instance alias、cluster domain、data center、region、physical environment、promotion rule 与 semi-sync enforced 状态。

候选提升至少同时考虑：复制坐标/GTID、SQL/IO thread、延迟、过滤规则、MySQL 版本、机房/区域策略、promotion rule、只读与 semi-sync 状态。分类字段缺失时，不应假定“空值等于同一机房”。在启用自动恢复前，对每个可能候选回读分类结果。

## GTID 与 Pseudo-GTID

GTID 拓扑优先使用 GTID relocation，并检查 errant transaction。非 GTID 环境依赖 file:position 或 Pseudo-GTID；Pseudo-GTID 必须在相关主库持续写入可识别标记，配置 pattern、查询、单调提示、binlog 保留和权限。没有标记或 binlog 已被清理时，部分智能重排无法安全执行。

## 验收与持续监控

```sh
orch --endpoint https://orchestrator.example discover --instance db1.example:3306
orch --endpoint https://orchestrator.example which-instance --instance db1.example:3306
orch --endpoint https://orchestrator.example which-cluster --cluster db1.example:3306
orch --endpoint https://orchestrator.example topology --cluster production
```

成功标准不是 discover 返回 200，而是实例身份唯一、复制边正确、集群/别名/位置/候选规则符合预期，并且下一个轮询周期后仍保持一致。持续关注发现队列、最后成功检查时间、问题实例、解析缓存和 filter log。发现状态过旧时先停止变更与自动恢复，再处理根因。
