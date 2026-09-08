# 升级

[English](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Upgrading) · **中文** · [Wiki 首页](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

当前 `main` 可能包含尚未进入最新正式 Release 的改动。服务端二进制、配置、服务定义、`orch` 客户端和内嵌 Web 资源应作为一套经过验证的部署集合；源码成功构建不等于已经发布。

## 前置检查

1. 记录准确的源码/Release revision，并通读 [`docs/upgrading.md`](https://github.com/SisyphusSQ/orchestrator/blob/main/docs/upgrading.md) 中此版本之后的所有条目。
2. 按各自一致性要求备份每个节点的 Raft 目录和独立元数据库。
3. 盘点所有配置层、生成的服务参数、自动化脚本、监控规则、反向代理以及外部 hooks/KV 消费者。
4. 在隔离环境验证启动、多数派、发现、代表性读写、恢复策略、Web/API 认证、指标和回滚。
5. 明确维护负责人、流量切换、终止条件和变更后的独立回读方式。

## 当前主要不兼容项

- 服务端仅支持 Raft。删除 `RaftEnabled`，配置持久 ID、数据目录、bind 和 advertise 地址。
- `orchestrator server` 取代历史 `http`/`continuous` 入口；`orchestrator admin` 只用于本地维护。
- 独立 `orch` HTTP 客户端取代直连数据库的 CLI、`-c` 命令和 Shell 客户端。
- Prometheus/OpenTelemetry 取代 Graphite 与旧 raw 指标 API；已移除设置会被拒绝。
- ZooKeeper 发布和 `ZkAddress` 已删除，必须先把消费者迁移到 Consul KV 或外部 hook。
- Web 资源已内嵌，应移除依赖外置前端 resources 目录的部署逻辑。
- 源码迁到 `cmd/` 与 `internal/`，不保留历史公开 Go import 路径。
- HTTP transport、后端 DAO 和日志实现已变化，需要验证认证/代理、代表性数据库路径、日志解析和 syslog 可用性。

## 发布与回滚

不得从同一逻辑部署建立多个 Raft 集群，也不得让新旧系统同时对同一拓扑执行恢复。现有 Raft 成员复用原状态，不能再次 bootstrap。不要假设任意混合版本成员都安全，应按跨越版本的具体兼容边界执行。

回滚时同时恢复相互匹配的旧服务端、客户端、配置和服务定义。持久状态如何处理取决于跨越的具体变更，必须遵循详细升级条目，不要临时删除或改写 Raft/数据库状态。
