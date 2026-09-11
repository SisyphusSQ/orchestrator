# Topology discovery and classification
**English** · [中文](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Topology-Discovery) · [Wiki home](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

Discovery converts live MySQL replication state into the topology model. It is continuous, not a one-time import: schedulers poll known instances, follow replication relationships, normalize hostnames, apply filters, and persist results in each node's own metadata database.

## Contract before discovery

The topology account must connect and read version, global variables, replication state, binlog, and GTID data. Mutations require additional privileges; do not grant broad administration merely to simplify discovery. Every Raft node needs credentials and TLS trust that cover the same instance set.

Verify stable DNS/custom resolution, reachable MySQL `report_host`/`report_port`, firewall paths from every server node, clocks, server UUIDs, and expected replication channels.

## Seeds and expansion

- `topology.discovery.seeds` supplies stable initial anchors after startup.
- `orch discover --instance host:port` performs an immediate read for acceptance or ad-hoc discovery.
- Known instances refresh at `pollSeconds`; failed instances use the dead-poll cadence.
- `useShowReplicaHosts` can follow replicas reported by a primary, but reported addresses still must be reachable.
- Forgetting an instance does not stop rediscovery through replication. Use intentional filters or fix upstream identity for durable exclusion.

## Hostname normalization

`topology.hostname.resolveMethod` selects input resolution. `mysqlResolveMethod` may obtain the canonical name from MySQL (default `@@hostname`). `resolveExpiryMinutes` controls cache lifetime and `rejectResolvePattern` rejects unsafe results. Export mappings before changing policy: recognizing one physical server by two names creates duplicates and corrupts alias and candidate decisions.

Troubleshoot in order: network → TLS/account → MySQL-reported identity → DNS/cache → ignore filters → metadata persistence. Do not loosen filters first merely because a host disappeared.

## Filter semantics

| Setting | Effect |
| --- | --- |
| `ignoreHostnames` | Excludes matching hosts from general discovery |
| `ignoreReplicaHostnames` | Excludes matching hosts discovered as replicas |
| `ignoreMasterHostnames` | Excludes matching upstreams |
| `ignoreReplicationUsernames` | Ignores relationships using matching replication users |
| `recoveryIgnoreHostnameFilters` | Recovery-only policy; it does not hide discovery |
| `promotionIgnoreHostnameFilters` | Forbids promotion; it does not hide topology |

Treat values according to current regular-expression behavior. Test both positive and negative representative hostnames and inspect filter logs. An over-broad expression can remove an entire site.

## Cluster, location, and candidate classification

Cluster name normally derives from primary identity; use an explicit unique cluster alias as the stable human interface. Queries and patterns can populate instance alias, cluster domain, data center, region, physical environment, promotion rule, and semi-sync enforcement.

Promotion considers coordinates/GTID, SQL and IO threads, lag, filters, version, site policy, promotion rule, read-only state, and semi-sync. Missing classification must not mean “same site.” Read every possible candidate's classification before automatic recovery.

## GTID and Pseudo-GTID

Prefer GTID relocation in GTID topologies and check errant transactions. Non-GTID environments use file:position or Pseudo-GTID. Pseudo-GTID requires continuous markers, a pattern/query/monotonic hint, sufficient binlog retention, and permissions. Missing or purged markers make some smart relocations unsafe.

## Acceptance and monitoring

```sh
orch --endpoint https://orchestrator.example discover --instance db1.example:3306
orch --endpoint https://orchestrator.example which-instance --instance db1.example:3306
orch --endpoint https://orchestrator.example which-cluster --cluster db1.example:3306
orch --endpoint https://orchestrator.example topology --cluster production
```

A 200 response is not enough. Identity must be unique, replication edges correct, classification intentional, and state stable after the next poll. Monitor queue depth, last successful check, problem instances, resolution cache, and filter logs. Stop mutations and automatic recovery when discovery is stale.
