# 外部集成
[English](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Integrations) · **中文** · [Wiki 首页](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

外部集成位于 orchestrator 的一致性边界之外。Raft 提交成功不代表 Consul、Hook、Agent、代理或遥测后端已经成功；每个系统都需要独立回读、超时和责任人。

## Consul KV

Consul 用于发布集群 writer 信息。配置 `consul.address`、scheme、datacenter、HTTP timeout 和可选 ACL token；HTTPS 使用 `consul.tls` 的 CA、server name 与成对客户端证书。`skipVerify` 只用于有期限的兼容处理。

`consul.kv.clusterMasterPrefix` 决定键前缀，provider 当前为 `consul`。一次集群发布通常涉及多个键，受 `maxKVsPerTransaction` 限制。跨数据中心分发启用后，任一远端超时或部分成功都必须按实际状态处理；系统不会替你做跨系统事务回滚。

验收时执行单集群发布，读取前缀下全部键，核对 hostname、IP、port 与实际主库，再验证应用解析结果。ACL token 不得出现在日志、CLI 历史或 Wiki。

## Recovery Hooks

页面化 Hook profile 可绑定 9 个恢复阶段，并按 global/cluster scope 使用 `inherit`、`replace` 或 `disable`。命令由 `hooks.shellCommand` 指定的 shell 以服务进程身份执行；profile 必须设置 1–3600 秒 timeout、1024–1048576 bytes 输出限额、`continue` 或 `abort` 失败策略。

Hook 不是 Raft 内的外部事务。写脚本时使用幂等键（如 recovery UID）、显式网络 timeout、结构化日志和可重复查询的外部结果。不要把密码放在命令参数；审计脱敏只覆盖常见赋值模式，不能代替秘密管理。

“测试执行”会在真实服务端运行命令。先在隔离 scope 使用无副作用 payload，确认服务身份、工作目录、PATH、DNS、证书和防火墙，再接入生产系统。

## Agent 与 seed

启用 `agents.serveHTTP` 后，控制台提供 Agents、活动 seed、seed 详情和数据恢复任务。Agent TLS 与主 HTTP TLS 是独立配置域；分别设置证书、mTLS、CA 和 OU。`pollMinutes`、`unseenForgetHours`、`staleSeedFailMinutes` 与发送前等待时间决定任务可见性和失败判定。

Agent 自定义命令和数据恢复会在远端主机产生真实副作用。执行前核对 hostname、mount、MySQL port、磁盘空间、源 seed、目标目录和终止方式；完成后同时回读 Agent、任务状态、目标文件和 MySQL。

## 反向代理与身份系统

Basic、multi、proxy、token 与 mTLS 由服务端统一处理。proxy 模式只能信任受控反向代理提供的用户头，边界必须删除外部同名头。代理需要正确传递 URL prefix、scheme、host、客户端证书信息（如架构要求）和超时；不要缓存 mutation 或认证相关响应。

## Prometheus 与 OpenTelemetry

Prometheus 从带 URL prefix 的 `/metrics` 抓取。指标是节点本地运行状态，聚合时保留 node/instance/cluster 等标签并控制基数。仓库的 `resources/metrics` 提供 dashboard/alert 资产。

`observability.tracing.endpoint` 启用 OTLP 导出，`sampleRatio` 控制采样。exporter 在启动时构建，endpoint 或 TLS 环境变更需要重启。遥测后端失败不应被解释为业务操作失败或成功；通过应用响应、审计和目标系统分别确认。

## 集成验收模板

1. 记录调用方、目标、身份、scope、timeout、重试与幂等键。
2. 在隔离对象上执行一次成功和一次预期失败。
3. 同时保留 orchestrator 响应/审计与外部系统读回证据。
4. 模拟断连，确认结果被标记为未知而不是静默重试。
5. 验证凭据轮换、证书到期、权限撤销和降级告警。
6. 为人工处置写清停止条件；禁止用第二套自动化与 orchestrator 竞争控制同一主库。

内建 ZooKeeper 发布已删除，不应再配置或依赖旧 ZK 键。需要新增 provider 时，应把它作为明确的新契约实现和测试，而不是复用 Consul 名称做隐式兼容。
