# Upgrading orchestrator

Review the breaking changes on this page before replacing an existing `orchestrator` binary. Unreleased changes remain listed here until they are included in a release.

## Unreleased breaking changes

### HTTP routing now uses Gin behind a compatibility adapter

The standard Web/API listener and the agent listener now use Gin v1.12.0
instead of Martini. No HTTP configuration keys or listener selection rules have
changed. Existing API synonyms, optional trailing slashes, GET-to-HEAD
compatibility, authentication modes, `URLPrefix`, custom `StatusEndpoint`,
template layout, static assets, debug endpoints, and HTTP/HTTPS/Unix socket
selection remain supported through a project-owned transport adapter.

Before rollout, exercise representative read and mutating requests with the
deployment's authentication mode and URL prefix. Deployments using mutual TLS
or Raft should additionally validate certificate OU rejection and follower
proxy behavior. Confirm the Web UI assets and debug endpoints through the same
reverse proxy used in production. A rollback requires only the prior binary and
matching configuration; this migration adds no persistent state or schema
change.

### Backend DAO now uses GORM without schema ownership

Backend DAO queries now use GORM for both MySQL and SQLite, while reusing the
single process-owned backend connection pool. The configured backend, schema,
configuration keys, and on-disk SQLite format are unchanged. Ordered SQL
bootstrap and patch statements remain the only schema migration mechanism;
GORM does not run `AutoMigrate` or other implicit DDL.

Before rollout, validate startup and representative backend reads and writes
against the deployment's configured backend. Pay particular attention to
recovery registration, maintenance registration, and agent seed operations,
whose generated identifiers continue to come from an explicit `database/sql`
adapter. A rollback requires only the prior binary and matching configuration;
this migration adds no database patch of its own.

### Database connections now have an explicit process lifecycle

Backend and topology connections are now created from typed MySQL driver
configuration and owned by the `orchestrator` process. Shutdown closes the
backend pool, the discovery pools, and the topology-operation pools. The
`MySQLTopologyMaxAllowedPacket` setting now applies to topology connections;
previous builds accidentally wrote that value to an unrelated cached backend
DSN, so deployments that set it may observe the intended topology packet limit
for the first time.

Database connection settings reloaded with `SIGHUP` do not rebuild pools that
are already open. Restart every `orchestrator` process after changing database
endpoints, credentials, TLS files or verification, connection timeouts, packet
limits, lifetime, or pool sizing. No configuration keys or JSON value formats
changed in this migration.

### Log output and syslog failure behavior changed

Application logging now uses Uber Zap v1.28.0 and writes text lines to stderr in the form `time\t[LEVEL]\t[caller]\tmessage`. The added caller field and Tab separators replace the previous space-separated format. Console output remains plain text, not JSON, and CLI data on stdout remains separate. Update log parsers, collection rules, and alerts that depend on the previous layout before replacing the binary.

When `EnableSyslog` is `true`, failure to initialize the local syslog writer now stops startup instead of silently continuing with stderr only. The same explicit startup failure applies to `AuditToSyslog`. Confirm syslog access from the actual host, container, or service sandbox before rollout.

Application and audit syslog writes no longer start an unbounded goroutine per entry. They are synchronous, and audit file/syslog errors are returned and logged. Verify local syslog latency under representative load and ensure the service is supervised before enabling these sinks.

### Built-in ZooKeeper KV publishing removed

`orchestrator` no longer includes a ZooKeeper adapter or client dependencies. The internal KV store, Consul KV providers, `submit-masters-to-kv-stores` CLI/API, and Raft `put-key-value` command remain supported.

`ZkAddress` has also been removed from the configuration schema. A configuration file containing that field now causes startup or configuration reload to exit with an error, even when its value is empty. This explicit failure prevents an old configuration from being accepted while ZooKeeper master publishing silently stops.

Before upgrading:

1. Check every configuration file or generated configuration layer for `ZkAddress`, including files loaded during a reload.
2. Migrate master discovery consumers to Consul KV or implement an [external recovery hook](configuration-recovery.md#hooks) that maintains ZooKeeper or another external store.
3. Remove `ZkAddress` from all configuration layers.
4. Validate master discovery after a manual `submit-masters-to-kv-stores` request and after a representative failover in an environment that contains the real external consumers.

This change does not migrate existing ZooKeeper data or consumers. If rollback is required before consumers have migrated, restore a pre-removal binary together with its matching configuration; do not add `ZkAddress` back to the new binary.

### Consul uses the official Go SDK with strict TLS by default

`orchestrator` no longer uses `github.com/armon/consul-api`. Ordinary Consul KV and `consul-txn` now share one official `github.com/hashicorp/consul/api` v1.34.3 client. ACL tokens are sent only as `X-Consul-Token`.

Previous HTTPS clients set `InsecureSkipVerify: true` unconditionally. The new default verifies certificates against the system trust store, or against `ConsulTLSCAFile` / `ConsulTLSCAPath` when configured. `CONSUL_HTTP_SSL_VERIFY` cannot turn verification off. Historical HTTPS deployments that relied on skipped verification must configure a trusted CA and `ConsulTLSServerName` before upgrading. Only a temporary compatibility path should set `"ConsulTLSSkipVerify": true`, which logs one non-sensitive startup warning.

Client construction and TLS file errors now fail CLI and continuous-mode startup instead of logging and continuing with a nil Consul client. `ConsulCrossDataCenterDistribution` requires `ConsulAddress`. Cross-DC updates can still succeed in some datacenters and fail in others; the caller receives an aggregated error and successful datacenters are not rolled back.

Consul settings are not rebuilt on `SIGHUP` configuration reload. Restart every `orchestrator` process after changing Consul address, scheme, token, datacenter, TLS files, skip-verify, timeout, or provider. `ConsulHttpTimeoutSeconds` defaults to `60`; `0` means no overall deadline. Timed-out writes are not retried.

Before upgrading:

1. Confirm every Consul HTTPS endpoint presents a certificate that the process trust store, or an explicit CA setting, will accept.
2. Set `ConsulTLSServerName` when the certificate hostname does not match the configured address.
3. Configure `ConsulTLSCertFile` and `ConsulTLSPrivateKeyFile` together if Consul requires mTLS.
4. Keep `ConsulTLSSkipVerify` false unless a short-lived compatibility window is required.
5. Validate with unit/fixture tests, then an isolated Consul HTTP/HTTPS/ACL/mTLS environment. Existing system tests that only grep CLI output do not prove Consul KV contents.

Rollback restores the previous binary and its matching configuration. The new TLS fields do not change KV data already stored in Consul. Do not add the removed armon client back to the new binary.
