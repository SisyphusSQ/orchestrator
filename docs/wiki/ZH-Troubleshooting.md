# 故障排查
[English](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Troubleshooting) · **中文** · [Wiki 首页](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

排障先确定故障层级，再收集同一时间窗口的证据。不要在原因未知时反复 reload、bootstrap、重试拓扑写入或直接修改元数据库；这些动作会覆盖最有价值的现场。

## 五分钟分诊

1. 记录时间、请求 ID、访问节点、版本/commit、配置来源和最近变更。
2. 依次读取 `/health/live`、`/health/ready`、`/health/leader-ready` 和 `/api/raft/configuration`。
3. 确认客户端命中的实际 URL、URL prefix、代理、认证身份、HTTP 状态与响应 `Code`。
4. 回读目标实例和集群，检查最后成功发现时间、复制线程、延迟、GTID/坐标、只读和 maintenance/downtime。
5. 查看应用日志、审计、恢复步骤、Prometheus 指标和 tracing；保留原始错误，先脱敏再共享。

## 症状矩阵

| 症状 | 优先检查 | 常见误判 | 停止条件 |
| --- | --- | --- | --- |
| live 失败 | 进程、监听、TLS、崩溃日志 | 把端口拒绝当 Raft 故障 | 进程持续退出或证书不可读 |
| live 成功、ready 失败 | 元数据库、Raft 初始化/追平、health 新鲜度 | 继续向节点发管理写 | 多数派未知或 backend 不可用 |
| leader-ready 失败 | 当前 Leader、选举、成员地址 | 强制把 follower 变 Leader | 网络分区或多个疑似 Leader |
| Web 503/空白 | 嵌入资源、prefix、`web-config`、代理缓存 | 用 Storybook 成功代替生产验证 | 资源版本与后端不一致 |
| CLI 401/403 | auth method、凭据、proxy header、power 用户 | 把授权失败当路由不存在 | 身份来源不可信 |
| API 200 但业务失败 | JSON `Code`、`Message`、`Details` | 只看 HTTP 状态 | 写结果未知或 validation 失败 |
| 实例未发现 | 网络/TLS/账号/身份/过滤器 | 先删除实例或放宽正则 | 同一实例出现多个规范名 |
| 拓扑陈旧 | 队列、并发、poll、慢查询、节点时间 | 在旧视图上执行重排 | 发现年龄超出变更窗口 |
| 恢复未触发 | 全局开关、策略、Leader/多数派、阻塞期、候选 | 直接 force failover | fencing 未确认或候选不安全 |
| Hook 失败 | profile revision、命令、身份、timeout、输出限额 | 在生产反复 Test Run | Hook 非幂等或输出含秘密 |

## 配置启动失败

配置使用严格解析。未知字段、旧平铺字段、大小写错误、重复键、多 YAML document、尾随内容或同一路径多种扩展名都会失败。先运行同版本二进制的 `dump-config` 或直接读取启动错误，不要通过删除大段配置碰运气。

节点身份、Raft 地址/目录、HTTP listener、数据库连接池和 tracing 改动不能靠 reload 生效。reload 成功只说明可重载部分通过，不证明底层资源已经重建。

## Raft 问题

- 未初始化：只能对一个新节点 bootstrap，其他节点通过 Leader 加入。
- 无 Leader：检查 voter 是否仍有多数派、成员 advertise 双向连通、时钟和磁盘；不要重复 bootstrap。
- 成员地址旧：通过受保护的成员变更处理，并用 configuration index 防并发覆盖。
- node ID 不匹配：停止节点，核对 `node-id`、配置和完整数据目录来源；不要手工改文件让它“能启动”。
- 超时：按未知结果处理，先从多个节点回读成员表。

## 拓扑与恢复问题

先到 MySQL 直接读取复制状态，再与 orchestrator 视图比较。若 MySQL 已变而视图未变，调查发现链路；若视图正确而操作拒绝，读取候选、过滤、版本、GTID、跨机房和恢复阻塞理由。出现双主可写时先 fence，不要先追求控制台变绿。

恢复记录要连同 failure detection、analysis、selected successor、all errors、Hook fingerprint、operation audit 和外部路由一起分析。自动恢复没有执行可能是正确的安全拒绝。

## 收集支持包

至少保留：版本与 commit、脱敏的有效配置、三类 health、Raft configuration/state、相关实例/集群 JSON、失败请求和响应、相同时间窗口日志、审计/恢复 UID、指标截图、最近部署或配置 diff。不要包含数据库密码、Basic/token 凭据、Consul ACL token、证书私钥或 Hook 输出中的秘密。

## 何时升级处理

多数派无法确认、出现两个可写主库、恢复写结果未知、元数据库 Schema 不一致、Raft 身份冲突、审计缺失或需要直接改状态文件时，停止自助变更并升级给项目维护者/数据库负责人。记录已经执行的每个命令及其原始结果，避免下一位操作人重复危险动作。
