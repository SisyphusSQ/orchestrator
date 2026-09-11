# orchestrator

[![CI](https://github.com/SisyphusSQ/orchestrator/actions/workflows/main.yml/badge.svg)](https://github.com/SisyphusSQ/orchestrator/actions/workflows/main.yml)

[Chinese documentation](README.zh-CN.md) · [GitHub Wiki](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Overview) · [Releases](https://github.com/SisyphusSQ/orchestrator/releases)

`orchestrator` discovers, visualizes, refactors, and recovers MySQL replication topologies. This maintained fork keeps the proven topology algorithms while providing a Raft-only server, a standalone HTTP client, an embedded Web console, and current observability and security contracts.

## What is different in this fork?

- Every server participates in Raft. Development uses an explicitly bootstrapped single-node cluster; production normally uses three or five voters.
- The server starts with `orchestrator server`. Remote administration uses the standalone Go client `orch`; the historical database-connected CLI and shell client are not included.
- Each Raft node owns an independent MySQL or SQLite metadata backend. Nodes coordinate through Raft instead of sharing a backend database.
- The React, TypeScript, and Ant Design console is embedded with `go:embed`, so a deployed server does not need Node.js or an external frontend directory.
- Each node exposes health and Prometheus endpoints and can export OpenTelemetry traces over OTLP HTTP.

## Console

The screenshots below are generated from the current Storybook fixtures with `make docs-screenshots`, so documentation states stay reviewable and reproducible.

### Cluster overview

![Cluster overview](docs/assets/screenshots/cluster-overview.png)

### Topology and recovery policy

| Topology detail | Recovery configuration |
| --- | --- |
| ![Topology detail](docs/assets/screenshots/topology-detail.png) | ![Recovery configuration](docs/assets/screenshots/recovery-configuration.png) |

## Quick start

Requirements: Go 1.27.0, Node.js 22.22.2 or newer, and pnpm 10.33.2. Re-read `go.mod` and `web/package.json` when building a different revision.

```sh
make deps
make web-deps
make build
bin/orchestrator server --config /absolute/path/orchestrator.conf.yaml
```

The server is Raft-only. A new development deployment must bootstrap its first member explicitly; follow the [getting-started guide](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Getting-Started) instead of treating process startup as cluster creation.

Build and use only the remote client:

```sh
make cli
export ORCH_ENDPOINT="http://127.0.0.1:3000"
bin/orch clusters
bin/orch topology --cluster production
```

Run `bin/orch help <command>` for the flags implemented by the current binary. Automation should prefer `--output json` and must read back a mutating request when the client reports an unknown result.

## Documentation and development

- [Getting started](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Getting-Started)
- [Configuration](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Configuration)
- [Raft operations](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Raft-Operations)
- [Failure recovery](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Failure-Recovery) and [planned switchover](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Planned-Switchover)
- [Development](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Development) and [package guide](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Package-Guide)
- [Upgrade guide](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Upgrading)
- [Versioned documentation source](docs/README.md)

Useful development entry points:

```sh
make test-unit
make test-integration
make test-web
make test-storybook
make test-docs
```

Build, automated tests, package publication, deployment, and live MySQL/Raft acceptance are separate evidence surfaces. Report each one independently.

## Project lineage and license

This repository is derived from [Percona's orchestrator fork](https://github.com/percona/orchestrator) and the original [openark/orchestrator](https://github.com/openark/orchestrator), authored by [Shlomi Noach](https://github.com/shlomi-noach). Historical attribution is preserved in the repository history and documentation.

`orchestrator` is licensed under the [Apache License 2.0](LICENSE). Report problems or propose changes through [GitHub Issues](https://github.com/SisyphusSQ/orchestrator/issues).
