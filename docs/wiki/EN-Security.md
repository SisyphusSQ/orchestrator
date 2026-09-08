# Security

**English** · [中文](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Security) · [Wiki home](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

`orchestrator` can change production replication topology. Place the HTTP listener and Raft ports on controlled networks, use authenticated TLS for untrusted paths, and grant database privileges according to the operations you actually enable.

## HTTP authentication

Supported policies include:

- `basic`: one configured HTTP username/password.
- `multi`: the configured power credential plus a read-only user.
- `proxy`: trust an identity header set by an authenticating reverse proxy; `PowerAuthUsers` controls writers.
- `token`: access tokens issued by the local administrative command.
- empty method: unauthenticated access; use only inside an adequately isolated environment.
- `ReadOnly: true`: disable HTTP/Web writes regardless of the authentication method.

For proxy mode, remove inbound copies of the trusted identity header before authentication. Do not expose a listener that accepts arbitrary client-provided identity headers.

## TLS and mTLS

`UseSSL`, `SSLPrivateKeyFile`, `SSLCertFile`, and optional `SSLCAFile` protect Web/API traffic. `UseMutualTLS` plus `SSLValidOUs` requires and filters client certificates. The independent `orch` client supports a trusted CA and paired client certificate/key and never offers insecure skip verification.

Separate TLS settings cover topology MySQL, metadata MySQL, optional Agent endpoints, and Consul. Avoid skip-verify settings except for a documented, time-bounded migration, and validate certificate hostname, CA, expiry, and filesystem access from the actual service runtime.

## Secret and privilege handling

- Keep JSON configuration and private keys readable only by the service account.
- Prefer credentials files, environment injection, or a secret manager over command-line secrets.
- Never place tokens, passwords, or OTLP credentials in URLs, logs, metric labels, traces, Wiki pages, or issue attachments.
- Give topology users read permissions needed for discovery and only the additional privileges required by enabled mutation/recovery features.
- Back up sensitive state encrypted and test restore access separately.

Authentication protects entry points; it does not replace Raft quorum, MySQL authorization, recovery filters, fencing, audit retention, or browser origin checks. See the detailed [security](https://github.com/SisyphusSQ/orchestrator/blob/main/docs/security.md) and [TLS](https://github.com/SisyphusSQ/orchestrator/blob/main/docs/ssl-and-tls.md) references.
