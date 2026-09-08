# 项目概览

[English](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Overview) · **中文** · [Wiki 首页](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

`orchestrator` 用于发现 MySQL 复制拓扑、展示状态、执行受控拓扑调整、检测故障并协调恢复。它以长期运行的 HTTP/Web 服务提供能力，通过独立 `orch` 客户端、Web 控制台或 HTTP API 进行管理。

## 当前架构

本分支明确采用仅 Raft 架构：

- 每个服务节点都有稳定的 `RaftNodeID`、持久化 `RaftDataDir`，以及可达的 `RaftBind`/`RaftAdvertise` 地址。
- 每个节点独占自己的 MySQL 或 SQLite 元数据库；Raft 成员之间不共享元数据库。
- 新集群只在一个种子节点上 bootstrap，其余节点通过 Leader 加入。
- 所有就绪节点都发现 MySQL 拓扑；只有获得多数派确认的 Leader 执行恢复和受协调的业务写入。
- Follower 可将受支持的操作安全转发到 Leader；失去多数派时，业务写入以失败关闭。

## 使用入口

- `orchestrator server` 启动 Raft、HTTP API 和内嵌 Web 控制台。
- `orch` 是独立 Go 二进制，只访问 HTTP API，不连接元数据库。
- `web/` 下的控制台使用 React、TypeScript、Ant Design、React Flow 和 Dagre；生产资源编入服务端二进制。
- 每个节点提供 Prometheus 指标和本地存活/就绪接口，也可选择向 OTLP HTTP endpoint 导出 traces。

## 能力边界

项目继续支持拓扑发现、基于 GTID/Pseudo-GTID 的调整、计划切换、自动恢复、审计、标签、Consul KV 发布、认证、TLS 和可选 Agent 接口。可用性与安全性取决于正确的拓扑权限、恢复过滤、Raft 多数派和部署验收。

历史上的共享元数据库选主、非 Raft 服务模式、直连数据库的旧 CLI 和 Shell 客户端均已移除。替换旧二进制前请先阅读[升级指南](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Upgrading)。
