# Configuration

**English** · [中文](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Configuration) · [Wiki home](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

The server reads JSON configuration from an explicit `--config` path, or searches `/etc/orchestrator.conf.json`, `conf/orchestrator.conf.json`, then `orchestrator.conf.json`. Configuration contains database and authentication secrets; keep it outside public artifacts and restrict filesystem access.

## Required server identity

Every server start requires:

```json
{
  "RaftNodeID": "node-1",
  "RaftDataDir": "/var/lib/orchestrator/raft",
  "RaftBind": "10.0.0.1:10008",
  "RaftAdvertise": "10.0.0.1:10008",
  "ListenAddress": ":3000"
}
```

`RaftNodeID` is a durable identity, not an address. `RaftAdvertise` defaults to normalized `RaftBind`; set it explicitly behind NAT. These values, the HTTP listener, backend connection pools, and OpenTelemetry exporter configuration require a restart when changed.

## Metadata backend

Choose one independent backend per Raft node:

- MySQL: configure `MySQLOrchestratorHost`, port, database, user, and password or the credentials file.
- SQLite: set `BackendDB` to `sqlite` and provide an absolute writable `SQLite3DataFile`.

Do not point several Raft nodes at one shared metadata database. Backup both the metadata backend and the Raft data directory according to their distinct consistency requirements.

## Topology access and behavior

Configure `MySQLTopologyUser` and its password or credentials file on every node. The account must read replication state on every discovered instance and needs additional privileges for requested topology changes. Discovery seeds, hostname resolution, instance filters, promotion rules, recovery filters, hooks, audit sinks, Consul, authentication, TLS, and URL prefixes are independent policy choices.

Use the checked-in MySQL and SQLite samples under [`conf/`](https://github.com/SisyphusSQ/orchestrator/tree/main/conf). The complete field definition is [`internal/config/config.go`](https://github.com/SisyphusSQ/orchestrator/blob/main/internal/config/config.go); detailed topic pages remain under [`docs/`](https://github.com/SisyphusSQ/orchestrator/tree/main/docs).

## Removed settings

Do not carry forward `RaftEnabled`, `ZkAddress`, Graphite settings, or legacy in-memory metric retention settings. They are rejected so removed behavior cannot fail silently. Unknown fields may still be accepted for compatibility, so acceptance must test intended behavior rather than treating startup alone as proof.
