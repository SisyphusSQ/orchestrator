# 快速开始

[English](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Getting-Started) · **中文** · [Wiki 首页](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

本页使用 SQLite 建立单节点开发集群，用于演示进程启动和 Raft 初始化；它不是生产高可用方案。

## 前置条件

- `go.mod` 声明的 Go 版本
- `web/package.json` 声明的 Node.js 与 pnpm 版本
- Git、SQLite 编译所需的 C 编译器，以及 `rsync`
- 一个允许拓扑账号读取的 MySQL 实例

## 构建

```sh
make deps
make web-deps
make build
```

`bin/orchestrator` 包含服务端、API 和 Web 资源；`bin/orch` 是独立客户端。

## 配置

以 `conf/orchestrator-sample-sqlite.conf.yaml` 为起点。至少需要选择持久化路径并填写真实拓扑账号：

```yaml
RaftNodeID: dev-1
RaftDataDir: /absolute/path/orchestrator-raft
RaftBind: 127.0.0.1:10008
ListenAddress: 127.0.0.1:3000
BackendDB: sqlite
SQLite3DataFile: /absolute/path/orchestrator.sqlite3
MySQLTopologyUser: orchestrator
MySQLTopologyPassword: replace-me
```

配置文件含数据库凭据，应限制访问权限。

## 启动与 bootstrap

```sh
bin/orchestrator server --config /absolute/path/orchestrator.conf.yaml
```

在另一个终端中，只执行一次单节点 bootstrap：

```sh
bin/orch --endpoint http://127.0.0.1:3000 raft-bootstrap
bin/orch --endpoint http://127.0.0.1:3000 raft-configuration --output json
```

节点重启时复用原 Raft 数据目录，不要重复 bootstrap。

## 发现与查看

```sh
bin/orch --endpoint http://127.0.0.1:3000 discover --instance db.example.com:3306
bin/orch --endpoint http://127.0.0.1:3000 clusters
bin/orch --endpoint http://127.0.0.1:3000 topology --cluster db.example.com:3306
```

打开 `http://127.0.0.1:3000/web/clusters`，并检查同一节点的 `/health/live`、`/health/ready` 和 `/metrics`。进入生产前，请继续阅读[配置](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Configuration)、[Raft 运维](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Raft-Operations)、[安全](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Security)和[可观测性](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Observability)。

## 生产前置检查

- 使用 3 或 5 个 voter，并按预期故障域布局；不能把单节点数据目录临时扩成高可用方案。
- 每个节点使用独立 Raft 和元数据存储、稳定身份、其他成员可达的 advertise 地址、受监督的进程启动及经过测试的备份/恢复流程。
- 在每个被管理 MySQL 实例上创建专用拓扑账号。先授予发现权限，仅为明确启用的拓扑变更与恢复补充必要权限。
- 启用自动恢复前，先定义主机名解析、发现过滤、集群别名、机房/区域分类、提升规则、恢复过滤、hooks、审计保留与 fencing。
- 将 Web/API 和 Raft 放在受控网络，验证真实代理、TLS/mTLS、认证、URL prefix、只读角色、指标抓取、日志收集与告警路由。
- 在隔离拓扑中演练一次代表性发现、读取、受控写入、故障分析和回滚，并分别回读 MySQL、orchestrator、Raft、审计及外部路由/KV 状态。

配置样例只是起点，不是生产策略。逐项对照 [`internal/config/config.go`](https://github.com/SisyphusSQ/orchestrator/blob/main/internal/config/config.go)，并确保凭据不进入版本控制。
