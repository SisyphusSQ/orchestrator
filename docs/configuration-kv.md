# Configuration: Key-Value stores

`orchestrator` supports these key-value stores:

- An internal store based on a relational table
- [Consul](https://github.com/hashicorp/consul)

`orchestrator` supports master discovery by storing clusters' masters in KV.

```json
  "KVClusterMasterPrefix": "mysql/master",
  "ConsulAddress": "127.0.0.1:8500",
  "ConsulCrossDataCenterDistribution": true,
```

`KVClusterMasterPrefix` is the prefix to use for master discovery entries. As example, your cluster alias is `mycluster` and the master host is `some.host-17.com` then you will expect an entry where:

- The Key is `mysql/master/mycluster`
- The Value is `some.host-17.com:3306`

#### Breakdown entries

In addition to the above, `orchestrator` also breaks down the master entries and adds the follows (illustrating via example above):

- `mysql/master/mycluster/hostname`, value is `some.host-17.com`
- `mysql/master/mycluster/port`, value is `3306`
- `mysql/master/mycluster/ipv4`, value is `192.168.0.1`
- `mysql/master/mycluster/ipv6`, value is `<whatever>`

The `/hostname`, `/port`, `/ipv4` and `/ipv6` extensions are automatically added for any master entry.

### Stores

If specified, `ConsulAddress` indicates an address where a Consul HTTP service is available. If unspecified, no Consul access is attempted.

### ZooKeeper removal

Built-in ZooKeeper publishing has been removed. `ZkAddress` is no longer a supported configuration field; if it is present, including with an empty value or different letter casing, `orchestrator` exits with an upgrade error instead of silently ignoring it.

Before upgrading, migrate master discovery consumers to Consul KV or an [external recovery hook](configuration-recovery.md#hooks), then remove `ZkAddress` from every configuration layer. See the [upgrade notes](upgrading.md#built-in-zookeeper-kv-publishing-removed) for the required checks. An external hook or independently managed integration may still publish the master identity to ZooKeeper, but `orchestrator` no longer includes a ZooKeeper client or write path.

### Consul specific

See [kv](kv.md) documentation for Consul specific settings.
