# External integrations
**English** · [中文](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Integrations) · [Wiki home](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

External systems sit outside orchestrator's consistency boundary. A committed Raft command does not prove Consul, Hooks, Agent, proxy, or telemetry succeeded. Read each system independently and assign its timeout and owner.

## Consul KV

Consul publishes cluster writer information. Configure address, scheme, datacenter, HTTP timeout, optional ACL token, and TLS CA/server name/client certificate. `skipVerify` is a time-bounded compatibility option only.

`consul.kv.clusterMasterPrefix` selects the key prefix and the provider is `consul`. A cluster update can contain multiple keys and is bounded by `maxKVsPerTransaction`. Cross-data-center distribution is not a cross-system transaction; remote timeout or partial success requires explicit readback and remediation.

Accept the integration by publishing one cluster, reading every key, comparing hostname/IP/port with the actual primary, and validating application resolution. Never expose ACL tokens in logs, shell history, or Wiki pages.

## Recovery Hooks

Page-managed profiles bind to nine phases and global/cluster scopes using `inherit`, `replace`, or `disable`. Commands run through `hooks.shellCommand` as the server identity. Profiles require a 1–3600 second timeout, 1024–1048576 byte output limit, and `continue` or `abort` failure policy.

Hooks are not external transactions inside Raft. Use a recovery UID as an idempotency key, explicit network timeouts, structured logs, and queryable external results. Do not put secrets in arguments; audit redaction covers common assignments but is not a secret manager.

Test Run executes on the real server. Start with a harmless payload and verify identity, working directory, PATH, DNS, certificates, and firewall.

## Agent and seed

With `agents.serveHTTP`, the console exposes Agents, active seeds, seed details, and restore tasks. Agent TLS is separate from the main HTTP TLS domain. Configure certificate, mTLS, CA, and OU independently. Poll, unseen, stale-seed, and send-delay settings control visibility and failure classification.

Custom commands and restores have real remote side effects. Verify hostname, mount, MySQL port, disk space, source seed, destination, and cancellation before execution; read Agent, task, target files, and MySQL afterward.

## Reverse proxy and identity

Basic, multi, proxy, token, and mTLS are enforced by the server. Trust proxy user headers only from a controlled proxy and strip external copies. Preserve URL prefix, scheme, host, required certificate identity, and timeouts. Never cache mutations or authentication responses.

## Prometheus and OpenTelemetry

Scrape `/metrics` under the configured prefix. Metrics are node-local; retain node/instance/cluster labels while controlling cardinality. `resources/metrics` contains dashboards and alerts.

`observability.tracing.endpoint` enables OTLP and `sampleRatio` controls sampling. The exporter is built at startup, so endpoint/TLS environment changes require restart. Telemetry failure is not proof of business success or failure; use response, audit, and target readback.

## Integration acceptance

1. Record caller, target, identity, scope, timeout, retry, and idempotency key.
2. Run one success and one expected failure against an isolated object.
3. Preserve orchestrator response/audit and external readback.
4. Simulate disconnect and confirm an unknown result is not silently retried.
5. Verify credential rotation, certificate expiry, permission revocation, and alerts.
6. Define manual stop conditions; never run a second controller competing for the same primary.

Built-in ZooKeeper publication has been removed. A new provider must be an explicit implemented and tested contract, not an implicit reuse of Consul names.
