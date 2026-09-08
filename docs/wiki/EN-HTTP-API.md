# HTTP API

**English** · [中文](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-HTTP-API) · [Wiki home](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

The HTTP API is the contract used by both `orch` and the Web console. Its default base path is `/api`; prepend `URLPrefix` when configured.

## Response contract

Many endpoints return an envelope with `Code`, `Message`, and `Details`. Always evaluate the business `Code` in addition to HTTP status. Some compatibility endpoints can report a business error with HTTP 200.

Reads include:

```http
GET /api/clusters
GET /api/instance/db.example.com/3306
GET /api/raft/configuration
GET /health/ready
```

Raft membership uses explicit HTTP methods and JSON:

```http
POST /api/raft/members
Content-Type: application/json

{"id":"node-2","address":"node-2:10008","suffrage":"voter"}
```

The authoritative route registration is [`internal/http/api.go`](https://github.com/SisyphusSQ/orchestrator/blob/main/internal/http/api.go). The generated `orch` command catalog in [`tools/orch-cli/internal/cmd/catalog.json`](https://github.com/SisyphusSQ/orchestrator/blob/main/tools/orch-cli/internal/cmd/catalog.json) documents the supported client mapping.

## Routing and retries

Raft followers can proxy supported business requests to the leader. Node-local health, metrics, bootstrap, snapshot, and configuration readback stay on the addressed node. Use a leader-aware proxy or `orch` for normal automation.

Historical API compatibility includes some mutating GET routes. Therefore HTTP method alone is not a safe retry classifier. If a mutating request times out or disconnects after transmission, read back the resource or audit state before retrying. Use compare-and-set fields such as Raft `expectedIndex` where available.

## Security

Use TLS, authentication, and least-privilege network access. Never put credentials in a URL. With proxy authentication, strip user-supplied identity headers at the edge and set the trusted header only after successful proxy authentication. Prefer `orch` unless an integration needs the raw API; it already implements response checking and explicit unknown-result exits.
