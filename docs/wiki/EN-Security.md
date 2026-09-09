# Security

**English** · [中文](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Security) · [Wiki home](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

`orchestrator` can change production replication topology. Place the HTTP listener and Raft ports on controlled networks, use authenticated TLS for untrusted paths, and grant database privileges according to the operations you actually enable.

## HTTP authentication

Supported policies include:

- `basic`: one configured HTTP username/password.
- `multi`: the configured power credential plus a read-only user.
- `proxy`: trust an identity header set by an authenticating reverse proxy; `authentication.power.users` controls writers.
- Recovery policies and hooks require the narrower `authentication.configurationAdmins.users` / `authentication.configurationAdmins.groups` permission; hooks run as the Orchestrator process identity.
- `token`: access tokens issued by the local administrative command.
- empty method: unauthenticated access; use only inside an adequately isolated environment.
- `server.readOnly: true`: disable HTTP/Web writes regardless of the authentication method.

For proxy mode, remove inbound copies of the trusted identity header before authentication. Do not expose a listener that accepts arbitrary client-provided identity headers.

## TLS and mTLS

`server.tls.enabled`, `server.tls.privateKeyFile`, `server.tls.certFile`, and optional `server.tls.caFile` protect Web/API traffic. `server.tls.mutualTLS` plus `server.tls.validOUs` requires and filters client certificates. The independent `orch` client supports a trusted CA and paired client certificate/key and never offers insecure skip verification.

Separate TLS settings cover topology MySQL, metadata MySQL, optional Agent endpoints, and Consul. Avoid skip-verify settings except for a documented, time-bounded migration, and validate certificate hostname, CA, expiry, and filesystem access from the actual service runtime.

## Secret and privilege handling

- Keep configuration files and private keys readable only by the service account.
- Prefer credentials files, environment injection, or a secret manager over command-line secrets.
- Never place tokens, passwords, or OTLP credentials in URLs, logs, metric labels, traces, Wiki pages, or issue attachments.
- Give topology users read permissions needed for discovery and only the additional privileges required by enabled mutation/recovery features.
- Back up sensitive state encrypted and test restore access separately.

Authentication protects entry points; it does not replace Raft quorum, MySQL authorization, recovery filters, fencing, audit retention, or browser origin checks.

## Database, Agent, and Consul transport

Topology MySQL and metadata-backend MySQL have independent TLS settings and trust material. Verify hostnames and CA chains; do not assume the Web listener certificate protects database traffic. The topology account needs only discovery privileges plus the exact mutation/recovery privileges enabled by policy. The backend account owns only its node's metadata schema.

The optional Agent listener has its own exposure and timeout boundary. Enable it only when Agent/seed workflows are required, restrict its network path, and validate the real endpoint separately from the standard Web/API listener.

Consul HTTPS verifies certificates by default. Provide `consul.tls.caFile` or `consul.tls.caPath`, set `consul.tls.serverName` when address and certificate name differ, and configure client certificate/key together for mTLS. Keep `consul.tls.skipVerify` false outside a documented, time-bounded migration window. ACL tokens are carried in `X-Consul-Token`, not URLs.

## Deployment checklist

- Bind Web/API and Raft listeners only where required; expose Raft only to cluster members.
- Terminate TLS at a component whose trust and identity-header behavior are understood; test `server.urlPrefix`, redirects, WebSocket-independent polling, and deep links through the real proxy.
- Verify Basic/multi/proxy/token behavior, read-only enforcement, origin rejection, follower proxying, and client-certificate OU rejection with the deployed configuration.
- Rotate credentials and certificates with an explicit restart plan for components whose pools or exporters do not reload.
- Keep backups encrypted and test restore permissions separately from backup creation.
