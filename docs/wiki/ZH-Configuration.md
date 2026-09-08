# 配置

[English](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Configuration) · **中文** · [Wiki 首页](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

服务端从显式 `--config` 路径读取 JSON；未指定时依次查找 `/etc/orchestrator.conf.json`、`conf/orchestrator.conf.json` 和 `orchestrator.conf.json`。配置内含数据库与认证凭据，不应进入公开产物，并应限制文件访问权限。

## 必需的节点身份

每次启动服务端都需要：

```json
{
  "RaftNodeID": "node-1",
  "RaftDataDir": "/var/lib/orchestrator/raft",
  "RaftBind": "10.0.0.1:10008",
  "RaftAdvertise": "10.0.0.1:10008",
  "ListenAddress": ":3000"
}
```

`RaftNodeID` 是持久身份，不是网络地址。`RaftAdvertise` 默认使用规范化后的 `RaftBind`；位于 NAT 后时应显式设置。节点身份、Raft 路径与地址、HTTP 监听、已打开的数据库连接池以及 OpenTelemetry exporter 配置发生变化时都需要重启。

## 元数据库

每个 Raft 节点独立选择一个后端：

- MySQL：配置 `MySQLOrchestratorHost`、端口、数据库、账号和密码，或使用凭据文件。
- SQLite：将 `BackendDB` 设为 `sqlite`，并提供绝对且可写的 `SQLite3DataFile`。

不要让多个 Raft 节点共用同一个元数据库。元数据库和 Raft 数据目录有不同的一致性边界，应分别制定备份方案。

## 拓扑访问与策略

每个节点都要配置 `MySQLTopologyUser` 及密码或凭据文件。该账号必须能读取所有被发现实例的复制状态；执行拓扑变更时还需要对应权限。发现种子、主机名解析、实例过滤、提升规则、恢复过滤、hooks、审计输出、Consul、认证、TLS 和 URL 前缀均属于独立策略，需要按环境决定。

仓库在 [`conf/`](https://github.com/SisyphusSQ/orchestrator/tree/main/conf) 提供 MySQL 与 SQLite 示例。完整字段定义以 [`internal/config/config.go`](https://github.com/SisyphusSQ/orchestrator/blob/main/internal/config/config.go) 为准；各专题细节仍保留在 [`docs/`](https://github.com/SisyphusSQ/orchestrator/tree/main/docs)。

## 已移除配置

不要继续携带 `RaftEnabled`、`ZkAddress`、Graphite 配置或旧的内存指标保留配置。服务端会拒绝这些字段，避免被移除的行为静默失效。为兼容性，其他未知字段仍可能被接受，因此不能只以“成功启动”作为配置生效的验收依据。
