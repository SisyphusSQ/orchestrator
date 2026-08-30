# Upgrading orchestrator

Review the breaking changes on this page before replacing an existing `orchestrator` binary. Unreleased changes remain listed here until they are included in a release.

## Unreleased breaking changes

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
