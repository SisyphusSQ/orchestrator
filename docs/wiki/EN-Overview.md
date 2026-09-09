# Overview

**English** · [中文](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Overview) · [Wiki home](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

`orchestrator` discovers MySQL replication topologies, visualizes their state, performs controlled topology changes, detects failures, and coordinates recovery. It runs as a long-lived HTTP/Web service and is operated through the standalone `orch` client, the Web console, or the HTTP API.

## Current architecture

This fork is intentionally Raft-only:

- Every server has a stable `raft.nodeID`, a persistent `raft.dataDir`, and a reachable `raft.bind`/`raft.advertise` address.
- Every server owns its own MySQL or SQLite metadata backend. Metadata databases are not shared between Raft members.
- A new cluster is bootstrapped on exactly one seed; other nodes are added through the leader.
- All ready nodes discover MySQL topology. Only a quorum-confirmed leader performs recoveries and coordinated business writes.
- Followers can safely proxy supported operations to the leader. Losing quorum fails closed for business writes.

## Interfaces

- `orchestrator server` starts Raft plus the HTTP API and embedded Web console.
- `orch` is a separate Go binary that calls the HTTP API. It never connects to the metadata database.
- The console under `web/` uses React, TypeScript, Ant Design, React Flow, and Dagre. Production assets are embedded into the server binary.
- Each node exposes Prometheus metrics and local liveness/readiness endpoints. OTLP HTTP trace export is optional.

## Capability boundary

The project still supports topology discovery, GTID and Pseudo-GTID aware refactoring, planned takeovers, automated recovery, auditing, tags, Consul KV publishing, authentication, TLS, and optional Agent endpoints. Availability and safety depend on correct topology permissions, recovery filters, Raft quorum, and deployment validation.

Historical shared-backend election, non-Raft server mode, the old database-connected CLI, and the shell client are not available. Read [Upgrading](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Upgrading) before replacing an older binary.

This repository continues the open-source orchestrator project originally created at GitHub and later maintained by Percona. The implementation is distributed under the Apache License 2.0; see [`LICENSE`](https://github.com/SisyphusSQ/orchestrator/blob/main/LICENSE) for the complete terms.
