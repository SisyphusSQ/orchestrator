# Upgrading orchestrator

Review the breaking changes on this page before replacing an existing `orchestrator` binary. Unreleased changes remain listed here until they are included in a release.

## Unreleased breaking changes

### Built-in ZooKeeper KV publishing removed

`orchestrator` no longer includes a ZooKeeper adapter or client dependencies. The internal KV store, Consul KV providers, `submit-masters-to-kv-stores` CLI/API, and Raft `put-key-value` command remain supported.

`ZkAddress` has also been removed from the configuration schema. A configuration file containing that field now causes startup or configuration reload to exit with an error, even when its value is empty. This explicit failure prevents an old configuration from being accepted while ZooKeeper master publishing silently stops.

Before upgrading:

1. Check every configuration file or generated configuration layer for `ZkAddress`, including files loaded during a reload.
2. Migrate master discovery consumers to Consul KV or implement an [external recovery hook](configuration-recovery.md#hooks) that maintains ZooKeeper or another external store.
3. Remove `ZkAddress` from all configuration layers.
4. Validate master discovery after a manual `submit-masters-to-kv-stores` request and after a representative failover in an environment that contains the real external consumers.

This change does not migrate existing ZooKeeper data or consumers. If rollback is required before consumers have migrated, restore a pre-removal binary together with its matching configuration; do not add `ZkAddress` back to the new binary.
