# Web console

**English** · [中文](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Web-Console) · [Wiki home](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

The production Web console is a React, TypeScript, and Ant Design application embedded in `bin/orchestrator`. It shares the service listener, URL prefix, authentication, and TLS policy. No `resources/`, `web/`, Node.js, or separate static server is required at runtime.

Open `/web/clusters` under the configured service origin. If `URLPrefix` is `/orchestrator`, use `/orchestrator/web/clusters`.

## Operator workflow

The console provides cluster and problem summaries, topology and list views, instance details, discovery/search, maintenance and downtime controls, topology refactoring, recovery views, audit history, and optional Agent/seed pages. Available actions reflect server capabilities and read-only policy.

Mutating operations require confirmation and are disabled while in flight. The UI checks both HTTP status and the API response `Code`. A disconnected or timed-out operation is displayed as unknown rather than automatically replayed. Read back the topology, recovery, maintenance, and audit state before taking another action.

A follower may proxy supported operations to the leader. The leader still performs final authorization, readiness, and topology checks. Browser hints such as drag-and-drop validation never replace server validation.

## Authentication and safety

The console follows the server's anonymous, Basic, multi, proxy, token, read-only, TLS, and mTLS configuration. `/api/web-config` contains UI capabilities, not secrets. Credentials must not be embedded in frontend source or URLs. Reverse proxies must preserve the configured URL prefix, trusted identity headers, and browser origin checks.

## Development preview

Use `make web-dev` for Vite hot reload and `make storybook` for isolated component/business-state scenarios. These development servers are loopback-only by default and are not deployment artifacts. See [Development](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Development) for prerequisites and verification boundaries.
