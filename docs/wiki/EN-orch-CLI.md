# orch CLI

**English** · [中文](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-orch-CLI) · [Wiki home](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

`orch` is the standalone Go client for remote administration. It talks only to the HTTP API and needs neither the server configuration nor database access.

## Build and connect

```sh
make cli
export ORCH_ENDPOINT="https://orchestrator.example.com"
bin/orch clusters
bin/orch topology --cluster production
bin/orch replication-analysis --output json
```

An endpoint without `/api` gets that suffix automatically. `--endpoint` may be repeated or comma-separated; `ORCH_ENDPOINT` also accepts spaces. With multiple endpoints the client probes leader checks, selects an API endpoint, and sends the business request once. Node-local commands such as bootstrap and snapshot require exactly one endpoint.

## Authentication and TLS

| Option | Environment | Purpose |
| --- | --- | --- |
| `--user`, `--password` | `ORCH_USER`, `ORCH_PASSWORD` | Basic or multi authentication |
| `--token` | `ORCH_TOKEN` | `public:secret` access-token cookie |
| `--header` | — | Repeatable trusted-proxy header |
| `--ca` | `ORCH_CA` | Additional trusted CA PEM |
| `--cert`, `--key` | `ORCH_CERT`, `ORCH_KEY` | Paired mTLS client material |
| `--timeout` | `ORCH_TIMEOUT` | Per-request timeout; default 30 seconds |
| `--output` | — | `text` or `json` |

Prefer environment variables or a secret manager for passwords and tokens. The client always verifies TLS and has no insecure skip-verify option.

## Common operations

```sh
orch discover --instance db-1.example.com:3306
orch begin-maintenance --instance db-1.example.com:3306 \
  --owner operator --reason planned-change
orch relocate --instance replica.example.com:3306 \
  --destination primary.example.com:3306
orch recover --instance failed-primary.example.com:3306
orch raft-configuration --output json
orch which-api
```

Use `orch help <command>` for the exact flags registered by the current binary. `orch api PATH` is an escape hatch for a relative API path, but its side effects are treated as unknown and it must not be blindly retried.

## Failure semantics

Exit code 0 means confirmed success; 1 is an HTTP, authentication, server-business, or rendering failure; 2 is invalid local usage/configuration; 3 means a mutating result is unknown and requires readback; 4 means a batch partially succeeded before a confirmed failure. HTTP 200 with API `Code=ERROR` is still a failure. Cancellation or timeout does not prove the server rolled back an operation.
