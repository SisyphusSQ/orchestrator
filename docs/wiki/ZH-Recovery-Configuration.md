# 页面化恢复配置

[English](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Recovery-Configuration) · **中文** · [Wiki 首页](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

恢复决策参数与 pre/post Hook 统一在 Web 控制台的“恢复配置”页面维护，不再写入示例配置文件。系统提供 23 项代码默认策略；数据库仅保存用户修改过的全局值与集群覆盖值。

生效优先级为：集群覆盖 > 全局配置 > 代码默认值。集群覆盖必须使用 `cluster_alias_override` 中显式且唯一的集群别名，避免拓扑主机名变化后绑定漂移。策略保存使用 revision 乐观锁，经 Raft 命令同步；每次恢复在 `topology_recovery` 中记录策略与 Hook 指纹。

Hook 支持 9 个阶段：故障检测后、故障切换前、主库切换后、中间主库切换后、恢复结束后、恢复失败后、优雅切换前、优雅切换后、Take Master 后。集群阶段有三种明确语义：

- `inherit`：继承全局阶段配置；
- `replace`：完整替换全局配置，并按所选配置顺序执行；
- `disable`：在该集群显式关闭阶段 Hook。

Hook 以 Orchestrator 进程身份执行。每个配置必须设置单命令超时、失败策略与审计输出上限；常见密码、token、secret 与 API key 赋值会在审计前脱敏。页面“测试执行”会真实执行命令，必须先确认命令在当前环境中安全且幂等。

当启用认证时，恢复配置写权限由 `authentication.configurationAdmins.users` 与 `authentication.configurationAdmins.groups` 单独控制；空列表表示拒绝配置写入。未启用认证的本地部署允许配置写入，API 仍要求当前节点具备 Raft leader quorum。

这是不兼容升级：旧版 YAML/JSON 恢复参数与 Hook 键不再被接受，也不会自动迁移。升级前应记录旧值，启动新版后在页面重新配置并回读生效值。
