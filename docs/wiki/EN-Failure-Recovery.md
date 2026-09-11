# Failure detection and recovery

**English** · [中文](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Failure-Recovery) · [Wiki home](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

Failure detection and recovery are separate stages. Every ready Raft node probes the MySQL topology and contributes observations; only the quorum-confirmed leader registers and executes recovery.

## Configure policy before enabling automation

- Enable automated recovery globally or for an explicit cluster alias in Web → Recovery Settings. Cluster overrides take precedence over global values and code defaults.
- Exclude hosts with `RecoveryIgnoreHostnameFilters`.
- Configure promotion rules, data-center/region constraints, replication lag thresholds, and GTID/Pseudo-GTID behavior.
- Treat pre/post-failover hooks as production code: bound their runtime, make failures visible, and avoid hidden non-idempotent retries.
- Enable and retain backend audit records when operational traceability is required.

A detected problem does not automatically mean a safe replacement exists. Candidate eligibility, replication state, filters, quorum, and current topology all participate in the decision.

## Detection and candidate semantics

Analysis distinguishes dead primaries, dead intermediate primaries, co-primary failures, unreachable members, stopped or lagging replication, and structural warnings. Some observations are informational and intentionally do not trigger recovery. A failure becomes actionable only after the configured detection window, valid topology evidence, and recovery policy agree.

Promotion rules (`must`, `prefer`, `neutral`, `prefer_not`, and `must_not`), data-center/region policy, version compatibility, errant GTIDs, replication filters, SQL delay, and lag determine candidate eligibility. `must_not` prevents promotion but does not remove an instance from discovery. Keep classification metadata current rather than trying to correct it during an incident.

GTID relocation is preferred when supported and compatible. Pseudo-GTID relies on equivalent markers inserted into binary logs and must be configured before failure. Automated injection requires privileges and a writable source; manual injection must run on every writable primary at a stable interval. Expired or missing markers can make file-position matching impossible.

## Manual operations

Inspect analysis first:

```sh
orch replication-analysis --output json
orch topology --cluster production
```

Then use a specific recovery command only after reviewing the current state:

```sh
orch recover --instance failed-primary.example.com:3306
```

Planned maintenance should use maintenance/downtime markers and, where appropriate, graceful takeover instead of simulating a crash. Follow the [planned primary switchover](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Planned-Switchover) runbook for candidate checks, the exact execution sequence, application fencing, readback, and partial-failure handling. A forced failover intentionally discards the current primary and has a larger blast radius.

For a controlled switch to a named direct replica:

```sh
orch graceful-master-takeover \
  --cluster production \
  --destination candidate.example.com:3306
```

The non-auto command repoints the old primary but leaves its replication stopped. The `graceful-master-takeover-auto` variant can choose a candidate and attempts to start the old primary after repointing; those are additional automation semantics, not merely a shorter command name.

Maintenance suppresses automated actions around an intentional instance operation; downtime changes how known problems are surfaced. Neither changes MySQL state by itself. Recovery acknowledgements close operator attention records but do not repair topology. Anti-flapping blocks, active recovery records, and postponed operations must be inspected before forcing another attempt.

Tags are operational metadata in `key` or `key=value` form and can drive searches or external policy. Tag writes, candidate registration, downtime, maintenance, and recoveries are business mutations even where a compatibility route uses GET; automation must use command semantics rather than HTTP method alone.

Recovery hooks receive incident context through documented environment variables. Run them with least privilege, bounded timeout, durable logging, and explicit ownership. A successful topology change with a failed hook is not an entirely successful external cutover.

## Acceptance and unknown results

Success is not established by HTTP 200 alone. Confirm the promoted instance and its replication state from MySQL, read the orchestrator topology and recovery record, verify Raft leadership/quorum, and inspect audit/hook outcomes. If a client reports exit code 3 or loses the response, do not replay immediately: determine whether a recovery was registered or completed first.

No automated test or dry run substitutes for a representative isolated topology exercise before production. Coordinate application routing, DNS/KV consumers, fencing, and external hooks as separate acceptance surfaces.
