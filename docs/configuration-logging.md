# Logging

`orchestrator` writes application logs to stderr so that CLI results on stdout remain machine-readable. The default logger uses Uber Zap v1.28.0 with a console encoder; JSON output and sampling are disabled.

Each application log line uses this format:

```text
2006-01-02 15:04:05\t[LEVEL]\t[caller]\tmessage
```

The timestamp uses the process local timezone. The caller uses a trimmed source path such as `[logic/orchestrator.go:123]`. Structured fields added with the SugaredLogger `With`, `Debugw`, `Infow`, `Warnw`, or `Errorw` methods follow the message without changing the first four fields.

## Levels

`Debug` and the `--debug` CLI option enable debug output. The `--verbose` option enables informational output. Without either option, the command-line default is error output; the `Debug` configuration value can enable debug output after configuration is loaded.

Library and application packages return errors to their callers. Only the process entry point emits a fatal log and terminates the process, including failures returned by CLI dispatch, HTTP listeners, continuous discovery, Raft, database initialization, and configuration loading.

Legacy log levels map to console and syslog levels as follows:

| Legacy level | Console level | syslog priority |
| -- | -- | -- |
| DEBUG | `[DEBUG]` | Debug |
| INFO | `[INFO]` | Info |
| NOTICE | `[INFO]` | Notice |
| WARNING | `[WARN]` | Warning |
| ERROR | `[ERROR]` | Err |
| CRITICAL | `[ERROR]` | Crit |
| FATAL | `[FATAL]` | Emerg |

## Syslog

Set `EnableSyslog` to `true` to duplicate application logs to the local syslog service with the `orchestrator` tag. Startup now fails if the configured syslog writer cannot be initialized; it no longer silently falls back to stderr-only logging. Syslog writes are synchronous and do not create a goroutine per entry, so local syslog availability and latency are part of the logging path.

`AuditToSyslog` controls the separate audit stream. `AuditLogFile` appends audit entries to a file. Audit file and syslog writes are also synchronous: a write or close failure is returned and logged instead of being discarded in a background goroutine.

Before enabling either syslog option, verify that the service is reachable from the same runtime namespace as `orchestrator` (host, container, or service sandbox) and that it accepts the `orchestrator` tag.
