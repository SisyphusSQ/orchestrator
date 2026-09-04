# ChangeLog

## Unreleased

- raft
  - Replace the 2017 openark Raft fork with official `github.com/hashicorp/raft` v1.7.3 and `github.com/hashicorp/raft-boltdb/v2` v2.3.1 for newly created clusters.
  - Require a durable `RaftNodeID` independent of bind/advertise/DNS, bootstrap a single seed voter, and manage membership through ID-aware HTTP APIs (`/api/raft/configuration`, `/bootstrap`, `/members`, `/leadership/transfer`, `/snapshot`).
  - Use official FileSnapshotStore plus one Bolt store for logs and stable state, remove Yield/peer/health-report control paths, and report readiness from VerifyLeader, configuration suffrage, and last-contact.
- optimization
  - Replace Martini and its auth, gzip, and render extensions with Gin v1.12.0 behind a project-owned HTTP transport adapter, preserving 306 API, Web, debug, and agent route contracts plus authentication, templates, static assets, Raft proxy termination, URL prefixes, and HTTP/HTTPS/Unix listener behavior.
  - Migrate stable orchestrator backend DAO reads and writes to GORM v1.31.2 with MySQL and SQLite drivers, while reusing the process-owned pool, retaining ordered SQL schema migrations, and keeping topology, snapshot, Raft, and `LastInsertId` paths explicitly scoped.
  - Remove the local `sqlutils` package and replace its remaining dynamic topology and snapshot helpers with context-aware, null-preserving adapters.
  - Build MySQL backend and topology connections from typed driver configuration, move pool ownership and shutdown into the process database runtime, separate discovery and topology-operation pools, and apply `MySQLTopologyMaxAllowedPacket` to the correct connections.
  - Replace the local application logger implementation with Uber Zap v1.28.0 while preserving the compatibility API, stderr routing, legacy syslog priorities, and explicit logger shutdown. The default text format is now `time\t[LEVEL]\t[caller]\tmessage`; deployments that parse logs must update their patterns before upgrading.
  - Make configured application and audit syslog initialization failures fatal during startup, and make audit file/syslog write failures observable to callers instead of discarding them in per-entry goroutines.
  - Return database initialization, TLS setup, hostname resolution, and configuration parsing failures to process entry points instead of terminating from library packages. Failed configuration reloads leave the active configuration unchanged and return an API error or SIGHUP log entry.
  - Propagate HTTP listener, continuous discovery, and Raft monitor failures to the process entry point. Raft retains only the first pending fatal runtime error and no longer creates a goroutine for each report.
  - Return CLI dispatch failures through `Cli` and `CliWrapper`, leaving process termination exclusively to `main` while preserving exit status, stderr logging, and stdout command output.
  - Replace `github.com/armon/consul-api` with a single official `github.com/hashicorp/consul/api` v1.34.3 client for ordinary KV and `consul-txn`, defaulting Consul TLS to certificate verification, sending ACL tokens only as `X-Consul-Token`, and failing CLI/continuous startup on Consul client or TLS file errors.
