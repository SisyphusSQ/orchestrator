# Development

**English** · [中文](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Development) · [Wiki home](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

The repository uses one root Go module for the server and a nested module under `tools/orch-cli` for the client. Use the versions declared by `go.mod` and `web/package.json`; do not substitute remembered toolchain versions.

## Source layout

| Path | Responsibility |
| --- | --- |
| `cmd/orchestrator/` | server and local administrative entry points |
| `internal/` | application packages, HTTP, Raft, discovery, recovery, configuration, storage |
| `tools/orch-cli/` | independent HTTP client module |
| `web/` | React/TypeScript/Ant Design console, Storybook, browser tests |
| `conf/` | checked-in configuration examples |
| `resources/metrics/` | Grafana dashboard and Prometheus rule examples |
| `docs/wiki/` | bilingual GitHub Wiki source |
| `script/`, `tests/` | build compatibility scripts and test suites |

## Common commands

```sh
make help
make deps
make web-deps
make build
make test-unit
make test-integration
make test-docs
make test-web
make storybook
```

`make binary` builds the Web app, synchronizes its output into the embed source, and builds `bin/orchestrator`. `make build` also builds `bin/orch`. A direct `go build` does not prepare production Web assets.

For frontend hot reload:

```sh
ORCH_API_TARGET=http://127.0.0.1:3000 make web-dev
```

For isolated UI states, run `make storybook`. Mocked Storybook/browser fixtures prove UI behavior only; they do not prove a real MySQL topology, production Raft, authentication proxy, or recovery.

## Documentation workflow

Edit both `EN-*.md` and `ZH-*.md`, keep `managed-pages.txt` and navigation synchronized, then run `make test-docs`. After the source commit is merged, publish with `script/publish-wiki`. The publication commit records the source revision and preserves Wiki pages outside the managed list.

Contributions should keep runtime contracts and upgrade documentation synchronized. Separate build success, automated tests, packaging, release publication, deployment, and real environment acceptance in delivery reports.
