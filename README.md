# orchestrator

[![CI](https://github.com/SisyphusSQ/orchestrator/actions/workflows/main.yml/badge.svg)](https://github.com/SisyphusSQ/orchestrator/actions/workflows/main.yml)

[中文](#中文) · [English](#english) · [GitHub Wiki](https://github.com/SisyphusSQ/orchestrator/wiki)

## 中文

`orchestrator` 是 MySQL 复制拓扑发现、调整和故障恢复服务。本仓库在历史 orchestrator 项目基础上继续维护，当前运行架构与旧版有几项关键差异：

- 服务端仅支持 Raft；开发环境也需要显式建立单节点 Raft，生产通常使用 3 或 5 个投票节点。
- 服务端入口是 `orchestrator server`；远程管理使用独立 Go HTTP 客户端 `orch`，不再提供直连数据库的旧 CLI 或 Shell 客户端。
- Web 控制台位于 `web/`，使用 React、TypeScript 与 Ant Design，并通过 `go:embed` 编入服务端二进制；运行时不需要外置前端资源或 Node.js。
- 每个 Raft 节点使用独立的 MySQL 或 SQLite 元数据库。节点之间通过 Raft 协调，不共享元数据库。
- 节点原生暴露 Prometheus 指标和健康检查，并可向 OTLP HTTP trace endpoint 发送 OpenTelemetry traces。

快速构建：

```sh
make deps
make web-deps
make build
bin/orchestrator server --config /absolute/path/orchestrator.conf.yaml
```

单独构建客户端：

```sh
make cli
ORCH_ENDPOINT=http://127.0.0.1:3000 bin/orch clusters
```

从 [中文 Wiki 首页](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Overview) 开始，或查看仓库内的 [文档索引](docs/README.md)。现有部署升级前必须先阅读 [升级指南](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Upgrading)。

问题与改进建议请提交到本仓库的 [Issues](https://github.com/SisyphusSQ/orchestrator/issues)。发布产物在可用时会出现在 [Releases](https://github.com/SisyphusSQ/orchestrator/releases)。

## English

`orchestrator` discovers, refactors, and recovers MySQL replication topologies. This maintained fork has several important differences from historical orchestrator releases:

- The server is Raft-only. Development also requires an explicitly bootstrapped single-node Raft cluster; production normally uses three or five voters.
- Run the service with `orchestrator server`. Remote administration uses the standalone Go HTTP client `orch`; the former database-connected CLI and shell client are not available.
- The React, TypeScript, and Ant Design console lives in `web/` and is embedded in the server binary with `go:embed`; Node.js and external frontend resources are not runtime dependencies.
- Every Raft node owns an independent MySQL or SQLite metadata backend. Nodes coordinate through Raft rather than sharing a backend database.
- Every node exposes Prometheus metrics and health endpoints and can export OpenTelemetry traces over OTLP HTTP.

Quick build:

```sh
make deps
make web-deps
make build
bin/orchestrator server --config /absolute/path/orchestrator.conf.yaml
```

Build only the client:

```sh
make cli
ORCH_ENDPOINT=http://127.0.0.1:3000 bin/orch clusters
```

Start with the [English Wiki home](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Overview), or browse the in-repository [documentation index](docs/README.md). Existing deployments must read the [upgrade guide](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Upgrading) before replacing a binary.

Report problems and propose changes in this repository's [Issues](https://github.com/SisyphusSQ/orchestrator/issues). Published artifacts, when available, appear under [Releases](https://github.com/SisyphusSQ/orchestrator/releases).

## Project lineage and license

This repository is derived from [Percona's orchestrator fork](https://github.com/percona/orchestrator) and the original [openark/orchestrator](https://github.com/openark/orchestrator), authored by [Shlomi Noach](https://github.com/shlomi-noach). Historical attribution is preserved in the repository history and documentation.

`orchestrator` is licensed under the [Apache License 2.0](LICENSE).
