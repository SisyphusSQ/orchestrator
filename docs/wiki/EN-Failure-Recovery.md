# Failure detection and recovery

**English** · [中文](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Failure-Recovery) · [Wiki home](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

Failure detection and recovery are separate stages. Every ready Raft node probes the MySQL topology and contributes observations; only the quorum-confirmed leader registers and executes recovery.

## Configure policy before enabling automation

- Define which clusters may recover with `RecoverMasterClusterFilters` and `RecoverIntermediateMasterClusterFilters`.
- Exclude hosts with `RecoveryIgnoreHostnameFilters`.
- Configure promotion rules, data-center/region constraints, replication lag thresholds, and GTID/Pseudo-GTID behavior.
- Treat pre/post-failover hooks as production code: bound their runtime, make failures visible, and avoid hidden non-idempotent retries.
- Enable and retain backend audit records when operational traceability is required.

A detected problem does not automatically mean a safe replacement exists. Candidate eligibility, replication state, filters, quorum, and current topology all participate in the decision.

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

Planned maintenance should use maintenance/downtime markers and, where appropriate, graceful takeover instead of simulating a crash. A forced failover intentionally discards the current primary and has a larger blast radius.

## Acceptance and unknown results

Success is not established by HTTP 200 alone. Confirm the promoted instance and its replication state from MySQL, read the orchestrator topology and recovery record, verify Raft leadership/quorum, and inspect audit/hook outcomes. If a client reports exit code 3 or loses the response, do not replay immediately: determine whether a recovery was registered or completed first.

No automated test or dry run substitutes for a representative isolated topology exercise before production. Coordinate application routing, DNS/KV consumers, fencing, and external hooks as separate acceptance surfaces.
