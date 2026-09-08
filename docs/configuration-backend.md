# Configuration: backend

Let orchestrator know where to find backend database. In this setup `orchestrator` will serve HTTP on port `:3000`.

```json
{
  "Debug": false,
  "ListenAddress": ":3000",
}
```

You may choose a MySQL-protocol backend (MySQL 5.7–8.0, TiDB, or OceanBase MySQL mode) or a `SQLite` backend. The shared DDL contract and exact validation boundaries are documented under [Metadata schema](schema/README.md). See [High availability](high-availability.md) for deployment scenarios; every Raft node uses its own independent backend.

## MySQL backend

You will need to set up schema & credentials:

```json
{
  "MySQLOrchestratorHost": "orchestrator.backend.master.com",
  "MySQLOrchestratorPort": 3306,
  "MySQLOrchestratorDatabase": "orchestrator",
  "MySQLOrchestratorCredentialsConfigFile": "/etc/mysql/orchestrator-backend.cnf",
}
```

Notice `MySQLOrchestratorCredentialsConfigFile`. It will be of the form:
```
[client]
user=orchestrator_srv
password=${ORCHESTRATOR_PASSWORD}
```

where either `user` or `password` can be in plaintext or get their value from the environment.


Alternatively, you may choose to use plaintext credentials in the config file:

```json
{
  "MySQLOrchestratorUser": "orchestrator_srv",
  "MySQLOrchestratorPassword": "orc_server_password",
}
```

#### MySQL-compatible backend DB setup

For a MySQL-compatible backend DB, grant the necessary privileges using the target product's account syntax. The following example is native MySQL syntax:

```
CREATE USER 'orchestrator_srv'@'orc_host' IDENTIFIED BY 'orc_server_password';
GRANT ALL ON orchestrator.* TO 'orchestrator_srv'@'orc_host';
```

#### Configuring max_allowed_packet size sent by Orchestrator
When Orchestrator communicates with backend MySQL instance or with managed
instances, it has to respect the instance's `max_allowed_packet` parameter
value. The following two options allow configuring this area:

```
MySQLOrchestratorMaxAllowedPacket
MySQLTopologyMaxAllowedPacket
```
Allowed values are:

`-1` - use the value hardcoded in the driver

`0` - let the driver to query the max packet value automatically from the server (only once per connection at the connection begin)

`> 0` - use the value provided

The two settings are applied independently. `MySQLOrchestratorMaxAllowedPacket`
only configures the backend connection, while `MySQLTopologyMaxAllowedPacket`
only configures connections to managed MySQL instances.

#### Connection pool lifecycle

`orchestrator` maps the backend and topology settings into typed MySQL driver
configuration. It owns one backend pool for the lifetime of the process and
separate topology pools for discovery and topology operations. Discovery and
topology-operation pools remain distinct even when their configured timeouts
are equal, so a slow operation cannot consume the discovery pool's identity.

The process closes all owned pools during normal shutdown and `SIGTERM`.
Connection-affecting configuration changes loaded by `SIGHUP` apply to newly
constructed configuration, but existing process-owned pools are not rebuilt.
Restart `orchestrator` after changing database endpoints, credentials, TLS,
timeouts, packet limits, or pool sizing.

## SQLite backend

Default backend is `MySQL`. To setup `SQLite`, use:

```json
{
  "BackendDB": "sqlite",
  "SQLite3DataFile": "/var/lib/orchestrator/orchestrator.db",  
}
```

`SQLite` is embedded within `orchestrator`.

If the file indicated by `SQLite3DataFile` does not exist, `orchestrator` will create it. It will need write permissions on given path/file.

The SQLite backend also uses one process-owned pool, limited to one open and
idle connection, and is closed through the same shutdown lifecycle as MySQL.

## Backend data-access boundary

Runtime reads and writes against the orchestrator backend use GORM with the
configured MySQL or SQLite dialect. GORM is bound to the same process-owned
`database/sql` pool described above; it does not create or close a second pool.
Statements that require a driver-provided `LastInsertId` use a small explicit
`database/sql` adapter over that same pool.

Schema ownership remains explicit and never moves to GORM `AutoMigrate` or
`Migrator`. Empty databases are initialized from the executable
[`docs/schema/mysql.sql`](schema/mysql.sql) contract. Databases that already
contain orchestrator tables continue through the ordered `generateSQLBase` and
`generateSQLPatches` compatibility stream; the two layouts are recorded in
`orchestrator_schema_migrations`. See the [migration guide](schema/migration-guide.md)
before upgrading or rolling back. Connections to managed topology instances,
Raft storage, dynamic topology result sets, and snapshot table transfer remain
outside the backend GORM DAO boundary.

The GORM statement logger records duration and errors without rendering SQL or
bound values. Existing database credentials and query parameters are therefore
not added to application logs by this data-access layer.
