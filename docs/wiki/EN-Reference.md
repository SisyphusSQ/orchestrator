# Reference

**English** · [中文](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Reference) · [Wiki home](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

The bilingual Wiki is the maintained entry point for user, operator, and contributor guides. Repository links below point to executable contracts, architecture decisions, generated assets, or issue-specific evidence rather than a competing guide set.

## Documentation map

- [Overview](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Overview): architecture, interfaces, capability boundary, and project lineage.
- [Getting started](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Getting-Started): build, single-node bootstrap, discovery, and production preflight.
- [Production deployment](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Production-Deployment) and [Backup and restore](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Backup-and-Restore): production layout, startup acceptance, consistency domains, replacement, and disaster handling.
- [Configuration](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Configuration): Raft identity, backends, discovery, recovery policy, KV, logging, and removed settings.
- [Configuration reference](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Configuration-Reference): all top-level domains, key defaults, sensitive settings, and restart boundaries.
- [Topology discovery](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Topology-Discovery) and [Topology operations](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Topology-Operations): discovery identity, filtering/classification, relocation choice, readback, and stop conditions.
- [Raft operations](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Raft-Operations): cluster formation, membership, replacement, quorum, health, and backup boundaries.
- [orch CLI](https://github.com/SisyphusSQ/orchestrator/wiki/EN-orch-CLI), [Web console](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Web-Console), and [HTTP API](https://github.com/SisyphusSQ/orchestrator/wiki/EN-HTTP-API): supported management interfaces and failure semantics.
- [CLI reference](https://github.com/SisyphusSQ/orchestrator/wiki/EN-CLI-Reference) and [API reference](https://github.com/SisyphusSQ/orchestrator/wiki/EN-API-Reference): complete CLI command index and HTTP route-family contract.
- [Integrations](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Integrations) and [Troubleshooting](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Troubleshooting): Consul, Hooks, Agent, proxy/telemetry acceptance, and symptom-driven triage.
- [Failure recovery](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Failure-Recovery), [planned switchover](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Planned-Switchover), and [Recovery settings](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Recovery-Configuration): analysis, candidate selection, controlled primary moves, 23 page-managed policies, nine hook phases, and acceptance.
- [Observability](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Observability) and [Security](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Security): node-local telemetry, authentication, TLS, credentials, and privilege boundaries.
- [Upgrading](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Upgrading), [Development](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Development), and the [package guide](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Package-Guide): breaking-change ledger, rollback, source layout, package ownership, build, test, and publication workflows.

## Executable and machine-readable contracts

- Configuration samples: [`conf/`](https://github.com/SisyphusSQ/orchestrator/tree/main/conf); complete fields and validation: [`internal/config/config.go`](https://github.com/SisyphusSQ/orchestrator/blob/main/internal/config/config.go).
- Metadata schema: [`docs/schema/`](https://github.com/SisyphusSQ/orchestrator/tree/main/docs/schema), including executable MySQL DDL, compatibility, and migration guidance.
- HTTP route registration: [`internal/http/routes.go`](https://github.com/SisyphusSQ/orchestrator/blob/main/internal/http/routes.go); CLI catalog: [`tools/orch-cli/internal/cmd/catalog.json`](https://github.com/SisyphusSQ/orchestrator/blob/main/tools/orch-cli/internal/cmd/catalog.json).
- Architecture decisions: [`docs/architecture/`](https://github.com/SisyphusSQ/orchestrator/tree/main/docs/architecture); generated documentation screenshots: [`docs/assets/screenshots/`](https://github.com/SisyphusSQ/orchestrator/tree/main/docs/assets/screenshots).
- Web source and browser tests: [`web/`](https://github.com/SisyphusSQ/orchestrator/tree/main/web).
- Metrics, dashboards, and alerts: [`resources/metrics/`](https://github.com/SisyphusSQ/orchestrator/tree/main/resources/metrics).
- Build and validation entry points: [`Makefile`](https://github.com/SisyphusSQ/orchestrator/blob/main/Makefile), [`script/`](https://github.com/SisyphusSQ/orchestrator/tree/main/script), and [`tests/`](https://github.com/SisyphusSQ/orchestrator/tree/main/tests).

## Project and history

This maintained fork derives from [Percona orchestrator](https://github.com/percona/orchestrator) and the original [openark/orchestrator](https://github.com/openark/orchestrator), authored by Shlomi Noach. Historical pages, presentations, screenshots, and superseded commands remain available in Git history but are not current product documentation. The repository is licensed under the [Apache License 2.0](https://github.com/SisyphusSQ/orchestrator/blob/main/LICENSE).

When prose and executable behavior disagree, treat current code and tests as implementation evidence, then fix both language pages in the same change. Report issues at [SisyphusSQ/orchestrator](https://github.com/SisyphusSQ/orchestrator/issues).
