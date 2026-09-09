# Development

**English** · [中文](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Development) · [Wiki home](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

The repository uses one root Go module for the server and a nested module under `tools/orch-cli` for the client. The current baseline is Go 1.26.8, Node.js 22.22.2 or newer, and pnpm 10.33.2; always re-read `go.mod` and `web/package.json` when preparing a build.

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
| `docs/schema/` | executable metadata DDL, compatibility, migration, and generation checks |
| `docs/verification/` | issue-specific evidence excluded from Wiki publication |
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

## Validation layers

| Entry point | What it establishes |
| --- | --- |
| `make fmt-check` | Go formatting without rewriting files |
| `make test-unit` | root packages and independent CLI tests |
| `make test-integration` | configured core integration suite |
| `make test-docs` | Wiki pairing, navigation, links, schema index, and docs boundaries |
| `make test-web` | frontend type checking and unit tests |
| `make test-storybook` | isolated component rendering and interactions |
| `pnpm --dir web test:e2e` | browser behavior against test fixtures |
| `make build` | embedded server and standalone client build |

Container, package, CVE, system, and Raft targets are listed by `make help`. They can download dependencies, build images, or start containers; inspect the target and environment before running it. CI uses the same Make entry points and also checks the generated metadata schema against isolated supported database engines when credentials and services are available.

Build success, unit/integration tests, browser fixtures, package creation, Release publication, deployment readiness, and live MySQL/Raft acceptance are separate evidence surfaces. Report any layer that was not run instead of inferring it from another.

For frontend hot reload:

```sh
ORCH_API_TARGET=http://127.0.0.1:3000 make web-dev
```

For isolated UI states, run `make storybook`. Mocked Storybook/browser fixtures prove UI behavior only; they do not prove a real MySQL topology, production Raft, authentication proxy, or recovery.

## Documentation workflow

Edit both `EN-*.md` and `ZH-*.md`, keep `managed-pages.txt` and navigation synchronized, then run `make test-docs`. After the source commit is merged, publish with `script/publish-wiki`. The publication commit records the source revision and preserves Wiki pages outside the managed list.

Contributions should keep runtime contracts and upgrade documentation synchronized. Separate build success, automated tests, packaging, release publication, deployment, and real environment acceptance in delivery reports.

The repository intentionally has no prose documentation tree outside `docs/wiki/`. Durable machine-readable contracts stay next to their owners (`conf/`, `internal/`, `tools/orch-cli`, `resources/`, and `docs/schema/`); historical prose remains available through Git history.
