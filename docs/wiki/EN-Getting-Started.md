# Getting started

**English** · [中文](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Getting-Started) · [Wiki home](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

This walkthrough creates a single-node development cluster with SQLite. It demonstrates process startup and Raft initialization; it is not a production HA design.

## Prerequisites

- Go version declared by `go.mod`
- Node.js and pnpm versions declared by `web/package.json`
- Git, a C compiler for SQLite, and `rsync`
- A MySQL instance that the topology account is allowed to inspect

## Build

```sh
make deps
make web-deps
make build
```

`bin/orchestrator` contains the server, API, and Web assets. `bin/orch` is the standalone client.

## Configure

Start from `conf/orchestrator-sample-sqlite.conf.json`. At minimum, choose durable local paths and set real topology credentials:

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

Protect the configuration file because it contains database credentials.

## Start and bootstrap

```sh
bin/orchestrator server --config /absolute/path/orchestrator.conf.json
```

In another terminal, bootstrap this one node exactly once:

```sh
bin/orch --endpoint http://127.0.0.1:3000 raft-bootstrap
bin/orch --endpoint http://127.0.0.1:3000 raft-configuration --output json
```

A restarted node reuses its Raft data directory; do not bootstrap it again.

## Discover and inspect

```sh
bin/orch --endpoint http://127.0.0.1:3000 discover --instance db.example.com:3306
bin/orch --endpoint http://127.0.0.1:3000 clusters
bin/orch --endpoint http://127.0.0.1:3000 topology --cluster db.example.com:3306
```

Open `http://127.0.0.1:3000/web/clusters`. Check `/health/live`, `/health/ready`, and `/metrics` on the same node. Before production, continue with [Configuration](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Configuration), [Raft operations](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Raft-Operations), [Security](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Security), and [Observability](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Observability).
