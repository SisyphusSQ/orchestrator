# ChangeLog

## Unreleased

- optimization
  - Migrate stable orchestrator backend DAO reads and writes to GORM v1.31.2 with MySQL and SQLite drivers, while reusing the process-owned pool, retaining ordered SQL schema migrations, and keeping topology, snapshot, Raft, and `LastInsertId` paths explicitly scoped.
  - Remove the local `sqlutils` package and replace its remaining dynamic topology and snapshot helpers with context-aware, null-preserving adapters.
  - Build MySQL backend and topology connections from typed driver configuration, move pool ownership and shutdown into the process database runtime, separate discovery and topology-operation pools, and apply `MySQLTopologyMaxAllowedPacket` to the correct connections.
  - Replace the local application logger implementation with Uber Zap v1.28.0 while preserving the compatibility API, stderr routing, legacy syslog priorities, and explicit logger shutdown. The default text format is now `time\t[LEVEL]\t[caller]\tmessage`; deployments that parse logs must update their patterns before upgrading.
  - Make configured application and audit syslog initialization failures fatal during startup, and make audit file/syslog write failures observable to callers instead of discarding them in per-entry goroutines.
  - Return database initialization, TLS setup, hostname resolution, and configuration parsing failures to process entry points instead of terminating from library packages. Failed configuration reloads leave the active configuration unchanged and return an API error or SIGHUP log entry.
  - Propagate HTTP listener, continuous discovery, and Raft monitor failures to the process entry point. Raft retains only the first pending fatal runtime error and no longer creates a goroutine for each report.
  - Return CLI dispatch failures through `Cli` and `CliWrapper`, leaving process termination exclusively to `main` while preserving exit status, stderr logging, and stdout command output.
