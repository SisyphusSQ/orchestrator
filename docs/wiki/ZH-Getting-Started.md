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

以 `conf/orchestrator-sample-sqlite.conf.json` 为起点。至少需要选择持久化路径并填写真实拓扑账号：

```json
{
  "RaftNodeID": "dev-1",
  "RaftDataDir": "/absolute/path/orchestrator-raft",
  "RaftBind": "127.0.0.1:10008",
  "ListenAddress": "127.0.0.1:3000",
  "BackendDB": "sqlite",
  "SQLite3DataFile": "/absolute/path/orchestrator.sqlite3",
  "MySQLTopologyUser": "orchestrator",
  "MySQLTopologyPassword": "replace-me"
}
```

配置文件含数据库凭据，应限制访问权限。

## 启动与 bootstrap

```sh
bin/orchestrator server --config /absolute/path/orchestrator.conf.json
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
